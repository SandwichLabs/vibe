package engine

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scanBytes(t *testing.T, query string, maxRows int, maxBytes int64) (*ResultSet, int, error) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck
	return ScanRows(rows, maxRows, maxBytes)
}

func TestScanRows_ByteCap(t *testing.T) {
	cases := map[string]string{
		"strings": "SELECT repeat('x', 100000) AS v FROM range(100)",
		"blobs":   "SELECT repeat('x', 100000)::BLOB AS v FROM range(100)",
		"lists":   "SELECT [repeat('x', 100000), repeat('y', 100000)] AS v FROM range(100)",
		"structs": "SELECT {'a': repeat('x', 100000)} AS v FROM range(100)",
		"maps":    "SELECT MAP {'k': repeat('x', 100000)} AS v FROM range(100)",
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := scanBytes(t, q, 0, 1<<20)
			require.ErrorIs(t, err, ErrResultTooLarge)
			assert.Contains(t, err.Error(), "1 MB")
			assert.NotContains(t, err.Error(), "repeat")
		})
	}
}

func TestScanRows_ByteCapNotTriggered(t *testing.T) {
	rs, total, err := scanBytes(t, "SELECT repeat('x', 1000) AS v, 1 AS n FROM range(100)", 0, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, 100, total)
	assert.Len(t, rs.Rows, 100)

	// No cap: a large result is returned as before.
	rs, _, err = scanBytes(t, "SELECT repeat('x', 100000) AS v FROM range(100)", 0, 0)
	require.NoError(t, err)
	assert.Len(t, rs.Rows, 100)
}

// Rows past maxRows are not kept, so they do not count against the budget.
func TestScanRows_ByteCapCountsKeptRowsOnly(t *testing.T) {
	rs, total, err := scanBytes(t, "SELECT repeat('x', 100000) AS v FROM range(100)", 5, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, 100, total)
	assert.Len(t, rs.Rows, 5)
}

func TestWithMaxResultBytes(t *testing.T) {
	assert.Zero(t, maxResultBytes(context.Background()))
	assert.EqualValues(t, 7, maxResultBytes(WithMaxResultBytes(context.Background(), 7)))
}
