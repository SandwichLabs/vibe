package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

func TestQuoteIdent(t *testing.T) {
	assert.Equal(t, `"t"`, QuoteIdent("t"))
	assert.Equal(t, `"a""b"`, QuoteIdent(`a"b`))
	assert.Equal(t, `"a\""b"`, QuoteIdent(`a\"b`), "backslash is not an escape in DuckDB")
	assert.Equal(t, `"; DROP TABLE x; --"`, QuoteIdent("; DROP TABLE x; --"))
	assert.Equal(t, `"ab"`, QuoteIdent("a\x00b"))
}

func TestQuoteLiteral(t *testing.T) {
	assert.Equal(t, `'x'`, QuoteLiteral("x"))
	assert.Equal(t, `'it''s'`, QuoteLiteral("it's"))
	assert.Equal(t, `''' ); DROP TABLE t; --'`, QuoteLiteral("' ); DROP TABLE t; --"))
	assert.Equal(t, `'ab'`, QuoteLiteral("a\x00b"))
}

func TestQuoteQualified(t *testing.T) {
	assert.Equal(t, `"db"."main"."t"`, QuoteQualified("db", "main", "t"))
}

func TestParseTableRef(t *testing.T) {
	cases := []struct {
		in   string
		want TableRef
	}{
		{"t", TableRef{Name: "t"}},
		{"s.t", TableRef{Schema: "s", Name: "t"}},
		{"c.s.t", TableRef{Catalog: "c", Schema: "s", Name: "t"}},
		{`"my.table"`, TableRef{Name: "my.table"}},
		{`c."s.x"."a""b"`, TableRef{Catalog: "c", Schema: "s.x", Name: `a"b`}},
	}
	for _, c := range cases {
		got, err := ParseTableRef(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got, c.in)
	}
	for _, bad := range []string{"", ".t", "a..b", "a.b.c.d", `"unterminated`, `"a"b`, "a."} {
		_, err := ParseTableRef(bad)
		assert.Error(t, err, bad)
	}
	r := TableRef{Catalog: "c", Schema: "s", Name: `t"x`}
	assert.Equal(t, `"c"."s"."t""x"`, r.SQL())
	rt, err := ParseTableRef(r.SQL())
	require.NoError(t, err)
	assert.Equal(t, r, rt, "SQL() output re-parses")
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "prod_pg", "_x1"} {
		assert.NoError(t, ValidateName(ok), ok)
	}
	for _, bad := range []string{"", "A", "1a", "a-b", `a"b`, "a b", "x'; DROP"} {
		assert.Error(t, ValidateName(bad), bad)
	}
}

func TestRedact(t *testing.T) {
	got := secrets.RedactPatterns("Unable to connect to Postgres at user='rosa' password='WRONGpw_needle' host='h'")
	assert.NotContains(t, got, "WRONGpw_needle")
	assert.Contains(t, got, "user='rosa'")
}

func TestResolveStatusPrecedence(t *testing.T) {
	assert.Equal(t, StatusExtensionMissing, ResolveStatus(StatusOK, StatusError, StatusExtensionMissing))
	assert.Equal(t, StatusNeedsCredentials, ResolveStatus(StatusError, StatusNeedsCredentials, StatusLockedBy))
	assert.Equal(t, StatusError, ResolveStatus(StatusLockedBy, StatusError, StatusNotConnected))
	assert.Equal(t, StatusLockedBy, ResolveStatus(StatusNotConnected, StatusLockedBy, StatusOK))
	assert.Equal(t, StatusNotConnected, ResolveStatus(StatusOK, StatusNotConnected))
	assert.Equal(t, StatusOK, ResolveStatus(StatusOK))
}

// TestCopyToSQL covers the trailing-semicolon / trailing-comment /
// embedded-quote cases: CopyToSQL strips a trailing ";" (with surrounding
// whitespace) but does not try to parse a trailing "-- comment" out of the query.
func TestCopyToSQL(t *testing.T) {
	cases := []struct {
		name, query, dst, opts, want string
	}{
		{"plain", "SELECT 1", "/tmp/out.csv", "FORMAT csv", "COPY (SELECT 1\n) TO '/tmp/out.csv' WITH (FORMAT csv)"},
		{"trailing semicolon", "SELECT 1;", "/tmp/out.csv", "FORMAT csv", "COPY (SELECT 1\n) TO '/tmp/out.csv' WITH (FORMAT csv)"},
		{"trailing semicolon + blank lines", "SELECT 1 ;\n\n", "/tmp/out.csv", "FORMAT csv", "COPY (SELECT 1\n) TO '/tmp/out.csv' WITH (FORMAT csv)"},
		{"trailing comment kept", "SELECT 1 -- note", "/tmp/out.csv", "FORMAT csv", "COPY (SELECT 1 -- note\n) TO '/tmp/out.csv' WITH (FORMAT csv)"},
		{"semicolon inside a string literal kept", "SELECT ';' AS x", "/tmp/out.csv", "FORMAT csv", "COPY (SELECT ';' AS x\n) TO '/tmp/out.csv' WITH (FORMAT csv)"},
		{"quote in path doubled", "SELECT 1", "/tmp/o'brien.csv", "FORMAT csv", "COPY (SELECT 1\n) TO '/tmp/o''brien.csv' WITH (FORMAT csv)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CopyToSQL(tc.query, tc.dst, tc.opts))
		})
	}
}

func TestCopyFormat(t *testing.T) {
	ext, opts, err := CopyFormat(" CSV ")
	require.NoError(t, err)
	assert.Equal(t, ".csv", ext)
	assert.Contains(t, opts, "HEADER true")
	ext, opts, err = CopyFormat("parquet")
	require.NoError(t, err)
	assert.Equal(t, ".parquet", ext)
	assert.Equal(t, "FORMAT parquet", opts)
	_, _, err = CopyFormat("xlsx")
	assert.EqualError(t, err, `unsupported export format "xlsx" (want csv or parquet)`)
}
