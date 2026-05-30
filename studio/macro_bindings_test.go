package main

// TDD RED — Phase 5 / unit P5.3: "Wails RunMacro / macro CRUD bindings +
// UI-intent contract" (parity plan §5.1/§5.3).
//
// REAL integration test: opens a real encrypted .vibe container (SQLCipher +
// DuckDB) via newOpenApp, exercises the production Wails-bound macro methods,
// and asserts the P5.3 acceptance contract end-to-end (no mocks):
//
//   - SaveMacro/GetMacro/ListMacro/DeleteMacro are guardStudioOnly: in App Mode
//     they return errCapabilityDenied (definitions are builder-only)
//   - RunMacro is callable in BOTH modes (App-Mode users press macro-bound
//     buttons)
//   - In Studio Mode RunMacro applies server-side mutations through the live
//     engine (a RunSQL INSERT hits DuckDB) AND returns the structured UI-intent
//     list (an OpenForm action surfaces as an open_form intent)
//   - In App Mode RunMacro refuses privileged actions (raw RunSQL/RunQuery DDL)
//     consistent with capability gating, while a pure-navigation (OpenForm)
//     macro still returns its intent
//
// These assertions FAIL against the current stubs (the bindings either do not
// exist or are no-ops) until P5.3 is implemented.

