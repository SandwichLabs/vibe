package semantics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode_GoodDocuments(t *testing.T) {
	e, err := DecodeEntity("customer", []byte(goodEntity))
	require.NoError(t, err)
	assert.Equal(t, "prod_pg.public.customers", e.Table)
	assert.True(t, e.Columns[0].PII)
	require.Len(t, e.Assertions, 3)
	assert.Equal(t, []string{"active", "churned"}, e.Assertions[2].Values)

	m, err := DecodeMetric("net_revenue", []byte(goodMetric))
	require.NoError(t, err)
	assert.Equal(t, "sum(amount)", m.Measure)
	require.NotNil(t, m.SettledBy)
	assert.Contains(t, m.SettledBy.Text, "Refunds are excluded")

	j, err := DecodeJoin("orders_customers", []byte(goodJoin))
	require.NoError(t, err)
	require.NotNil(t, j.Measured)
	assert.Equal(t, "2026-10-29T15:30:12Z", j.Measured.At, "an unquoted timestamp decodes into the string field")
	assert.Equal(t, Count(12000), j.Measured.LeftRows)
}

func TestDecode_RoundTripIsStable(t *testing.T) {
	e, err := DecodeEntity("customer", []byte(goodEntity))
	require.NoError(t, err)
	first, err := EncodeEntity(e)
	require.NoError(t, err)
	e2, err := DecodeEntity("customer", first)
	require.NoError(t, err)
	second, err := EncodeEntity(e2)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))

	j, err := DecodeJoin("orders_customers", []byte(goodJoin))
	require.NoError(t, err)
	jb, err := EncodeJoin(j)
	require.NoError(t, err)
	j2, err := DecodeJoin("orders_customers", jb)
	require.NoError(t, err)
	assert.Equal(t, j.Measured, j2.Measured)
}

