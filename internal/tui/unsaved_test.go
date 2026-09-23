package tui

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realDuckDBContainer is a ContainerAccess backed by a real, in-memory
// DuckDB connection — needed because executeREPLQuery now classifies every
// statement with agents.IsReadOnlyStatement, which parses/binds the SQL
// against a real DuckDB catalog rather than guessing from a keyword prefix.
type realDuckDBContainer struct {
	db *sql.DB
}

func (c *realDuckDBContainer) SQLiteDB() *sql.DB { return nil }
func (c *realDuckDBContainer) DuckDBDB() *sql.DB { return c.db }
func (c *realDuckDBContainer) Save() error       { return nil }
func (c *realDuckDBContainer) Close() error      { return nil }

// realEngine runs a query for real against the same DuckDB connection, via
// internal/engine.Manager — executeREPLQuery's row results/errors need to
// reflect what actually happened (e.g. a successful multi-statement INSERT
// must actually insert), not a canned stub.
type realEngine struct {
	mgr *engine.Manager
}

func (e *realEngine) Execute(ctx context.Context, sqlText string, params map[string]any) (*engine.ResultSet, error) {
	return e.mgr.Execute(ctx, sqlText, params)
}

func newRealDuckDBApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE t (id INTEGER); CREATE SEQUENCE s;`)
	require.NoError(t, err)

	c := &realDuckDBContainer{db: db}
	mgr := engine.NewManager(db)
	app := NewApp(c, &realEngine{mgr: mgr}, nil)
	return app, db
}

func rowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&n))
	return n
}

// TestExecuteREPLQuery_MarksUnsavedForRealWrites is the regression guard for
// the live-reproduced data-loss bug: the old prefix-based looksLikeWrite
// treated these three shapes as reads (they all "start with" a read-looking
// keyword), so the TUI never saved them, and the rows/tables were gone
// after quit. All three must actually run AND mark the session unsaved.
func TestExecuteREPLQuery_MarksUnsavedForRealWrites(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"CTE wrapping an INSERT", "WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x"},
		{"multi-statement with a trailing INSERT", "SELECT 1; INSERT INTO t VALUES (2)"},
		{"EXPLAIN ANALYZE of an INSERT", "EXPLAIN ANALYZE INSERT INTO t VALUES (3)"},
		{"a nextval() read that mutates a sequence", "SELECT nextval('s')"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app, _ := newRealDuckDBApp(t)
			cmd := app.executeREPLQuery(c.query)
			require.NotNil(t, cmd)
			msg := cmd()
			result, ok := msg.(replResultMsg)
			require.True(t, ok)
			assert.True(t, result.wrote, "query %q must mark the session unsaved", c.query)
		})
	}
}

// TestExecuteREPLQuery_PlainSelectDoesNotMarkUnsaved is the read-side
// counterpart: an ordinary SELECT must never cost an unnecessary save.
func TestExecuteREPLQuery_PlainSelectDoesNotMarkUnsaved(t *testing.T) {
	app, _ := newRealDuckDBApp(t)
	cmd := app.executeREPLQuery("SELECT * FROM t")
	msg := cmd()
	result, ok := msg.(replResultMsg)
	require.True(t, ok)
	assert.False(t, result.wrote)
}

// TestExecuteREPLQuery_WritesActuallyPersistData proves the three shapes
// above aren't just correctly FLAGGED as writes — they actually ran and
// changed the data (the point of the whole fix: rows must survive).
func TestExecuteREPLQuery_WritesActuallyPersistData(t *testing.T) {
	app, db := newRealDuckDBApp(t)
	assert.Equal(t, 0, rowCount(t, db))

	cmd := app.executeREPLQuery("WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x")
	msg := cmd()
	result, _ := msg.(replResultMsg)
	assert.NotContains(t, result.output, "-- Error", "the CTE-wrapped INSERT must actually run, not be swallowed by a broken cap-wrap")
	assert.Equal(t, 1, rowCount(t, db))
}
