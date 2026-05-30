package main

// Phase 5 / unit P5.7: "AutoExec + form/record event hooks" (parity plan §5.3).
//
// REAL integration test: opens a real encrypted .vibe container (SQLCipher +
// DuckDB) and asserts the P5.7 acceptance contract end-to-end on the SAME code
// paths the Studio UI uses (no mocks, real Executor, real DuckDB writes):
//
//   - AutoExec is executed on the ACTUAL open lifecycle: App.OpenFile runs the
//     configured autoexec_macro and emits its startup intents on the
//     "app:autoexec" event the App Shell consumes (open_switchboard).
//   - FormSpec gains additive events bindings that round-trip through
//     SaveForm/GetForm.
//   - SubmitFormFor (the binding the Vue FormView calls for a NEW record) fires
//     before_insert BEFORE the INSERT and after_insert only on success; a vetoing
//     before_insert blocks the write so no row is left behind.
//   - UpdateRowFor (the binding the Vue FormView calls for an EDIT) fires
//     before_update BEFORE the UPDATE and after_update only on success; a vetoing
//     before_update blocks the write.
//   - FireFormOpen (the binding FormView calls when a form opens) fires on_open
//     then on_load and surfaces their UI intents.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rowExists reports whether a customers row with the given id is present, used to
// prove a before_* veto blocked the write.
func rowExists(t *testing.T, app *App, id int) bool {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = ?;`, id).Scan(&n))
	return n > 0
}

// nameOf returns the current name for a customers row, used to prove a vetoed
// before_update left the row unchanged.
func nameOf(t *testing.T, app *App, id int) string {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var name string
	require.NoError(t, ddb.QueryRow(`SELECT name FROM customers WHERE id = ?;`, id).Scan(&name))
	return name
}

// auditCount counts rows in the audit table, used to prove an after_* hook's
// RunSQL side effect actually hit DuckDB.
func auditCount(t *testing.T, app *App) int {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM audit_log;`).Scan(&n))
	return n
}

// emittedEvent is one event captured from the App's emitter sink during a test.
type emittedEvent struct {
	name string
	data []any
}

// withEventSink installs a recording emitter so the open lifecycle's
// runtime.EventsEmit calls (file:opened, app:autoexec) can be asserted without a
// live Wails runtime. It returns a pointer to the captured slice.
func withEventSink(app *App) *[]emittedEvent {
	var events []emittedEvent
	app.emit = func(name string, data ...any) {
		events = append(events, emittedEvent{name: name, data: data})
	}
	return &events
}

// TestAutoExecRunOnOpenLifecycle proves the AutoExec macro is executed on the
// ACTUAL file-open path (App.OpenFile), emitting its open_switchboard startup
// intent on the "app:autoexec" event the App Shell consumes — not merely when
// RunAutoExec is invoked in isolation.
func TestAutoExecRunOnOpenLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autoexec.vibe")

	// Stage 1: create a container at `path`, save an AutoExec macro that opens the
	// switchboard maximized on file open, point _vibe_meta at it, persist + close
	// so it can be reopened through the real lifecycle.
	{
		setup := NewApp(testLogger())
		setup.ctx = context.Background()
		setup.emit = func(string, ...any) {} // swallow lifecycle events in setup
		c, err := container.Create(path,
			container.WithPassphrase(testPassphrase),
			container.WithLogger(testLogger()),
			container.WithTitle("AutoExec Test"),
		)
		require.NoError(t, err)
		setup.container = c
		setup.filePath = path
		setup.initModeFromMeta()

		autoexec := automation.MacroSpec{
			ID:    "autoexec",
			Title: "AutoExec",
			Actions: []automation.MacroAction{
				{Kind: automation.ActionOpenSwitchboard, Args: map[string]string{"target": "main"}},
			},
		}
		require.NoError(t, setup.SaveMacro(autoexec))
		require.NoError(t, c.SetMeta("autoexec_macro", "autoexec"))
		require.NoError(t, c.Save())
		require.NoError(t, c.Close())
	}

	// Stage 2: open that file through the REAL App.OpenFile lifecycle with an
	// event sink, then assert AutoExec fired and emitted the open_switchboard
	// startup intent on "app:autoexec".
	app := NewApp(testLogger())
	app.ctx = context.Background()
	events := withEventSink(app)

	require.NoError(t, app.OpenFile(path, testPassphrase),
		"OpenFile must run the AutoExec macro on the real open lifecycle")
	t.Cleanup(func() { _ = app.container.Close() })

	// The open lifecycle must have emitted both file:opened and app:autoexec.
	var autoexecIntents []MacroIntent
	var sawFileOpened bool
	for _, ev := range *events {
		switch ev.name {
		case "file:opened":
			sawFileOpened = true
		case "app:autoexec":
			require.Len(t, ev.data, 1, "app:autoexec must carry the intent list")
			ints, ok := ev.data[0].([]MacroIntent)
			require.True(t, ok, "app:autoexec payload must be []MacroIntent, got %T", ev.data[0])
			autoexecIntents = ints
		}
	}
	require.True(t, sawFileOpened, "OpenFile must emit file:opened")
	require.NotNil(t, autoexecIntents, "OpenFile must emit app:autoexec when an autoexec_macro is configured")
	assert.True(t, hasIntent(autoexecIntents, automation.IntentOpenSwitchboard, "main"),
		"AutoExec on open must surface the open_switchboard startup intent, got: %+v", autoexecIntents)

	// And the exported binding the App Shell calls on file:opened returns the same.
	res, err := app.RunAutoExec()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, hasIntent(res.Intents, automation.IntentOpenSwitchboard, "main"),
		"RunAutoExec must return the same open_switchboard startup intent")
}

