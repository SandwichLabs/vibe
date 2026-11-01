package engine

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/session/sessiontest"
)

// lazySession is a session with a registered, not yet attached DuckDB file
// "other" holding t(x) = 7.
func lazySession(t *testing.T) session.Session {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	sess := sessiontest.ForTests(db)
	path := filepath.Join(t.TempDir(), "other.duckdb")
	other, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = other.Exec("CREATE TABLE t(x INT); INSERT INTO t VALUES (7)")
	require.NoError(t, err)
	require.NoError(t, other.Close())
	require.NoError(t, sess.Register(session.Spec{Name: "other", Type: "duckdb", Path: path}))
	return sess
}

// ExecuteOn is ExecuteCappedIn on a connection the caller holds: the same
// rows, total, error text and lazy attach.
func TestExecuteOn_SameAsExecuteCappedIn(t *testing.T) {
	ctx := context.Background()
	queries := []string{"SELECT * FROM range(10)", "SELECT * FROM other.main.t", "SELECT * FROM nope"}

	var viaIn []string
	for _, q := range queries {
		sessIn := lazySession(t)
		mIn := NewManager(sessIn)
		rsIn, totalIn, errIn := mIn.ExecuteCappedIn(ctx, "", q, nil, 4)
		stIn, _ := sessIn.Status("other")

		sessOn := lazySession(t)
		mOn := NewManager(sessOn)
		conn, err := sessOn.Conn(ctx)
		require.NoError(t, err)
		rsOn, totalOn, errOn := mOn.ExecuteOn(ctx, conn, "", q, nil, 4)
		stOn, _ := sessOn.Status("other")
		require.NoError(t, conn.Close())

		assert.Equal(t, totalIn, totalOn, q)
		assert.Equal(t, rsIn, rsOn, q)
		assert.Equal(t, stIn.Kind, stOn.Kind, "%s: both attach, or neither does", q)
		if errIn != nil {
			require.Error(t, errOn, q)
			assert.Equal(t, errIn.Error(), errOn.Error(), q)
			viaIn = append(viaIn, errIn.Error())
		} else {
			require.NoError(t, errOn, q)
		}
	}
	assert.Len(t, viaIn, 1, "only the missing table fails")
}

// A USE on the held connection moves only it, and a later statement on it
// sees the new default catalog.
func TestExecuteOn_KeepsTheConnectionsState(t *testing.T) {
	ctx := context.Background()
	sess := lazySession(t)
	m := NewManager(sess)
	conn, err := sess.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	_, _, err = m.ExecuteOn(ctx, conn, "", "CREATE TEMP TABLE keep(x INT)", nil, 0)
	require.NoError(t, err)
	_, _, err = m.ExecuteOn(ctx, conn, "", "SELECT * FROM other.main.t", nil, 0)
	require.NoError(t, err)
	require.NoError(t, conn.Use(ctx, "other"))
	rs, _, err := m.ExecuteOn(ctx, conn, "other", "SELECT current_database() AS c", nil, 0)
	require.NoError(t, err)
	assert.Equal(t, "other", rs.Rows[0][0])

	// The pool's other connections are unmoved and do not see the TEMP table.
	rs, _, err = m.ExecuteCappedIn(ctx, "", "SELECT current_database() AS c", nil, 0)
	require.NoError(t, err)
	assert.Equal(t, sess.DefaultCatalog(), rs.Rows[0][0])
	_, _, err = m.ExecuteCappedIn(ctx, "", "SELECT * FROM keep", nil, 0)
	assert.Error(t, err)
}
