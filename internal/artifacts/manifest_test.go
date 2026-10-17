package artifacts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodManifest = `version: 1
title: Regional sales
description: Revenue by region, with drill-down to customers.
parameters:
  - name: from
    type: date
    default: today-90d
    label: From
  - name: to
    type: date
    default: today
  - name: region
    type: enum
    options_query: regions
    default: ""
  - name: top
    type: integer
    min: 1
    max: 100
    default: "10"
  - name: note
    type: text
    max_length: 20
    default: ""
  - name: share
    type: number
    default: "0.5"
  - name: live
    type: boolean
    default: "true"
queries:
  - name: regions
    connection: prod_pg
    sql: SELECT DISTINCT region FROM sales.orders ORDER BY 1
  - name: by_region
    connection: prod_pg
    sql: |
      SELECT region, sum(amount) AS revenue
      FROM sales.orders
      WHERE order_date BETWEEN $from AND $to
      GROUP BY ALL ORDER BY revenue DESC LIMIT $top
  - name: customers
    sql: |
      SELECT customer FROM sales.orders
      WHERE order_date BETWEEN $from AND $to AND ($region = '' OR region = $region)
view: a2ui
`

func mustDecode(t *testing.T, s string) *Manifest {
	t.Helper()
	m, err := Decode([]byte(s))
	require.NoError(t, err)
	return m
}

func TestDecode_Good(t *testing.T) {
	m := mustDecode(t, goodManifest)
	assert.Equal(t, "Regional sales", m.Title)
	require.Len(t, m.Parameters, 7)
	require.NotNil(t, m.Parameters[2].Default)
	assert.Equal(t, "", *m.Parameters[2].Default, `default: "" is a default`)
	assert.Equal(t, []string{"from", "to"}, m.Queries[1].Uses[:2])
	assert.Equal(t, []string{"from", "to", "top"}, m.Queries[1].Uses)
	assert.Equal(t, []string{"from", "to", "region"}, m.Queries[2].Uses)
	assert.Empty(t, m.Queries[0].Uses)
	assert.Equal(t, ViewA2UI, m.View)
}

func TestDecode_ViewDefaultsToA2UI(t *testing.T) {
	m := mustDecode(t, "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n")
	assert.Equal(t, ViewA2UI, m.View)
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := mustDecode(t, goodManifest)
	out, err := Encode(m)
	require.NoError(t, err)
	again, err := Decode(out)
	require.NoError(t, err)
	out2, err := Encode(again)
	require.NoError(t, err)
	assert.Equal(t, string(out), string(out2), "encode is stable")
	assert.Equal(t, m, again)
	assert.NotContains(t, string(out), "uses", "derived Uses is never written")
}

// base returns a one-query manifest with the given extra top-level yaml
// spliced in before the queries.
func withParams(params string) string {
	return "version: 1\ntitle: T\nparameters:\n" + params + "queries:\n  - name: q\n    sql: SELECT $p\n"
}