import (
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// customerCount counts rows in the customers table, used to prove a RunSQL
// INSERT inside a macro actually mutated DuckDB through the live engine.
func customerCount(t *testing.T, app *App) int {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers;`).Scan(&n))
	return n
}

// insertAndOpenMacro is a macro that mutates DuckDB (RunSQL INSERT) and then
// asks the frontend to open a form (OpenForm) — the canonical "command button"
// that writes a row and navigates.
func insertAndOpenMacro() automation.MacroSpec {
	return automation.MacroSpec{
		ID:    "add_customer",
		Title: "Add Customer",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunSQL, Args: map[string]string{
				"sql": "INSERT INTO customers (id, name) VALUES (42, 'Macro Made')",
			}},
			{Kind: automation.ActionOpenForm, Args: map[string]string{
				"target": "customer_form",
			}},
		},
	}
}

// pureNavMacro is a macro with only UI-bound navigation (no server mutation),
// which must remain runnable even in App Mode.
func pureNavMacro() automation.MacroSpec {
	return automation.MacroSpec{
		ID:    "open_customers",
		Title: "Open Customers",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionOpenForm, Args: map[string]string{
				"target": "customer_form",
			}},
		},
	}
}

// privilegedMacro is a macro whose RunSQL action runs arbitrary DDL/DML — the
// App-Mode capability surface must refuse this when RunMacro is called in App
// Mode, exactly like the raw-SQL bindings.
func privilegedMacro() automation.MacroSpec {
	return automation.MacroSpec{
		ID:    "wipe_customers",
		Title: "Wipe Customers",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunSQL, Args: map[string]string{
				"sql": "DROP TABLE customers",
			}},
		},
	}
}

// hasIntent reports whether the returned intent list contains an intent of the
// given kind targeting target.
func hasIntent(intents []MacroIntent, kind, target string) bool {
	for _, in := range intents {
		if in.Kind == kind && in.Target == target {
			return true
		}
	}
	return false
}

// findIntent returns the first intent of the given kind targeting target, or
// nil if absent.
func findIntent(intents []MacroIntent, kind, target string) *MacroIntent {
	for i := range intents {
		if intents[i].Kind == kind && intents[i].Target == target {
			return &intents[i]
		}
	}
	return nil
}

// openThenFilterMacro opens a form and then applies a SetFilter to it — the
// canonical "open this form filtered" command button.
func openThenFilterMacro() automation.MacroSpec {
	return automation.MacroSpec{
		ID:    "open_orders_filtered",
		Title: "Open Orders (filtered)",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionOpenForm, Args: map[string]string{
				"target": "frm_orders",
			}},
			{Kind: automation.ActionSetFilter, Args: map[string]string{
				"target": "frm_orders",
				"filter": "total > 50",
			}},
		},
	}
}

// TestMacroBindingsSetFilterForwarded proves the SetFilter path is wired
// end-to-end: a macro whose SetFilter action the Executor records in
// res.Filters must be forwarded by App.RunMacro as a set_filter UI intent
// carrying the predicate under the "where" arg key the frontend consumes. A
// regression in the forwarding code (or an arg-key mismatch) fails this test.
func TestMacroBindingsSetFilterForwarded(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()

	require.NoError(t, app.SaveMacro(openThenFilterMacro()))

	res, err := app.RunMacro("open_orders_filtered")
	require.NoError(t, err, "RunMacro must succeed for an open+filter macro")
	require.NotNil(t, res)

	// The OpenForm action surfaces as an open_form intent.
	assert.True(t, hasIntent(res.Intents, "open_form", "frm_orders"),
		"expected an open_form intent, got: %+v", res.Intents)

	// The SetFilter action must be forwarded as a set_filter intent targeting the
	// same form, carrying the predicate under the "where" key (the contract the
	// frontend's applyMacroIntents consumes).
	sf := findIntent(res.Intents, automation.IntentSetFilter, "frm_orders")
	require.NotNil(t, sf,
		"SetFilter must be forwarded as a set_filter intent; got: %+v", res.Intents)
	assert.Equal(t, "total > 50", sf.Args["where"],
		"set_filter intent must carry the predicate under the \"where\" arg key, got: %+v", sf.Args)
}

// TestMacroBindings is the headline P5.3 acceptance test.
func TestMacroBindings(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()

	// Seed a customers table the macros operate on.
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name VARCHAR);`)
	require.NoError(t, err)

	// --- Studio Mode: CRUD bindings succeed ---
	spec := insertAndOpenMacro()
	require.NoError(t, app.SaveMacro(spec), "SaveMacro must persist a macro in Studio Mode")

	got, err := app.GetMacro("add_customer")
	require.NoError(t, err, "GetMacro must round-trip in Studio Mode")
	require.NotNil(t, got)
	assert.Equal(t, "add_customer", got.ID)
	assert.Len(t, got.Actions, 2)

	list, err := app.ListMacro()
	require.NoError(t, err, "ListMacro must succeed in Studio Mode")
	require.Len(t, list, 1)

	// Also save the pure-nav and privileged macros (in Studio Mode) so they exist
	// for the App-Mode RunMacro assertions below.
	require.NoError(t, app.SaveMacro(pureNavMacro()))
	require.NoError(t, app.SaveMacro(privilegedMacro()))

	// --- Studio Mode: RunMacro mutates DuckDB AND returns UI intents ---
	base := customerCount(t, app)
	res, err := app.RunMacro("add_customer")
	require.NoError(t, err, "RunMacro must succeed in Studio Mode")
	require.NotNil(t, res)
	assert.Equal(t, base+1, customerCount(t, app),
		"RunMacro's RunSQL INSERT must hit DuckDB through the live engine in Studio Mode")
	assert.True(t, hasIntent(res.Intents, "open_form", "customer_form"),
		"RunMacro must return the OpenForm action as an open_form UI intent, got: %+v", res.Intents)

	// --- flip to App Mode ---
	require.NoError(t, app.SetMode("app"))

	// CRUD bindings are denied in App Mode (definitions are builder-only).
	assert.True(t, isCapabilityDenied(app.SaveMacro(insertAndOpenMacro())),
		"SaveMacro must be capability-denied in App Mode")
	assert.True(t, isCapabilityDenied(app.DeleteMacro("add_customer")),
		"DeleteMacro must be capability-denied in App Mode")
	{
		_, e := app.GetMacro("add_customer")
		assert.True(t, isCapabilityDenied(e), "GetMacro must be capability-denied in App Mode")
	}
	{
		_, e := app.ListMacro()
		assert.True(t, isCapabilityDenied(e), "ListMacro must be capability-denied in App Mode")
	}

	// --- App Mode: RunMacro is still callable for pure-navigation macros ---
	navRes, err := app.RunMacro("open_customers")
	require.NoError(t, err, "RunMacro on a pure-navigation macro must still succeed in App Mode")
	require.NotNil(t, navRes)
	assert.True(t, hasIntent(navRes.Intents, "open_form", "customer_form"),
		"a pure-navigation macro must still return its open_form intent in App Mode, got: %+v", navRes.Intents)

	// --- App Mode: RunMacro refuses privileged (raw SQL / DDL) actions ---
	beforeWipe := customerCount(t, app)
	_, err = app.RunMacro("wipe_customers")
	require.Error(t, err, "RunMacro must refuse a privileged RunSQL/DDL macro in App Mode")
	assert.True(t, isCapabilityDenied(err),
		"a RunSQL/DDL macro must be refused by the App-Mode capability surface, got: %v", err)
	assert.Equal(t, beforeWipe, customerCount(t, app),
		"a refused privileged macro must make no mutation in App Mode")
}

// TestMacroBindingsNoFileOpen verifies the no-file-open guards mirror the other
// definition bindings.
func TestMacroBindingsNoFileOpen(t *testing.T) {
	app := NewApp(testLogger())

	err := app.SaveMacro(insertAndOpenMacro())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")
}
