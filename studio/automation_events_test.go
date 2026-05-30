package main

// TDD RED — Phase 5 / unit P5.7: "AutoExec + form/record event hooks"
// (parity plan §5.3).
//
// REAL integration test: opens a real encrypted .vibe container (SQLCipher +
// DuckDB) via newOpenApp and asserts the P5.7 acceptance contract end-to-end
// (no mocks, real Executor, real DuckDB writes):
//
//   - autoexec_macro persists in _vibe_meta and is executed on file open via the
//     startup binding (App.RunAutoExec), returning the startup intents the shell
//     applies (an OpenSwitchboard AutoExec macro surfaces as an open_switchboard
//     intent).
//   - FormSpec gains additive events bindings (before_insert / after_update) that
//     round-trip through SaveForm/GetForm.
//   - SubmitForm fires the form's before_insert hook BEFORE the INSERT, and a
//     before_insert hook that vetoes (its macro errors) blocks the write (re-query
//     shows no row). A bound after_update hook's RunSQL side effect is observable
//     in DuckDB after a successful UpdateRow.
//
// These assertions FAIL against the current stubs (RunAutoExec is a no-op and the
// SubmitForm / UpdateRow paths do not yet fire event hooks) until P5.7 is
// implemented.

import (
	"context"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rowExists reports whether a customers row with the given id is present, used to
// prove a before_insert veto rolled the INSERT back.
func rowExists(t *testing.T, app *App, id int) bool {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = ?;`, id).Scan(&n))
	return n > 0
}

// auditCount counts rows in the audit table, used to prove an after_update hook's
// RunSQL side effect actually hit DuckDB.
func auditCount(t *testing.T, app *App) int {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM audit_log;`).Scan(&n))
	return n
}

// TestAutoExecRunOnStartup proves autoexec_macro persists in _vibe_meta and the
// startup binding runs it, returning its startup intent (open_switchboard).
func TestAutoExecRunOnStartup(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()

	// An AutoExec macro that opens the switchboard maximized on file open.
	autoexec := automation.MacroSpec{
		ID:    "autoexec",
		Title: "AutoExec",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionOpenSwitchboard, Args: map[string]string{
				"target": "main",
			}},
		},
	}
	require.NoError(t, app.SaveMacro(autoexec))

	// The AutoExec macro id is stored in _vibe_meta.
	require.NoError(t, app.container.SetMeta("autoexec_macro", "autoexec"))
	got, err := app.container.Meta("autoexec_macro")
	require.NoError(t, err)
	require.Equal(t, "autoexec", got, "autoexec_macro must persist in _vibe_meta")

	// The startup binding runs the AutoExec macro and returns its startup intents.
	res, err := app.RunAutoExec()
	require.NoError(t, err, "RunAutoExec must succeed when an autoexec_macro is configured")
	require.NotNil(t, res)
	assert.True(t, hasIntent(res.Intents, automation.IntentOpenSwitchboard, "main"),
		"RunAutoExec must return the AutoExec macro's open_switchboard startup intent, got: %+v", res.Intents)
}

// TestFormEventsRoundTrip proves the additive events bindings survive a
// SaveForm/GetForm round trip without disturbing existing forms.
func TestFormEventsRoundTrip(t *testing.T) {
	app := newOpenApp(t)

	// A form WITHOUT events must still round-trip (events stays nil/omitted).
	plain := schema.FormSpec{
		ID:        "plain_form",
		Title:     "Plain",
		Table:     "customers",
		Archetype: "columnar",
		Fields:    []schema.FormFieldSpec{{Name: "id"}, {Name: "name"}},
	}
	require.NoError(t, app.SaveForm(plain))
	gotPlain, err := app.GetForm("plain_form")
	require.NoError(t, err)
	require.NotNil(t, gotPlain)
	assert.Nil(t, gotPlain.Events, "a form with no events must round-trip with nil events")

	// A form WITH events must round-trip the bindings intact.
	withEvents := schema.FormSpec{
		ID:        "customer_form",
		Title:     "Customers",
		Table:     "customers",
		Archetype: "columnar",
		Fields:    []schema.FormFieldSpec{{Name: "id"}, {Name: "name"}},
		Events: &schema.FormEvents{
			OnOpen:       "log_open",
			BeforeInsert: "validate_customer",
			AfterUpdate:  "audit_update",
		},
	}
	require.NoError(t, app.SaveForm(withEvents))
	got, err := app.GetForm("customer_form")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Events, "events bindings must round-trip through GetForm")
	assert.Equal(t, "log_open", got.Events.OnOpen)
	assert.Equal(t, "validate_customer", got.Events.BeforeInsert)
	assert.Equal(t, "audit_update", got.Events.AfterUpdate)
}