func TestDecode_Refusals(t *testing.T) {
	q := "queries:\n  - name: q\n    sql: SELECT 1\n"
	cases := map[string]string{
		"unknown key":              "version: 1\ntitle: T\nsecret: hunter2\n" + q,
		"unknown query key":        "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n    password: x\n",
		"uses is not an input key": "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n    uses: [a]\n",
		"duplicate top-level key":  "version: 1\ntitle: T\ntitle: U\n" + q,
		"wrong version":            "version: 2\ntitle: T\n" + q,
		"no version":               "title: T\n" + q,
		"no title":                 "version: 1\n" + q,
		"empty":                    "",
		"two documents":            "version: 1\ntitle: T\n" + q + "---\nversion: 1\n",
		"no queries":               "version: 1\ntitle: T\n",
		"view html":                "version: 1\ntitle: T\nview: html\n" + q,
		"anchor":                   "version: 1\ntitle: &t T\n" + q,
		"alias":                    "version: 1\ntitle: T\ndescription: &d x\nparameters: []\nqueries:\n  - name: q\n    sql: *d\n",
		"merge key":                "version: 1\ntitle: T\nqueries:\n  - &base {name: q, sql: SELECT 1}\n  - <<: *base\n    name: r\n",
		"custom tag":               "version: 1\ntitle: !evil T\n" + q,
		"binary tag":               "version: 1\ntitle: !!binary /w==\n" + q,
		"min nan":                  "version: 1\ntitle: T\nparameters:\n  - name: p\n    type: number\n    min: .nan\n    default: \"1\"\nqueries:\n  - name: q\n    sql: SELECT $p\n",
		"max inf":                  "version: 1\ntitle: T\nparameters:\n  - name: p\n    type: number\n    max: .inf\n    default: \"1\"\nqueries:\n  - name: q\n    sql: SELECT $p\n",
		"min -inf":                 "version: 1\ntitle: T\nparameters:\n  - name: p\n    type: number\n    min: -.inf\n    default: \"1\"\nqueries:\n  - name: q\n    sql: SELECT $p\n",
		"not yaml":                 "version: 1\n  title: [\n",
		"env interpolation is just text, not a param": "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT ${HOME}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := Decode([]byte(src))
			if name == "env interpolation is just text, not a param" {
				// ${HOME} has no "$name" form the binder knows ($ then {), so it
				// stays literal SQL text; it is never expanded.
				require.NoError(t, err)
				assert.Contains(t, m.Queries[0].SQL, "${HOME}")
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestValidate_InvalidUTF8Text(t *testing.T) {
	m := mustDecode(t, withParams("  - name: p\n    type: text\n    default: x\n"))
	m.Title = "a\xffb"
	assert.Error(t, Validate(m))
	m = mustDecode(t, withParams("  - name: p\n    type: text\n    default: x\n"))
	m.Description = "a\xffb"
	assert.Error(t, Validate(m))
	m = mustDecode(t, withParams("  - name: p\n    type: text\n    default: x\n"))
	m.Parameters[0].Label = "a\xffb"
	assert.Error(t, Validate(m))
}

func TestDecode_AliasBombIsRefusedBeforeExpansion(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: 1\ntitle: T\nx0: &a0 [lol, lol, lol, lol, lol, lol, lol, lol, lol]\n")
	for i := 1; i < 9; i++ {
		fmt.Fprintf(&b, "x%d: &a%d [*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d]\n", i, i, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1)
	}
	b.WriteString("queries:\n  - name: q\n    sql: SELECT 1\n")
	start := time.Now()
	_, err := Decode([]byte(b.String()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anchors or aliases")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestDecode_CapIsCheckedBeforeParsing(t *testing.T) {
	// 300 KB that is not even YAML: the size error wins, nothing is parsed.
	big := strings.Repeat("[", MaxManifestBytes+1)
	_, err := Decode([]byte(big))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte limit")

	// Exactly at the cap with valid content passes the size check.
	pad := strings.Repeat("a", MaxManifestBytes-len("version: 1\ntitle: T\ndescription: \nqueries:\n  - name: q\n    sql: SELECT 1\n"))
	_, err = Decode([]byte("version: 1\ntitle: T\ndescription: " + pad + "\nqueries:\n  - name: q\n    sql: SELECT 1\n"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "byte limit", "at the cap: refused for the description, not the size")
	assert.Contains(t, err.Error(), "description")
}

func TestDecode_TooManyYAMLNodes(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\nextra: [")
	b.WriteString(strings.Repeat("1,", maxYAMLNodes+10))
	b.WriteString("]\n")
	require.Less(t, b.Len(), MaxManifestBytes)
	_, err := Decode([]byte(b.String()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many YAML nodes")
}

func TestValidate_Names(t *testing.T) {
	for _, bad := range []string{"1", "1a", "_a", "a-b", "a b", "", "a.b", "é", strings.Repeat("a", 65)} {
		m := mustDecode(t, withParams("  - name: p\n    type: text\n    default: x\n"))
		m.Parameters[0].Name = bad
		m.Queries[0].SQL = "SELECT 1"
		assert.Error(t, Validate(m), "parameter name %q", bad)
	}
	m := mustDecode(t, withParams("  - name: p\n    type: text\n    default: x\n"))
	m.Queries[0].Name = "1"
	assert.Error(t, Validate(m), "query name 1")
}

func TestValidate_DuplicatesAndUndeclared(t *testing.T) {
	_, err := Decode([]byte(withParams("  - name: p\n    type: text\n    default: x\n  - name: p\n    type: text\n    default: y\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twice")

	_, err = Decode([]byte(withParams("  - name: p\n    type: text\n    default: x\n  - name: P\n    type: text\n    default: y\n")))
	require.Error(t, err, "names differing only by case are ambiguous")

	_, err = Decode([]byte("version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n  - name: q\n    sql: SELECT 2\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twice")

	_, err = Decode([]byte("version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT $nope\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$nope")
}

func TestValidate_PositionalAndNumericPlaceholdersAreNeverDeclared(t *testing.T) {
	// "1" cannot be declared (must start with a letter), so a $1 in SQL has no
	// parameter to resolve to.
	_, err := Decode([]byte("version: 1\ntitle: T\nparameters:\n  - name: \"1\"\n    type: text\n    default: x\nqueries:\n  - name: q\n    sql: SELECT $1\n"))
	require.Error(t, err)
	_, err = Decode([]byte("version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT $1\n"))
	require.Error(t, err)
}

func TestValidate_ParameterWithoutDefault(t *testing.T) {
	_, err := Decode([]byte(withParams("  - name: p\n    type: text\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default")
	_, err = Decode([]byte(withParams("  - name: p\n    type: text\n    default:\n")))
	require.Error(t, err, "null default is no default")
}

func TestValidate_Caps(t *testing.T) {
	base := func() *Manifest {
		return mustDecode(t, "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n")
	}

	m := base()
	m.Queries = nil
	for i := 0; i <= MaxQueries; i++ {
		m.Queries = append(m.Queries, Query{Name: fmt.Sprintf("q%d", i), SQL: "SELECT 1"})
	}
	assert.Error(t, Validate(m), "21 queries")
	m.Queries = m.Queries[:MaxQueries]
	assert.NoError(t, Validate(m), "20 queries")

	m = base()
	m.Queries[0].SQL = "SELECT '" + strings.Repeat("x", MaxSQLBytes) + "'"
	assert.Error(t, Validate(m), "sql over 64 KB")
	m.Queries[0].SQL = "SELECT '" + strings.Repeat("x", MaxSQLBytes-len("SELECT ''")) + "'"
	assert.NoError(t, Validate(m), "sql at 64 KB")

	m = base()
	m.Title = strings.Repeat("t", MaxTitleLength+1)
	assert.Error(t, Validate(m))
	m = base()
	m.Title = "two\nlines"
	assert.Error(t, Validate(m))

	m = base()
	for i := 0; i <= MaxParams; i++ {
		d := "x"
		m.Parameters = append(m.Parameters, Param{Name: fmt.Sprintf("p%d", i), Type: TypeText, Default: &d})
	}
	assert.Error(t, Validate(m), "51 parameters")

	m = base()
	m.Queries[0].SQL = " \n "
	assert.Error(t, Validate(m), "empty sql")
	m = base()
	m.Queries[0].SQL = "SELECT 1\x00"
	assert.Error(t, Validate(m), "NUL in sql")
	m = base()
	m.Queries[0].Connection = "Prod-PG"
	assert.Error(t, Validate(m), "bad connection name")
}

func param(t *testing.T, body string) error {
	t.Helper()
	_, err := Decode([]byte("version: 1\ntitle: T\nparameters:\n  - name: p\n" + body + "queries:\n  - name: q\n    sql: SELECT $p\n"))
	return err
}

func TestValidate_ParameterTypes(t *testing.T) {
	ok := map[string]string{
		"text":            "    type: text\n    default: hi\n",
		"text max":        "    type: text\n    max_length: 5\n    default: hello\n",
		"text max 1000":   "    type: text\n    max_length: 1000\n    default: \"\"\n",
		"number":          "    type: number\n    default: \"-1.5e3\"\n",
		"number yaml":     "    type: number\n    default: 2.5\n",
		"number bounded":  "    type: number\n    min: 0\n    max: 1\n    default: \"0.5\"\n",
		"integer":         "    type: integer\n    default: 7\n",
		"integer bounded": "    type: integer\n    min: 1\n    max: 10\n    default: \"10\"\n",
		"date literal":    "    type: date\n    default: 2026-02-28\n",
		"date relative":   "    type: date\n    default: today-3660d\n",
		"date month":      "    type: date\n    default: start-of-month\n",
		"boolean":         "    type: boolean\n    default: false\n",
		"enum static":     "    type: enum\n    options: [a, b]\n    default: a\n",
		"enum all":        "    type: enum\n    options: [\"\", a]\n    default: \"\"\n",
	}
	for name, body := range ok {
		assert.NoError(t, param(t, body), name)
	}
	bad := map[string]string{
		"unknown type":               "    type: money\n    default: x\n",
		"no type":                    "    default: x\n",
		"text default over max":      "    type: text\n    max_length: 3\n    default: hello\n",
		"text max over 1000":         "    type: text\n    max_length: 1001\n    default: x\n",
		"text default over cap":      "    type: text\n    default: " + strings.Repeat("x", 1001) + "\n",
		"max_length on number":       "    type: number\n    max_length: 3\n    default: \"1\"\n",
		"min on text":                "    type: text\n    min: 1\n    default: x\n",
		"min above max":              "    type: number\n    min: 5\n    max: 1\n    default: \"3\"\n",
		"number default not number":  "    type: number\n    default: abc\n",
		"number default NaN":         "    type: number\n    default: NaN\n",
		"number default inf":         "    type: number\n    default: Inf\n",
		"number default hex":         "    type: number\n    default: \"0x10\"\n",
		"number default below min":   "    type: number\n    min: 1\n    default: \"0\"\n",
		"integer default float":      "    type: integer\n    default: \"1.5\"\n",
		"integer over 2^53":          "    type: integer\n    default: \"9007199254740993\"\n",
		"integer default over max":   "    type: integer\n    max: 3\n    default: \"4\"\n",
		"date not a date":            "    type: date\n    default: \"2026-02-30\"\n",
		"date short":                 "    type: date\n    default: \"2026-2-3\"\n",
		"date relative zero":         "    type: date\n    default: today-0d\n",
		"date relative too far":      "    type: date\n    default: today-3661d\n",
		"date relative future":       "    type: date\n    default: today+1d\n",
		"date arbitrary":             "    type: date\n    default: yesterday\n",
		"boolean yes":                "    type: boolean\n    default: \"yes\"\n",
		"enum no options":            "    type: enum\n    default: a\n",
		"enum both":                  "    type: enum\n    options: [a]\n    options_query: q\n    default: a\n",
		"enum default not an option": "    type: enum\n    options: [a, b]\n    default: c\n",
		"enum dup options":           "    type: enum\n    options: [a, a]\n    default: a\n",
		"options on text":            "    type: text\n    options: [a]\n    default: a\n",
		"options_query on text":      "    type: text\n    options_query: q\n    default: a\n",
	}
	for name, body := range bad {
		err := param(t, body)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
	var many strings.Builder
	for i := 0; i <= MaxEnumOptions; i++ {
		fmt.Fprintf(&many, "%q,", fmt.Sprint("o", i))
	}
	assert.Error(t, param(t, "    type: enum\n    options: ["+many.String()+"]\n    default: o0\n"), "201 options")
}

func TestValidate_OptionsQuery(t *testing.T) {
	mk := func(optsSQL string, extra string) string {
		return "version: 1\ntitle: T\nparameters:\n  - name: a\n    type: enum\n    options_query: oa\n    default: \"\"\n" + extra +
			"queries:\n  - name: oa\n    sql: " + optsSQL + "\n  - name: other\n    sql: SELECT $a\n"
	}
	_, err := Decode([]byte(mk("SELECT 1", "")))
	require.NoError(t, err)

	_, err = Decode([]byte(mk("SELECT $a", "")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may not use $a")

	_, err = Decode([]byte(strings.Replace(mk("SELECT 1", ""), "options_query: oa", "options_query: nope", 1)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a query")

	cyc := "version: 1\ntitle: T\nparameters:\n" +
		"  - name: a\n    type: enum\n    options_query: oa\n    default: \"\"\n" +
		"  - name: b\n    type: enum\n    options_query: ob\n    default: \"\"\n" +
		"queries:\n  - name: oa\n    sql: SELECT $b\n  - name: ob\n    sql: SELECT $a\n"
	_, err = Decode([]byte(cyc))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depends on itself")

	chain := "version: 1\ntitle: T\nparameters:\n" +
		"  - name: a\n    type: enum\n    options_query: oa\n    default: \"\"\n" +
		"  - name: b\n    type: enum\n    options_query: ob\n    default: \"\"\n" +
		"queries:\n  - name: oa\n    sql: SELECT 1\n  - name: ob\n    sql: SELECT $a\n"
	_, err = Decode([]byte(chain))
	assert.NoError(t, err, "b depending on a is fine without a loop")
}

func TestDefaults_ResolvesDates(t *testing.T) {
	m := mustDecode(t, goodManifest)
	got, err := Defaults(m, time.Date(2026, time.March, 10, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Len(t, got, len(m.Parameters))
	assert.Equal(t, "2025-12-10", got["from"])
	assert.Equal(t, "2026-03-10", got["to"])
	assert.Equal(t, "", got["region"])
	assert.Equal(t, int64(10), got["top"])
	assert.Equal(t, true, got["live"])
}
