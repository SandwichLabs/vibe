package session

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ROLLBACK with no open transaction is the normal case on release; the driver
// must report it as a transaction-class error, and nothing else may match.
func TestIsNoTransaction(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	c, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, err = c.ExecContext(context.Background(), "ROLLBACK")
	require.Error(t, err)
	assert.True(t, isNoTransaction(err), "got %v", err)

	_, err = c.ExecContext(context.Background(), "SELECT * FROM nope")
	require.Error(t, err)
	assert.False(t, isNoTransaction(err))
	assert.False(t, isNoTransaction(assert.AnError))
}