// TestAutoExecNoMacroIsNoOp proves opening a file with no autoexec_macro
// configured does not emit app:autoexec (no startup intents to apply).
func TestAutoExecNoMacroIsNoOp(t *testing.T) {
	app := newOpenApp(t)
	events := withEventSink(app)
	app.emitAutoExec()
	for _, ev := range *events {
		assert.NotEqual(t, "app:autoexec", ev.name,
			"no autoexec_macro configured must not emit app:autoexec")
	}
	res, err := app.RunAutoExec()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Empty(t, res.Intents, "RunAutoExec with no autoexec_macro returns no intents")
}

// TestFormEventsRoundTrip proves the additive events bindings survive a
// SaveForm/GetForm round trip without disturbing existing forms.
func TestFormEventsRoundTrip(t *testing.T) {
	app := newOpenApp(t)

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

	withEvents := schema.FormSpec{
		ID:        "customer_form",
		Title:     "Customers",
		Table:     "customers",
		Archetype: "columnar",
		Fields:    []schema.FormFieldSpec{{Name: "id"}, {Name: "name"}},
		Events: &schema.FormEvents{
			OnOpen:       "log_open",
			OnLoad:       "log_load",
			BeforeInsert: "validate_customer",
			AfterInsert:  "audit_insert",
			BeforeUpdate: "validate_update",
			AfterUpdate:  "audit_update",
		},
	}
	require.NoError(t, app.SaveForm(withEvents))
	got, err := app.GetForm("customer_form")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Events, "events bindings must round-trip through GetForm")
	assert.Equal(t, "log_open", got.Events.OnOpen)
	assert.Equal(t, "log_load", got.Events.OnLoad)
	assert.Equal(t, "validate_customer", got.Events.BeforeInsert)
	assert.Equal(t, "audit_insert", got.Events.AfterInsert)
	assert.Equal(t, "validate_update", got.Events.BeforeUpdate)
	assert.Equal(t, "audit_update", got.Events.AfterUpdate)
}

// formTestApp opens an app, creates the customers + audit_log tables, and saves
// a customer_form bound to the given events. It is the shared fixture for the
// write-path hook tests.
func formTestApp(t *testing.T, events *schema.FormEvents) *App {
	t.Helper()
	app := newOpenApp(t)
	app.initModeFromMeta()

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name VARCHAR);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE audit_log (note VARCHAR);`)
	require.NoError(t, err)

	form := schema.FormSpec{
		ID:        "customer_form",
		Title:     "Customers",
		Table:     "customers",
		Archetype: "columnar",
		Fields:    []schema.FormFieldSpec{{Name: "id"}, {Name: "name"}},
		Events:    events,
	}
	require.NoError(t, app.SaveForm(form))
	return app
}

// vetoMacro is a macro that errors (vetoes) by querying a non-existent table.
func vetoMacro(id string) automation.MacroSpec {
	return automation.MacroSpec{
		ID:    id,
		Title: id,
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunQuery, Args: map[string]string{"sql": "SELECT 1 FROM no_such_table"}},
		},
	}
}

// auditMacro is a macro that logs to the audit table via RunSQL (a successful
// side effect we can observe in DuckDB).
func auditMacro(id string) automation.MacroSpec {
	return automation.MacroSpec{
		ID:    id,
		Title: id,
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunSQL, Args: map[string]string{"sql": "INSERT INTO audit_log (note) VALUES ('hooked')"}},
		},
	}
}

// TestBeforeInsertVetoBlocksWrite proves a before_insert hook fires on the REAL
// SubmitFormFor path (the binding FormView calls for a new record) and a vetoing
// hook blocks the INSERT so no row is left behind.
func TestBeforeInsertVetoBlocksWrite(t *testing.T) {
	app := formTestApp(t, &schema.FormEvents{BeforeInsert: "veto_insert"})
	require.NoError(t, app.SaveMacro(vetoMacro("veto_insert")))

	err := app.SubmitFormFor("customer_form",
		FormSubmitRow{Table: "customers", Values: map[string]any{"id": 7, "name": "Blocked"}},
		nil)
	require.Error(t, err, "a before_insert veto must surface as an error from SubmitFormFor")
	assert.False(t, rowExists(t, app, 7),
		"a vetoed before_insert must block the INSERT — no customers row should exist")
}

// TestAfterInsertHookSideEffect proves a bound after_insert hook fires only after
// a successful SubmitFormFor and that its RunSQL side effect is observable.
func TestAfterInsertHookSideEffect(t *testing.T) {
	app := formTestApp(t, &schema.FormEvents{AfterInsert: "audit_insert"})
	require.NoError(t, app.SaveMacro(auditMacro("audit_insert")))

	require.Equal(t, 0, auditCount(t, app))
	err := app.SubmitFormFor("customer_form",
		FormSubmitRow{Table: "customers", Values: map[string]any{"id": 3, "name": "Inserted"}},
		nil)
	require.NoError(t, err)
	assert.True(t, rowExists(t, app, 3), "the row must be inserted on success")
	assert.Equal(t, 1, auditCount(t, app),
		"after_insert RunSQL side effect must be observable after a successful submit")
}

// TestBeforeUpdateVetoBlocksWrite proves a before_update hook fires on the REAL
// UpdateRowFor path (the binding FormView calls for an edit) and a vetoing hook
// blocks the UPDATE, leaving the row unchanged.
func TestBeforeUpdateVetoBlocksWrite(t *testing.T) {
	app := formTestApp(t, &schema.FormEvents{BeforeUpdate: "veto_update"})
	require.NoError(t, app.SaveMacro(vetoMacro("veto_update")))

	ddb := app.container.DuckDBDB()
	_, err := ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Original');`)
	require.NoError(t, err)

	_, err = app.UpdateRowFor("customer_form", "id", 1, map[string]any{"name": "Changed"})
	require.Error(t, err, "a before_update veto must surface as an error from UpdateRowFor")
	assert.Equal(t, "Original", nameOf(t, app, 1),
		"a vetoed before_update must block the UPDATE — the row must be unchanged")
}

