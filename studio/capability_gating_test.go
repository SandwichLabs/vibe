package main

// TDD RED — Phase 4 / unit P4.4: "Capability gating: deny privileged app.go
// methods + force agent sandbox in App Mode" (parity plan §4.3, risk §8).
//
// REAL integration test: opens a real encrypted .vibe container (SQLCipher +
// DuckDB), flips the live runtime mode to "app" via the production SetMode
// binding, and asserts the App-Mode lockdown contract end-to-end (no mocks):
//
//   - every privileged schema/SQL/page/definition/import binding returns the
//     capability-denied error AND performs no mutation
//   - App-Mode-permitted data-entry (SubmitForm/InsertRow/UpdateRow/DeleteRow),
//     report render, and nav read remain allowed
//   - the agent sandbox is forced to at most read-write in App Mode: a
//     schema-category tool (create_table) is rejected by the sandbox while a
//     data-category tool path is permitted, and ModeFull is never granted
//   - returning to Studio Mode via the unlock entrypoint re-enables the
//     privileged methods
//
// These assertions FAIL against the current stubs (guardStudioOnly is a no-op
// and agentSandboxForMode always returns ModeFull) until P4.4 is implemented.

import (
	"errors"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isCapabilityDenied reports whether err is the App-Mode capability-denied
// error (matched via errors.Is on the production sentinel).
func isCapabilityDenied(err error) bool {
	return err != nil && errors.Is(err, errCapabilityDenied)
}

// userTableCount counts non-_vibe_ user tables in DuckDB. Used to prove that a
// denied CreateTable/DropTable/ImportCSV made no mutation.
func userTableCount(t *testing.T, app *App) int {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	var n int
	require.NoError(t, ddb.QueryRow(
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = 'main' AND table_name NOT LIKE '_vibe_%';`).Scan(&n))
	return n
}

// TestAppModeCapabilityGating is the headline P4.4 acceptance test.
func TestAppModeCapabilityGating(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()
	createOrderEntrySchema(t, app) // orders + order_items (see forms_test.go)

	// Seed a header row so UpdateRow/DeleteRow have something to act on in App Mode.
	_, err := app.InsertRow("orders", map[string]any{"id": "1", "customer_id": "1", "order_date": "2026-05-30"})
	require.NoError(t, err, "seed an orders row in Studio Mode")

	// Save a form definition and a nav item while still in Studio Mode (these are
	// definition edits and must be denied once we flip to App Mode).
	require.NoError(t, app.SaveForm(orderEntryFormSpec()))

	baseTables := userTableCount(t, app)

	// --- flip to App Mode ---
	require.NoError(t, app.SetMode("app"))
	cfg, err := app.GetAppConfig()
	require.NoError(t, err)
	require.Equal(t, "app", cfg.Mode)

	// --- privileged bindings must be denied and make no mutation ---

	// CreateTable
	err = app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}})
	assert.True(t, isCapabilityDenied(err), "CreateTable must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseTables, userTableCount(t, app), "CreateTable must not create a table in App Mode")

	// DropTable
	err = app.DropTable("order_items")
	assert.True(t, isCapabilityDenied(err), "DropTable must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseTables, userTableCount(t, app), "DropTable must not drop a table in App Mode")

	// AddForeignKey
	_, err = app.AddForeignKey(ForeignKeySpec{
		ChildTable: "order_items", ChildColumn: "order_id",
		ParentTable: "orders", ParentColumn: "id",
	})
	assert.True(t, isCapabilityDenied(err), "AddForeignKey must be capability-denied in App Mode, got: %v", err)

	// ExecuteQuery (raw SQL) — even a benign SELECT is a Studio-only surface.
	_, err = app.ExecuteQuery("SELECT 1")
	assert.True(t, isCapabilityDenied(err), "ExecuteQuery (raw SQL) must be capability-denied in App Mode, got: %v", err)

	// ExecuteQuery must not mutate either: a DROP via raw SQL must be refused.
	_, err = app.ExecuteQuery("DROP TABLE order_items")
	assert.True(t, isCapabilityDenied(err), "ExecuteQuery DROP must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseTables, userTableCount(t, app), "ExecuteQuery DROP must not drop a table in App Mode")

	// DeletePage (page editing) — create a page in Studio Mode first via a direct
	// insert is not available; instead prove the binding is gated.
	err = app.DeletePage("nonexistent-page-id")
	assert.True(t, isCapabilityDenied(err), "DeletePage must be capability-denied in App Mode, got: %v", err)

	// ImportCSV
	_, err = app.ImportCSV("/tmp/does-not-exist.csv", "imported")
	assert.True(t, isCapabilityDenied(err),
		"ImportCSV must be capability-denied in App Mode before touching the filesystem, got: %v", err)
	assert.Equal(t, baseTables, userTableCount(t, app), "ImportCSV must not create a table in App Mode")

	// SaveForm (definition edit)
	err = app.SaveForm(orderEntryFormSpec())
	assert.True(t, isCapabilityDenied(err), "SaveForm must be capability-denied in App Mode, got: %v", err)

	// SaveNav (definition edit)
	err = app.SaveNav(nil)
	assert.True(t, isCapabilityDenied(err), "SaveNav must be capability-denied in App Mode, got: %v", err)

	// --- agent sandbox is forced to at most read-write in App Mode ---
	// (Asserted before the permitted-op require()s so it is always exercised,
	// independent of the bound-table data path.)
	sb := agentSandboxForMode("app")
	require.NotNil(t, sb)
	assert.NotEqual(t, agents.ModeFull, sb.Mode, "ModeFull must never be granted in App Mode")
	assert.Equal(t, agents.ModeReadWrite, sb.Mode, "App Mode agent sandbox must be capped at read-write")

	// A schema-category tool must be rejected by the App-Mode sandbox.
	assert.Error(t, sb.Check("create_table"),
		"schema-category agent tool (create_table) must be denied by the App-Mode sandbox")
	// A data-category tool path must be permitted.
	assert.NoError(t, sb.Check("run_query"),
		"data-category agent tool (run_query) must be permitted by the App-Mode sandbox")
	// And introspection stays allowed.
	assert.NoError(t, sb.Check("introspect_schema"),
		"introspect agent tool must be permitted by the App-Mode sandbox")

	// In Studio Mode the agent sandbox may grant full access.
	sbStudio := agentSandboxForMode("studio")
	require.NotNil(t, sbStudio)
	assert.Equal(t, agents.ModeFull, sbStudio.Mode, "Studio Mode agent sandbox grants full access")

	// --- App-Mode-permitted operations must still succeed ---

	// InsertRow on a bound table.
	_, err = app.InsertRow("orders", map[string]any{"id": "2", "customer_id": "2", "order_date": "2026-05-31"})
	assert.NoError(t, err, "InsertRow must remain allowed in App Mode")

	// UpdateRow on a bound table.
	_, err = app.UpdateRow("orders", "id", "2", map[string]any{"customer_id": "9"})
	assert.NoError(t, err, "UpdateRow must remain allowed in App Mode")

	// SubmitForm (master-detail data entry).
	assert.NoError(t, app.SubmitForm(
		FormSubmitRow{Table: "orders", Values: map[string]any{"id": "3", "customer_id": "3", "order_date": "2026-06-01"}},
		[]FormSubmitRow{{Table: "order_items", Values: map[string]any{"id": "1", "order_id": "3", "product": "W", "qty": "1", "price": "1.00"}}},
	), "SubmitForm data entry must remain allowed in App Mode")

	// DeleteRow on a bound table.
	_, err = app.DeleteRow("orders", "id", "2")
	assert.NoError(t, err, "DeleteRow must remain allowed in App Mode")

	// Nav read remains allowed.
	_, err = app.ListNav()
	assert.NoError(t, err, "ListNav (read) must remain allowed in App Mode")

	// Form render read remains allowed.
	gotForm, err := app.GetForm("order_entry")
	assert.NoError(t, err, "GetForm (render read) must remain allowed in App Mode")
	assert.NotNil(t, gotForm)

	// RenderReportContent for a missing report may error, but NOT with a
	// capability-denied error — report rendering is permitted in App Mode.
	_, err = app.RenderReportContent("nonexistent-report")
	assert.False(t, isCapabilityDenied(err),
		"RenderReportContent must NOT be capability-denied in App Mode, got: %v", err)

	// --- returning to Studio Mode re-enables privileged methods ---
	require.NoError(t, app.SetMode("studio"), "unlock back to Studio Mode")

	err = app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}})
	require.NoError(t, err, "CreateTable must work again after returning to Studio Mode")
	assert.Equal(t, baseTables+1, userTableCount(t, app), "the widgets table was created in Studio Mode")
}