// TestBeforeInsertVetoBlocksWrite proves a before_insert hook fires on the
// SubmitForm path and that a vetoing hook (its validating RunQuery errors) blocks
// the INSERT so no row is left behind.
func TestBeforeInsertVetoBlocksWrite(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name VARCHAR);`)
	require.NoError(t, err)

	// A before_insert macro that vetoes by running a query against a non-existent
	// table — the error must propagate and roll the SubmitForm INSERT back.
	veto := automation.MacroSpec{
		ID:    "veto_insert",
		Title: "Veto Insert",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunQuery, Args: map[string]string{
				"sql": "SELECT 1 FROM no_such_table",
			}},
		},
	}
	require.NoError(t, app.SaveMacro(veto))

	form := schema.FormSpec{
		ID:        "customer_form",
		Title:     "Customers",
		Table:     "customers",
		Archetype: "columnar",
		Fields:    []schema.FormFieldSpec{{Name: "id"}, {Name: "name"}},
		Events:    &schema.FormEvents{BeforeInsert: "veto_insert"},
	}
	require.NoError(t, app.SaveForm(form))

	// SubmitForm bound to this form must fire before_insert; the veto must surface
	// as an error and block the INSERT.
	err = app.SubmitFormFor("customer_form",
		FormSubmitRow{Table: "customers", Values: map[string]any{"id": 7, "name": "Blocked"}},
		nil)
	require.Error(t, err, "a before_insert veto must surface as an error from SubmitForm")
	assert.False(t, rowExists(t, app, 7),
		"a vetoed before_insert must roll the INSERT back — no customers row should exist")
}

// TestAfterUpdateHookSideEffect proves a bound after_update hook fires only after
// a successful UpdateRow and that its RunSQL side effect is observable in DuckDB.
func TestAfterUpdateHookSideEffect(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name VARCHAR);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE audit_log (note VARCHAR);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Original');`)
	require.NoError(t, err)

	// after_update macro logs to the audit table via RunSQL.
	audit := automation.MacroSpec{
		ID:    "audit_update",
		Title: "Audit Update",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunSQL, Args: map[string]string{
				"sql": "INSERT INTO audit_log (note) VALUES ('updated')",
			}},
		},
	}
	require.NoError(t, app.SaveMacro(audit))

	form := schema.FormSpec{
		ID:        "customer_form",
		Title:     "Customers",
		Table:     "customers",
		Archetype: "columnar",
		Fields:    []schema.FormFieldSpec{{Name: "id"}, {Name: "name"}},
		Events:    &schema.FormEvents{AfterUpdate: "audit_update"},
	}
	require.NoError(t, app.SaveForm(form))

	require.Equal(t, 0, auditCount(t, app))

	// A form-bound UpdateRow must fire after_update on success.
	_, err = app.UpdateRowFor("customer_form", "id", 1, map[string]any{"name": "Changed"})
	require.NoError(t, err, "UpdateRow on a form with an after_update hook must succeed")
	assert.Equal(t, 1, auditCount(t, app),
		"after_update RunSQL side effect must be observable in DuckDB after a successful update")

	// Sanity: the executor used by hooks is the live one (compile-time use).
	_ = context.Background()
	_ = automation.NewExecutor()
}