// TestAfterUpdateHookSideEffect proves a bound after_update hook fires only after
// a successful UpdateRowFor and that its RunSQL side effect is observable.
func TestAfterUpdateHookSideEffect(t *testing.T) {
	app := formTestApp(t, &schema.FormEvents{AfterUpdate: "audit_update"})
	require.NoError(t, app.SaveMacro(auditMacro("audit_update")))

	ddb := app.container.DuckDBDB()
	_, err := ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Original');`)
	require.NoError(t, err)

	require.Equal(t, 0, auditCount(t, app))
	_, err = app.UpdateRowFor("customer_form", "id", 1, map[string]any{"name": "Changed"})
	require.NoError(t, err, "UpdateRowFor on a form with an after_update hook must succeed")
	assert.Equal(t, "Changed", nameOf(t, app, 1), "the update must be applied")
	assert.Equal(t, 1, auditCount(t, app),
		"after_update RunSQL side effect must be observable after a successful update")
}

// TestUpdateRowForResolvesBoundTable proves UpdateRowFor resolves the form's
// bound table from its spec (not the formID), so the write hits the real table —
// covering the removal of the former table4Form stub.
func TestUpdateRowForResolvesBoundTable(t *testing.T) {
	app := formTestApp(t, nil)
	ddb := app.container.DuckDBDB()
	_, err := ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Original');`)
	require.NoError(t, err)

	// formID "customer_form" != table "customers"; the write must still land in
	// customers because the bound table is resolved from the spec.
	res, err := app.UpdateRowFor("customer_form", "id", 1, map[string]any{"name": "Resolved"})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.Affected)
	assert.Equal(t, "Resolved", nameOf(t, app, 1))
}

// TestFireFormOpenRunsOnOpenThenOnLoad proves FireFormOpen fires the on_open then
// on_load hooks and surfaces their UI intents to the shell (Access Form_Open /
// Form_Load), exercising the open-event path FormView wires.
func TestFireFormOpenRunsOnOpenThenOnLoad(t *testing.T) {
	app := formTestApp(t, &schema.FormEvents{OnOpen: "open_macro", OnLoad: "load_macro"})

	// on_open opens a filtered form; on_load raises a msg_box. Both must surface.
	require.NoError(t, app.SaveMacro(automation.MacroSpec{
		ID:    "open_macro",
		Title: "On Open",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionOpenForm, Args: map[string]string{"target": "orders"}},
		},
	}))
	require.NoError(t, app.SaveMacro(automation.MacroSpec{
		ID:    "load_macro",
		Title: "On Load",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionMsgBox, Args: map[string]string{"message": "loaded"}},
		},
	}))

	res, err := app.FireFormOpen("customer_form")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, hasIntent(res.Intents, automation.IntentOpenForm, "orders"),
		"on_open must surface its open_form intent, got: %+v", res.Intents)
	assert.True(t, hasIntent(res.Intents, automation.IntentMsgBox, ""),
		"on_load must surface its msg_box intent, got: %+v", res.Intents)

	// A form with no open hooks yields an empty result (no-op).
	require.NoError(t, app.SaveForm(schema.FormSpec{
		ID: "no_hooks", Title: "No Hooks", Table: "customers", Archetype: "columnar",
		Fields: []schema.FormFieldSpec{{Name: "id"}},
	}))
	empty, err := app.FireFormOpen("no_hooks")
	require.NoError(t, err)
	require.NotNil(t, empty)
	assert.Empty(t, empty.Intents, "a form with no on_open/on_load hooks fires nothing")
}
