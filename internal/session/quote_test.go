package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	got := Redact("Unable to connect to Postgres at user='rosa' password='WRONGpw_needle' host='h'")
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
