package engine

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
)

type refusingSession struct{ session.Session }

func (refusingSession) ConnectReferenced(context.Context, string, string) error {
	return errors.New(`failed to connect "pg": No credentials are set for this connection.`)
}

// A connection that cannot come up fails only the statements that need it.
func TestLazyConnectFailureIsNonFatalUnlessTheStatementNeedsIt(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	m := NewManager(refusingSession{sessiontest.ForTests(db)})
	ctx := context.Background()

	_, err = m.Execute(ctx, "CREATE TABLE pg_sales AS SELECT 1 AS x", nil)
	require.NoError(t, err, "a local statement still runs")
	rs, err := m.Execute(ctx, "SELECT 42 AS pg", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, rs.RowCount())

	_, err = m.Execute(ctx, "SELECT * FROM pg.public.orders", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No credentials are set", "the connection's own message, not 'catalog does not exist'")

	_, err = m.Execute(ctx, "SELECT nosuchcolumn FROM (SELECT 1 AS x)", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "No credentials")
}
