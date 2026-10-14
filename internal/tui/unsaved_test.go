package tui

import (
	"context"
	"database/sql"
	"testing"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realEngine runs a query for real against the same DuckDB connection, via
// internal/engine.Manager: executeREPLQuery's results and errors must reflect
// what actually happened, not a canned stub.
type realEngine struct {
	mgr *engine.Manager
}

func (e *realEngine) Execute(ctx context.Context, sqlText string, params map[string]any) (*engine.ResultSet, error) {
	return e.mgr.Execute(ctx, sqlText, params)
}

// newRealDuckDBApp is an App over a real in-memory DuckDB session:
// executeREPLQuery classifies every statement with agents.IsReadOnlyStatement,
// which parses and binds the SQL against a real catalog.
func newRealDuckDBApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE t (id INTEGER); CREATE SEQUENCE s;`)
	require.NoError(t, err)

	sess := sessiontest.ForTests(db)
	app := NewApp(&fakeWorkspace{sess: sess}, &realEngine{mgr: engine.NewManager(sess)}, nil)
	return app, db
}

func rowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&n))
	return n
}

// TestExecuteREPLQuery_FlagsRealWrites: the REPL flags a statement as a write
// by DuckDB's own classification, not a keyword-prefix guess. These shapes all
// start with a read-looking keyword and are real writes.
func TestExecuteREPLQuery_FlagsRealWrites(t *testing.T) {
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
			msg := app.executeREPLQuery(c.query)()
			result, ok := msg.(replResultMsg)
			require.True(t, ok)
			assert.True(t, result.wrote, "query %q must be flagged as a write", c.query)
		})
	}
}

func TestExecuteREPLQuery_PlainSelectIsNotAWrite(t *testing.T) {
	app, _ := newRealDuckDBApp(t)
	result, ok := app.executeREPLQuery("SELECT * FROM t")().(replResultMsg)
	require.True(t, ok)
	assert.False(t, result.wrote)
}

// TestExecuteREPLQuery_WritesActuallyRun: a write is not just flagged, it runs
// and changes the data, and there is no save step to forget.
func TestExecuteREPLQuery_WritesActuallyRun(t *testing.T) {
	app, db := newRealDuckDBApp(t)
	assert.Equal(t, 0, rowCount(t, db))

	result, _ := app.executeREPLQuery("WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x")().(replResultMsg)
	assert.NotContains(t, result.output, "-- Error", "the CTE-wrapped INSERT must run, not be swallowed by a broken cap-wrap")
	assert.Equal(t, 1, rowCount(t, db))
}

func TestApp_CtrlSIsNoLongerASave(t *testing.T) {
	app, _ := newRealDuckDBApp(t)
	assert.NotContains(t, app.renderStatusBar(), "unsaved")
}

func TestApp_StatusBarShowsReadOnlyAndName(t *testing.T) {
	app := NewApp(nil, nil, nil, WithReadOnly(true), WithWorkspaceName("acme"))
	app.width = 100
	bar := app.renderStatusBar()
	assert.Contains(t, bar, "[read-only]")
	assert.Contains(t, bar, "acme")
}

func TestWithNotices_LandInTheREPLOutput(t *testing.T) {
	app := NewApp(nil, nil, nil, WithNotices("-- connection x is not available: nope"))
	assert.Contains(t, app.repl.OutputContent(), "connection x is not available")
}

func TestExecuteREPLQuery_RefusesWhatVibeManages(t *testing.T) {
	app, _ := newRealDuckDBApp(t)
	result, ok := app.executeREPLQuery("ATTACH 'x.duckdb' AS x")().(replResultMsg)
	require.True(t, ok)
	assert.Contains(t, result.output, "not available here")

	ro, _ := newRealDuckDBApp(t)
	ro.readOnly = true
	result, _ = ro.executeREPLQuery("COPY t TO '/tmp/vibe-should-not-exist.csv'")().(replResultMsg)
	assert.Contains(t, result.output, "--read-only")
}