func TestDecode_StrictRefusals(t *testing.T) {
	cases := map[string]string{
		"anchor":       "version: 1\nname: &a customer\ngrain: g\ntable: t\nid_column: id\n",
		"alias":        "version: 1\nname: customer\ngrain: &g one\ntable: t\nid_column: id\nowner: *g\n",
		"tag":          "version: 1\nname: !!str customer\ngrain: g\ntable: t\nid_column: id\n",
		"merge":        "version: 1\nname: customer\ngrain: g\ntable: t\nid_column: id\n<<: {owner: x}\n",
		"unknown key":  goodEntity + "colour: red\n",
		"duplicate":    goodEntity + "grain: again\n",
		"two docs":     goodEntity + "---\n" + goodEntity,
		"empty":        "",
		"control char": "version: 1\nname: customer\ngrain: \"a\\u0007b\"\ntable: t\nid_column: id\n",
		"raw control":  "version: 1\nname: customer\ngrain: a\x07b\ntable: t\nid_column: id\n",
		"oversize":     "version: 1\nname: customer\ngrain: g\ntable: t\nid_column: id\ndescription: " + strings.Repeat("x", MaxYAMLBytes) + "\n",
		"bad name":     strings.Replace(goodEntity, "name: customer", "name: Customer", 1),
		"name stem":    strings.Replace(goodEntity, "name: customer", "name: client", 1),
		"device name":  strings.Replace(goodEntity, "name: customer", "name: con", 1),
		"bad version":  strings.Replace(goodEntity, "version: 1", "version: 2", 1),
		"no grain":     strings.Replace(goodEntity, "grain: one row per customer\n", "", 1),
		"bad table":    strings.Replace(goodEntity, "prod_pg.public.customers", "prod_pg.public.customers; drop table x", 1),
		"four parts":   strings.Replace(goodEntity, "prod_pg.public.customers", "a.b.c.d", 1),
		"no id":        strings.Replace(goodEntity, "id_column: customer_id\n", "", 1),
		"upper id":     strings.Replace(goodEntity, "id_column: customer_id", "id_column: CustomerId", 1),
		"bad conn":     strings.Replace(goodEntity, "connection: prod_pg", "connection: Prod-PG", 1),
		"bad kind":     strings.Replace(goodEntity, "kind: unique", "kind: non_negative", 1),
		"no values":    strings.Replace(goodEntity, ", values: [active, churned]", "", 1),
		"dup column":   strings.Replace(goodEntity, "{name: region}", "{name: email}", 1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeEntity("customer", []byte(doc))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestDecode_TooManyNodes(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: 1\nname: customer\ngrain: g\ntable: t\nid_column: id\ncolumns:\n")
	for i := 0; i < MaxYAMLNodes; i++ {
		b.WriteString("  - {name: c}\n")
	}
	_, err := DecodeEntity("customer", []byte(b.String()))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDecodeMetric_Refusals(t *testing.T) {
	sub := func(old, repl string) string { return strings.Replace(goodMetric, old, repl, 1) }
	cases := map[string]string{
		"no query":        sub("query: net_revenue_base\n", ""),
		"query not id":    sub("query: net_revenue_base", "query: ../etc/passwd"),
		"no measure":      sub("measure: sum(amount)\n", ""),
		"semicolon":       sub("sum(amount)", "sum(amount); drop table x"),
		"line comment":    sub("sum(amount)", "sum(amount) -- x"),
		"block comment":   sub("sum(amount)", "sum(amount) /* x */"),
		"newline":         sub("measure: sum(amount)", "measure: |\n  sum(amount)\n  + 1"),
		"unbalanced open": sub("sum(amount)", "sum(amount"),
		"unbalanced shut": sub("sum(amount)", "sum(amount))"),
		"open quote":      sub("sum(amount)", "sum(case when x = 'a then 1 end)"),
		"long measure":    sub("sum(amount)", "sum("+strings.Repeat("a", MaxMeasureBytes)+")"),
		"bad dimension":   sub("[region, month]", "[Region]"),
		"dup dimension":   sub("[region, month]", "[region, region]"),
		"bad format":      sub("format: currency", "format: euro"),
		"settled no text": sub(`text: "A counted refunds, B excluded them. Refunds are excluded."`, `text: ""`),
		"assert column":   sub("{kind: non_negative}", "{kind: non_negative, column: x}"),
		"assert kind":     sub("{kind: non_negative}", "{kind: unique}"),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeMetric("net_revenue", []byte(doc))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	t.Run("too many dimensions", func(t *testing.T) {
		dims := make([]string, MaxDimensions+1)
		for i := range dims {
			dims[i] = "d" + strings.Repeat("x", i)
		}
		_, err := DecodeMetric("net_revenue", []byte(sub("[region, month]", "["+strings.Join(dims, ", ")+"]")))
		require.Error(t, err)
	})
}

func TestDecodeMetric_MeasureShapesThatPass(t *testing.T) {
	for _, measure := range []string{"sum(amount)", "sum(amount) FILTER (WHERE status = 'paid')", "count(DISTINCT customer_id)",
		"sum(a) / nullif(sum(b), 0)", "avg(x)::DECIMAL(10,2)", "sum(CASE WHEN refunded THEN 0 ELSE amount END)",
		"sum(price * qty)", "sum(price*qty) / count(*)", "sum(a) * 100.0 / sum(b)", "sum(a * -1)", "coalesce(sum(a), 0)", "sum(CASE WHEN status = 'it''s' THEN 1 ELSE 0 END)", `sum("Amount")`, "count(*)"} {
		doc := strings.Replace(goodMetric, "measure: sum(amount)", "measure: "+yamlQuote(measure), 1)
		_, err := DecodeMetric("net_revenue", []byte(doc))
		assert.NoError(t, err, measure)
	}
}

// The P2 acceptance set still passes under the structural rules.
func TestDecodeMetric_AcceptanceSetStillPasses(t *testing.T) {
	for _, measure := range []string{"sum(amount)", "sum(amount) FILTER (WHERE status = 'paid')", "count(DISTINCT customer_id)",
		"sum(a) / nullif(sum(b), 0)", "avg(x)::DECIMAL(10,2)", "sum(CASE WHEN refunded THEN 0 ELSE amount END)", "sum(price * qty)",
		"sum(coalesce(a, 0))", "count(*) FILTER (WHERE status IN ('a', 'b'))", "sum(round(a, 2))", "max(date_trunc('month', d))",
		"count(*) FILTER (WHERE NOT (a IS NULL))", "count(*) FILTER (WHERE (status) = 'x')", "count(*) FILTER (WHERE (a) > 1 AND (b) < 2)", "sum(CASE WHEN a THEN 1 END)::BIGINT", "(sum(a) + sum(b)) / 2", "sum(a * (1 - discount))"} {
		doc := strings.Replace(goodMetric, "measure: sum(amount)", "measure: "+yamlQuote(measure), 1)
		_, err := DecodeMetric("net_revenue", []byte(doc))
		assert.NoError(t, err, measure)
	}
}

func yamlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func TestDecodeJoin_Refusals(t *testing.T) {
	sub := func(old, repl string) string { return strings.Replace(goodJoin, old, repl, 1) }
	cases := map[string]string{
		"cardinality":  sub("many_to_one", "some_to_some"),
		"no on":        sub("on:\n  - {left: customer_id, right: customer_id}\n", "on: []\n"),
		"bad column":   sub("{left: customer_id, right: customer_id}", "{left: Customer, right: customer_id}"),
		"bad left":     sub("left: orders", "left: Orders"),
		"bad time":     sub("2026-10-29T15:30:12Z", "yesterday"),
		"verdict":      sub("verdict: consistent", "verdict: fine"),
		"negative":     sub("left_rows: 12000", "left_rows: -1"),
		"fractional":   sub("left_rows: 12000", "left_rows: 1.5"),
		"hex":          sub("left_rows: 12000", "left_rows: 0x10"),
		"octal":        sub("left_rows: 12000", "left_rows: 0o17"),
		"exponent":     sub("left_rows: 12000", "left_rows: 1e3"),
		"underscore":   sub("left_rows: 12000", "left_rows: 1_000"),
		"float whole":  sub("left_rows: 12000", "left_rows: 12.0"),
		"string":       sub("left_rows: 12000", `left_rows: "12"`),
		"plus":         sub("left_rows: 12000", "left_rows: +5"),
		"name vs stem": sub("name: orders_customers", "name: other"),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeJoin("orders_customers", []byte(doc))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestDecodeGlossary(t *testing.T) {
	g, err := DecodeGlossary([]byte(goodGlossary))
	require.NoError(t, err)
	assert.Equal(t, []string{"Active customer", "Refund"}, g.Terms, "headings in a code fence are not terms; closing hashes are dropped")
	assert.Equal(t, goodGlossary, g.Text)

	_, err = DecodeGlossary([]byte(strings.Repeat("x", MaxGlossaryBytes+1)))
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = DecodeGlossary([]byte("## a\x1b[31m\n"))
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestEncode_RefusesInvalid(t *testing.T) {
	_, err := EncodeMetric(&Metric{Name: "x"})
	assert.ErrorIs(t, err, ErrInvalid)
	m, err := DecodeMetric("net_revenue", []byte(goodMetric))
	require.NoError(t, err)
	m.Name = "Bad Name"
	_, err = EncodeMetric(m)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestValidate_DeviceNamesAreRefused(t *testing.T) {
	for _, n := range []string{"con", "nul", "com1", "lpt9"} {
		e, err := DecodeEntity("customer", []byte(goodEntity))
		require.NoError(t, err)
		e.Name = n
		assert.ErrorIs(t, ValidateEntity(e), ErrInvalid, n)
	}
	e, err := DecodeEntity("customer", []byte(goodEntity))
	require.NoError(t, err)
	e.Name = "_con"
	assert.NoError(t, ValidateEntity(e))
}

func TestDecodeMetric_MeasureCannotBreakOut(t *testing.T) {
	cases := []string{
		// an unterminated string or identifier
		`count(*) || 'x''`, `count(*) || "x""`, `count(*) || E'x\'`,
		// a second column or a second query
		"sum(amount) AS x, (SELECT 1) AS y", "sum(a), sum(b)", "(SELECT getenv('HOME'))",
		"0 FROM read_text('/etc/passwd') UNION ALL SELECT sum(amount)",
		"sum(a) UNION SELECT 1", "sum(a) GROUP BY b", "sum(a) ORDER BY b", "sum(a) LIMIT 1", "sum(a) HAVING 1",
		"sum(a) WHERE b > 0", "sum(a) INTERSECT SELECT 1", "sum(a) EXCEPT SELECT 1", "sum(a) QUALIFY 1", "sum(a) WINDOW w AS ()",
		"sum(a) OFFSET 1", "WITH x AS (SELECT 1) SELECT 1", "sum(a) AS x", "CAST(a AS INTEGER)",
		"string_agg(x, ',' ORDER BY y)", "extract(year FROM d)",
		// calls
		"getenv('HOME')", "main.getenv('HOME')", `"getenv"('HOME')`, "query('select 1')", "query_table('t')",
		"read_csv('x')", "read_parquet('x')", "glob('*')", "sum(length(read_text('/etc/passwd')))",
		// more than one column or row, side effects, state
		"sum(COLUMNS(*))", "sum(COLUMNS('a.*'))", "sum(unnest([1,2]))", "sum(nextval('s'))", "sum(setval('s', 1))",
		"sum(currval('s'))", "sum(CAST(current_setting('threads') AS INTEGER))", "sum(getvariable('v'))", "sum(setvariable('v', 1))",
		"sum(metric('x'))", "sum(sleep_ms(5))", "sum(write_log('x'))", "sum(setseed(0.5))",
		"count(*) FILTER (WHERE (SELECT 1) = 1)", "count(*) FILTER (WHERE (TABLE t) IS NULL)", "sum(a) WHERE (b)", "sum(a) + WHERE(b)",
		// a star that is not count(*)
		"sum(*)", "count(DISTINCT *)", "sum(t.*)", "min(* EXCLUDE (a))",
		// statement-shaped subqueries hidden in parentheses
		"sum(a) + (PIVOT 'file.csv' ON a USING sum(b))", "count(*) FILTER (WHERE a IN (TABLE t))", "(DESCRIBE 'x.csv')",
		"count(*) FILTER (WHERE EXISTS (SUMMARIZE t))", "count(*) FILTER (WHERE EXISTS (SHOW TABLES))",
		"(PIVOT duckdb_secrets() ON x)", "(UNPIVOT t ON a)", "(VALUES (1))", "sum(a) + (EXPLAIN SELECT 1)",
		"sum(a) + (CALL x())", "sum(a) + (PRAGMA x)", "sum(a) + ( FROM t )", "sum(a) + ((SELECT 1))", "sum(a) + (pivot_wider)",
		"sum(a) IN (TABLE t)", "sum(a) IN (VALUES (1))", "sum(a) + (WITH x AS (SELECT 1) SELECT 1)",
		// a call outside the scalar and aggregate allow-list
		"sum(a) + read_csv_auto('x')", "sum(main.length(a))", "sum(foo_bar(a))", "sum(a) + duckdb_secrets()", "sum(which_secret('a','b'))",
		"sum(sniff_csv('x'))", `sum("getenv"('x'))`,
		// string forms
		"sum(a) || E'x'", "sum(a) || $$x$$", "sum(a) || $t$x$t$",
	}
	for _, measure := range cases {
		doc := strings.Replace(goodMetric, "measure: sum(amount)", "measure: "+yamlQuote(measure), 1)
		_, err := DecodeMetric("net_revenue", []byte(doc))
		assert.ErrorIs(t, err, ErrInvalid, measure)
	}
}

func TestValidate_ReservedKeywordsAreRefused(t *testing.T) {
	m, err := DecodeMetric("net_revenue", []byte(goodMetric))
	require.NoError(t, err)
	m.Dimensions = []string{"null"}
	assert.ErrorIs(t, ValidateMetric(m), ErrInvalid)
	m.Dimensions = []string{"region"}
	m.Name = "select"
	assert.ErrorIs(t, ValidateMetric(m), ErrInvalid)

	e, err := DecodeEntity("customer", []byte(goodEntity))
	require.NoError(t, err)
	e.Columns = []Column{{Name: "order"}}
	assert.ErrorIs(t, ValidateEntity(e), ErrInvalid)
	e.Columns = nil
	e.Table = "prod_pg.public.order"
	assert.ErrorIs(t, ValidateEntity(e), ErrInvalid)
	e.Table = "prod_pg.public.ORDER"
	assert.ErrorIs(t, ValidateEntity(e), ErrInvalid)
	e.Table = "orders"
	e.IDColumn = "group"
	assert.ErrorIs(t, ValidateEntity(e), ErrInvalid)
	e.IDColumn = "id"
	e.Name = "table"
	assert.ErrorIs(t, ValidateEntity(e), ErrInvalid)

	j, err := DecodeJoin("orders_customers", []byte(goodJoin))
	require.NoError(t, err)
	j.Name = "union"
	assert.ErrorIs(t, ValidateJoin(j), ErrInvalid)
	j.Name = "x"
	j.On = []JoinOn{{Left: "from", Right: "id"}}
	assert.ErrorIs(t, ValidateJoin(j), ErrInvalid)
}
