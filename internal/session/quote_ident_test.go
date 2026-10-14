package session

import (
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuoteIdent_DoublesQuotes(t *testing.T) {
	assert.Equal(t, `"o""brien"`, QuoteIdent(`o"brien`))
	assert.Equal(t, `"plain"`, QuoteIdent("plain"))
	assert.Equal(t, `"a\b"`, QuoteIdent(`a\b`), "a literal backslash must pass through unescaped — DuckDB has no backslash escapes in identifiers")
}

// TestQuoteIdent_SurvivesInjectionAttempt proves the fix for the CRITICAL
// finding: a table name crafted to break out of a Go %q-quoted identifier
// and run a COPY ... TO statement must, through QuoteIdent, remain a single
// inert identifier — DuckDB must reject it as "table not found", never
// execute the embedded statement.
func TestQuoteIdent_SurvivesInjectionAttempt(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	evil := `x") ; COPY (SELECT 'pwned') TO '/tmp/should-not-exist-quoteident.csv' ; --`
	_, err = db.Exec(`SELECT * FROM ` + QuoteIdent(evil))
	require.Error(t, err, "the crafted name must not exist as a real table")
	assert.NotContains(t, err.Error(), "syntax error", "it must fail as a missing-table lookup, not a parse error splitting the statement")
}
