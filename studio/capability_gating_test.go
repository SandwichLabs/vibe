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
	"github.com/sandwich-labs/vibe-app/internal/schema"
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

// queryCount counts named queries in the registry. Used to prove a denied
// SaveQuery/SaveFilterAsQuery/DeleteQuery made no mutation.
func queryCount(t *testing.T, app *App) int {
	t.Helper()
	qs, err := app.ListQueries()
	require.NoError(t, err)
	return len(qs)
}

// navCount counts nav items in the switchboard store. Used to prove a denied
// SaveNavItem/DeleteNavItem made no mutation.
func navCount(t *testing.T, app *App) int {
	t.Helper()
	items, err := app.ListNav()
	require.NoError(t, err)
	return len(items)
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

	// Seed a page, a named query, and a nav item in Studio Mode so the denied
	// page-edit / query / nav bindings have an existing target and we can prove
	// no mutation against a stable baseline.
	seedPageID, err := app.CreatePage("Seed Page", "# seed")
	require.NoError(t, err, "seed a page in Studio Mode")
	require.NoError(t, app.SaveQuery("seed-query", "Seed Query", "SELECT 1", ""), "seed a named query in Studio Mode")
	require.NoError(t, app.SaveNavItem(schema.NavItem{ID: "seed-nav", Label: "Seed", ActionKind: "open_form", TargetID: "order_entry"}), "seed a nav item in Studio Mode")

	baseTables := userTableCount(t, app)
	baseQueries := queryCount(t, app)
	baseNav := navCount(t, app)

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

	// SendAgentQuery (raw SQL — the most dangerous DDL/DML surface). It runs
	// arbitrary SQL via a.engine.Execute and must be gated exactly like
	// ExecuteQuery; a benign SELECT is denied and a DROP must make no mutation.
	_, err = app.SendAgentQuery("SELECT 1")
	assert.True(t, isCapabilityDenied(err), "SendAgentQuery (raw SQL) must be capability-denied in App Mode, got: %v", err)

	_, err = app.SendAgentQuery("DROP TABLE order_items")
	assert.True(t, isCapabilityDenied(err), "SendAgentQuery DROP must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseTables, userTableCount(t, app), "SendAgentQuery DROP must not drop a table in App Mode")

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

	// CreatePage (page edit — must not write _vibe_pages)
	pageCountSQL := func() int {
		var n int
		require.NoError(t, app.container.SQLiteDB().QueryRow("SELECT COUNT(*) FROM _vibe_pages").Scan(&n))
		return n
	}
	basePages := pageCountSQL()
	_, err = app.CreatePage("Should Not Exist", "# nope")
	assert.True(t, isCapabilityDenied(err), "CreatePage must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, basePages, pageCountSQL(), "CreatePage must not write _vibe_pages in App Mode")

	// CreateHTMLPage (page edit)
	_, err = app.CreateHTMLPage("Should Not Exist HTML", "<p>nope</p>")
	assert.True(t, isCapabilityDenied(err), "CreateHTMLPage must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, basePages, pageCountSQL(), "CreateHTMLPage must not write _vibe_pages in App Mode")

	// UpdatePage (page edit — must not mutate the seeded page)
	err = app.UpdatePage(seedPageID, "Hacked Title", "# hacked")
	assert.True(t, isCapabilityDenied(err), "UpdatePage must be capability-denied in App Mode, got: %v", err)
	{
		var gotTitle string
		require.NoError(t, app.container.SQLiteDB().
			QueryRow("SELECT title FROM _vibe_pages WHERE id = ?", seedPageID).Scan(&gotTitle))
		assert.Equal(t, "Seed Page", gotTitle, "UpdatePage must not mutate the seeded page in App Mode")
	}

	// SaveQuery (SQL/definition surface — must not add a named query)
	err = app.SaveQuery("", "Injected Query", "SELECT 42", "")
	assert.True(t, isCapabilityDenied(err), "SaveQuery must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseQueries, queryCount(t, app), "SaveQuery must not add a named query in App Mode")

	// SaveFilterAsQuery (SQL/definition surface — must not add a named query)
	_, err = app.SaveFilterAsQuery("orders", FilterSpec{}, nil, "Injected Filter Query")
	assert.True(t, isCapabilityDenied(err), "SaveFilterAsQuery must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseQueries, queryCount(t, app), "SaveFilterAsQuery must not add a named query in App Mode")

	// DeleteQuery (SQL/definition surface — must not remove the seeded query)
	err = app.DeleteQuery("seed-query")
	assert.True(t, isCapabilityDenied(err), "DeleteQuery must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseQueries, queryCount(t, app), "DeleteQuery must not remove a named query in App Mode")

	// RemoveForeignKey (schema edit)
	err = app.RemoveForeignKey("nonexistent-fk-id")
	assert.True(t, isCapabilityDenied(err), "RemoveForeignKey must be capability-denied in App Mode, got: %v", err)

	// SetMeta (definition/metadata edit — must not flip default_mode/app_title).
	// app_title is unset in this fixture; a denied SetMeta must leave it unset
	// (Meta returns a not-found error) rather than writing "Pwned".
	err = app.SetMeta("app_title", "Pwned")
	assert.True(t, isCapabilityDenied(err), "SetMeta must be capability-denied in App Mode, got: %v", err)
	{
		got, _ := app.container.Meta("app_title")
		assert.NotEqual(t, "Pwned", got, "SetMeta must not change app_title in App Mode")
	}

	// SetFieldMeta (definition/metadata edit)
	err = app.SetFieldMeta("orders", "customer_id", schema.FieldMeta{Caption: "Hacked"})
	assert.True(t, isCapabilityDenied(err), "SetFieldMeta must be capability-denied in App Mode, got: %v", err)

	// SaveNavItem (nav definition edit — must not change the nav store)
	err = app.SaveNavItem(schema.NavItem{ID: "injected-nav", Label: "Injected", ActionKind: "open_form", TargetID: "order_entry"})
	assert.True(t, isCapabilityDenied(err), "SaveNavItem must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseNav, navCount(t, app), "SaveNavItem must not add a nav item in App Mode")

	// DeleteNavItem (nav definition edit — must not remove the seeded nav item)
	err = app.DeleteNavItem("seed-nav")
	assert.True(t, isCapabilityDenied(err), "DeleteNavItem must be capability-denied in App Mode, got: %v", err)
	assert.Equal(t, baseNav, navCount(t, app), "DeleteNavItem must not remove a nav item in App Mode")

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

	// --- live mid-session downgrade restarts the MCP server with a capped sandbox ---
	// Start the MCP server while in Studio Mode (ModeFull), then flip to App Mode and
	// confirm the running server was restarted (its handler must not retain ModeFull).
	require.NoError(t, app.SetMode("studio"))
	studioURL, err := app.StartMCPServer()
	require.NoError(t, err, "start MCP server in Studio Mode")
	require.NotEmpty(t, studioURL)
	require.True(t, app.GetMCPStatus()["running"].(bool), "MCP server should be running")

	require.NoError(t, app.SetMode("app"))
	appURL := app.GetMCPStatus()["url"].(string)
	require.True(t, app.GetMCPStatus()["running"].(bool), "MCP server should still be running after mode flip")
	assert.NotEqual(t, studioURL, appURL,
		"flipping Studio->App must restart the MCP server (fresh address) so the handler sandbox is re-derived and never retains ModeFull")
	require.NoError(t, app.StopMCPServer())
	require.NoError(t, app.SetMode("app")) // restore App Mode for the permitted-op assertions below

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
