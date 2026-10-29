package semantics

import (
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReservedKeywordsMatchDuckDB keeps the static list equal to the engine's.
func TestReservedKeywordsMatchDuckDB(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	rows, err := db.Query("SELECT keyword_name FROM duckdb_keywords() WHERE keyword_category = 'reserved'")
	require.NoError(t, err)
	defer rows.Close()
	live := map[string]bool{}
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		live[k] = true
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, live, reservedKeywords)
}
