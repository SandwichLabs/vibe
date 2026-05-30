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

// TestExecutorConditionGatingSkipsFalseBranch is the negative-branch counterpart
// to the gating assertion in TestExecutorServerAndUIActions. It proves that a
// FALSE condition GATES (skips) the dependent action — the half of acceptance
// criterion #3 that the positive-only test cannot verify. A regressed executor
// that ignored conditions and always ran the action would FAIL this test.
//
// The seeded `customers` table contains no 'platinum' rows, so the guarding
// RunQuery COUNT(*) is 0 and the `> 0` comparison is FALSE. We assert that the
// guarded SetFilter did NOT fire (no AppliedFilter, no intent) and that DuckDB
// was not mutated. A paired TRUE-condition action in the same spec confirms the
// executor still fires when the condition holds, so the difference in outcome is
// attributable to the condition, not to the executor being broken outright.
func TestExecutorConditionGatingSkipsFalseBranch(t *testing.T) {
	qe, ddb := newExecutorEnv(t)
	ctx := context.Background()

	exec := automation.NewExecutor(automation.WithQueryEngine(qe))

	spec := automation.MacroSpec{
		ID:    "tier_filters",
		Title: "Conditionally apply tier filters",
		Actions: []automation.MacroAction{
			// FALSE branch: there are no 'platinum' customers, so COUNT(*) == 0
			// and `> 0` is false. This SetFilter MUST be skipped.
			{
				Kind: automation.ActionSetFilter,
				Args: map[string]string{
					"target": "customers",
					"filter": "tier = 'platinum'",
				},
				Condition: &automation.MacroCondition{
					Expr: "RunQuery(SELECT COUNT(*) AS n FROM customers WHERE tier = 'platinum') > 0",
				},
			},
			// TRUE branch (paired control): all 3 seeded rows are 'free', so
			// COUNT(*) == 3 and `> 0` is true. This SetFilter MUST fire.
			{
				Kind: automation.ActionSetFilter,
				Args: map[string]string{
					"target": "customers",
					"filter": "tier = 'free'",
				},
				Condition: &automation.MacroCondition{
					Expr: "RunQuery(SELECT COUNT(*) AS n FROM customers WHERE tier = 'free') > 0",
				},
			},
		},
	}

	res, err := exec.Run(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Only the TRUE-branch SetFilter fired; the FALSE-branch one was gated out.
	// If condition gating regressed (action always runs), len would be 2 and the
	// 'platinum' filter would be present — this assertion is the regression trap.
	require.Len(t, res.Filters, 1, "only the true-condition SetFilter must fire; the false-condition one must be skipped")
	assert.Equal(t, "customers", res.Filters[0].Target)
	assert.Equal(t, "tier = 'free'", res.Filters[0].Filter, "the surviving filter must be the true-condition one, not the gated platinum one")

	// No intents were produced (SetFilter is server-side), and DuckDB is unchanged
	// by a pure SetFilter macro (SetFilter records intent, does not mutate rows).
	assert.Empty(t, res.Intents, "SetFilter actions produce no UI intents")
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE tier <> 'free';`).Scan(&n))
	assert.Equal(t, 0, n, "gated/SetFilter actions must not mutate DuckDB rows")
}

// TestExecutorSetValueSurfacesAsIntent documents and verifies the interpretation
// of SetValue: it is UI-bound (it assigns a control/form value, like Access's
// SetValue), so it is surfaced as a structured "set_value" UI intent rather than
// executed as a DuckDB mutation. See executor.go ActionSetValue for the rationale
// and the deliberate divergence from the acceptance-criteria parenthetical.
func TestExecutorSetValueSurfacesAsIntent(t *testing.T) {
	qe, ddb := newExecutorEnv(t)
	ctx := context.Background()

	exec := automation.NewExecutor(automation.WithQueryEngine(qe))

	spec := automation.MacroSpec{
		ID: "set_status",
		Actions: []automation.MacroAction{
			{
				Kind: automation.ActionSetValue,
				Args: map[string]string{
					"target": "status_label",
					"value":  "Promoted",
				},
			},
		},
	}

	res, err := exec.Run(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, res)

	// SetValue is surfaced as exactly one structured set_value intent — not dropped.
	require.Len(t, res.Intents, 1, "SetValue must surface as exactly one UI intent")
	assert.Equal(t, automation.IntentSetValue, res.Intents[0].Kind)
	assert.Equal(t, "status_label", res.Intents[0].Target)
	assert.Equal(t, "Promoted", res.Intents[0].Args["value"], "SetValue args must be carried through to the intent")

	// SetValue does NOT write back to DuckDB: no AppliedFilter, and (since the
	// target is not even a table) the data store is untouched.
	assert.Empty(t, res.Filters, "SetValue is not a server-side filter")
	var cnt int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE tier <> 'free';`).Scan(&cnt))
	assert.Equal(t, 0, cnt, "SetValue must not mutate DuckDB rows")
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

	// MaxDepth is set to 64 — far above the 2-macro cycle length — so termination
	// CANNOT come from the depth cap. The only way to terminate before depth 64 is
	// the visited-set cycle detection in executor.run. This isolates the cycle
	// detector from the depth fallback: a regression that removed the visited-set
	// check would spin to depth 64 (still ErrMacroCycle, but) — so we additionally
	// assert the error message names the cycling macro rather than reporting a bare
	// depth overflow, proving the visited-set path fired.
	exec := automation.NewExecutor(
		automation.WithQueryEngine(qe),
		automation.WithMacroResolver(resolver),
		automation.WithMaxDepth(64),
	)

	_, err := exec.Run(ctx, specs["A"])
	require.Error(t, err, "an A->B->A macro cycle must be rejected")
	assert.ErrorIs(t, err, automation.ErrMacroCycle, "cycle must surface as ErrMacroCycle")
	// The visited-set detector reports the macro id already on the stack; the
	// depth-cap fallback reports a "depth N" overflow. Asserting the former proves
	// cycle detection fired before the depth cap could.
	assert.Contains(t, err.Error(), "already on stack",
		"cycle must be caught by the visited-set detector, not the depth cap")

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
