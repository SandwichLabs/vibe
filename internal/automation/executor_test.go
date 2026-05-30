package automation_test

// TDD RED — Phase 5 / unit P5.2: "Declarative action engine (executor) over the
// real backend" (parity plan §5.1).
//
// This is a REAL integration test: it opens a real encrypted .vibe container,
// materializes DuckDB, seeds a `customers` table with 3 rows, and drives the
// production automation.Executor against a real engine.Manager-backed
// QueryEngine. No mocks for the data path.
//
// Acceptance behavior asserted here:
//   - Server-side actions (RunSQL/RunQuery/SetFilter) execute against real DuckDB
//     and mutations are observable on re-query.
//   - If/Then conditions evaluate against query results and gate the dependent
//     action (the SetFilter only fires because COUNT(*) > 0).
//   - UI-bound actions (OpenForm) are returned as structured UI intents — exactly
//     one open_form intent with target cust_form and the computed filter — not
//     silently dropped or executed in Go.
//   - RunMacro recursion is bounded: an A->B->A cycle is rejected with a
//     depth/cycle error and performs no further mutation.

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realQueryEngine adapts a real engine.Manager (DuckDB) to the
// automation.QueryEngine capability interface the Executor depends on.
type realQueryEngine struct {
	mgr *engine.Manager
}

func (r *realQueryEngine) Execute(ctx context.Context, query string, params map[string]any) (*automation.ResultSet, error) {
	rs, err := r.mgr.Execute(ctx, query, params)
	if err != nil {
		return nil, err
	}
	cols := make([]automation.Column, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = automation.Column{Name: c.Name, Type: c.Type}
	}
	return &automation.ResultSet{Columns: cols, Rows: rs.Rows}, nil
}

// newExecutorEnv creates a real .vibe + DuckDB, seeds `customers` with 3 rows,
// and returns a QueryEngine adapter plus the live DuckDB handle for re-query.
func newExecutorEnv(t *testing.T) (*realQueryEngine, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "exec.vibe")
	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)
	_, err = ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, tier TEXT);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO customers (id, name, tier) VALUES
		(1, 'Alice', 'free'),
		(2, 'Bob', 'free'),
		(3, 'Carol', 'free');`)
	require.NoError(t, err)

	return &realQueryEngine{mgr: engine.NewManager(ddb)}, ddb
}

// TestExecutorServerAndUIActions exercises the §5.2 acceptance behavior end to
// end: a RunSQL mutation, an If/Then-gated SetFilter, and an OpenForm UI intent.
func TestExecutorServerAndUIActions(t *testing.T) {
	qe, ddb := newExecutorEnv(t)
	ctx := context.Background()

	exec := automation.NewExecutor(automation.WithQueryEngine(qe))

	spec := automation.MacroSpec{
		ID:    "promote_first",
		Title: "Promote first customer",
		Actions: []automation.MacroAction{
			// action1: server-side mutation on real DuckDB.
			{
				Kind: automation.ActionRunSQL,
				Args: map[string]string{
					"sql": "UPDATE customers SET tier = 'gold' WHERE id = 1",
				},
			},
			// action2: If(RunQuery COUNT(*) > 0) Then SetFilter on customers.
			{
				Kind: automation.ActionSetFilter,
				Args: map[string]string{
					"target": "customers",
					"filter": "tier = 'gold'",
				},
				Condition: &automation.MacroCondition{
					Expr: "RunQuery(SELECT COUNT(*) AS n FROM customers WHERE tier = 'gold') > 0",
				},
			},
			// action3: UI-bound — resolved into a structured intent, not run in Go.
			{
				Kind: automation.ActionOpenForm,
				Args: map[string]string{
					"target": "cust_form",
				},
			},
		},
	}

	res, err := exec.Run(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, res)

	// (1) The UPDATE actually changed the DuckDB row — observable on re-query.
	var tier string
	require.NoError(t, ddb.QueryRow(`SELECT tier FROM customers WHERE id = 1;`).Scan(&tier))
	assert.Equal(t, "gold", tier, "RunSQL UPDATE must mutate real DuckDB")

	// (2) The conditional branch fired: SetFilter executed because COUNT(*) > 0.
	//     The Executor records applied server-side filters so the caller can
	//     observe that the guarded action ran.
	filters := res.Filters
	require.Len(t, filters, 1, "the If/Then-gated SetFilter must have fired exactly once")
	assert.Equal(t, "customers", filters[0].Target)
	assert.Equal(t, "tier = 'gold'", filters[0].Filter)

	// (3) Exactly one open_form UI intent, with target cust_form.
	var openForms []automation.UIIntent
	for _, in := range res.Intents {
		if in.Kind == automation.IntentOpenForm {
			openForms = append(openForms, in)
		}
	}
	require.Len(t, openForms, 1, "OpenForm must surface exactly one open_form UI intent")
	assert.Equal(t, "cust_form", openForms[0].Target)
}

// TestExecutorRunMacroCycleRejected asserts RunMacro recursion is bounded: a
// macro cycle (A calls B calls A) is rejected with a depth/cycle error and does
// not silently spin or partially mutate beyond already-committed prior actions.
func TestExecutorRunMacroCycleRejected(t *testing.T) {
	qe, ddb := newExecutorEnv(t)
	ctx := context.Background()

	specs := map[string]automation.MacroSpec{
		"A": {
			ID: "A",
			Actions: []automation.MacroAction{
				{Kind: automation.ActionRunMacro, Args: map[string]string{"target": "B"}},
			},
		},
		"B": {
			ID: "B",
			Actions: []automation.MacroAction{
				{Kind: automation.ActionRunMacro, Args: map[string]string{"target": "A"}},
			},
		},
	}

	// The resolver lets RunMacro look up the named sub-macro by id.
	resolver := func(id string) (automation.MacroSpec, error) {
		s, ok := specs[id]
		if !ok {
			return automation.MacroSpec{}, automation.ErrMacroNotFound
		}
		return s, nil
	}

	exec := automation.NewExecutor(
		automation.WithQueryEngine(qe),
		automation.WithMacroResolver(resolver),
	)

	_, err := exec.Run(ctx, specs["A"])
	require.Error(t, err, "an A->B->A macro cycle must be rejected")
	assert.ErrorIs(t, err, automation.ErrMacroCycle, "cycle must surface as ErrMacroCycle")

	// No customers were mutated by the (purely RunMacro) cycle.
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE tier <> 'free';`).Scan(&n))
	assert.Equal(t, 0, n, "a rejected cycle must not partially mutate DuckDB")
}

// TestExecutorUnknownActionErrors asserts an unknown/invalid action kind returns
// a wrapped error rather than being silently skipped.
func TestExecutorUnknownActionErrors(t *testing.T) {
	qe, _ := newExecutorEnv(t)
	ctx := context.Background()

	exec := automation.NewExecutor(automation.WithQueryEngine(qe))

	spec := automation.MacroSpec{
		ID: "bad",
		Actions: []automation.MacroAction{
			{Kind: "Teleport"},
		},
	}
	_, err := exec.Run(ctx, spec)
	require.Error(t, err, "unknown action kind must error")
}
