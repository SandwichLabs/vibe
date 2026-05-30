package main

// TDD RED — Phase 4 / unit P4.2: "Wails bindings: GetNav/SaveNav/ListNav +
// nav target catalog" (parity plan §4.2).
//
// This is a REAL integration test: it opens a real encrypted .vibe container
// (SQLCipher + DuckDB) and drives the production Wails-bound switchboard nav
// methods directly (no mocks), proving they persist through the P4.1 NavStore
// and that the nav-target catalog enumerates the bindable objects (forms,
// reports, pages) the switchboard builder UI can target.
//
// Acceptance behavior asserted:
//   - ListNavTargets returns forms + reports + pages, categorized
//   - SaveNav persists a switchboard of items; ListNav + GetNav(root) read it
//     back with the same structure (label, action_kind, target_id, sort_order)
//   - DeleteNavItem removes the right node (and the test documents the
//     child-cascade-vs-orphan behavior)
//   - every binding errors with "no file open" when no container is open

import (
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedNavTargets seeds one form, one report, and one page so ListNavTargets has
// a known catalog to enumerate.
func seedNavTargets(t *testing.T, app *App) (formID, reportID, pageID string) {
	t.Helper()

	// --- a form in _vibe_forms ---
	formID = "customers_form"
	require.NoError(t, app.SaveForm(schema.FormSpec{
		ID:        formID,
		Title:     "Customers",
		Table:     "customers",
		Archetype: "columnar",
		OnSubmit:  "refresh",
		Fields: []schema.FormFieldSpec{
			{Name: "id", Caption: "ID", ControlType: "text"},
			{Name: "name", Caption: "Name", ControlType: "text"},
		},
	}), "seed form")

	// --- a report in _vibe_reports ---
	reportID = "sales_by_rep"
	rstore := reports.NewReportStore(app.container.SQLiteDB())
	require.NoError(t, rstore.Save(reports.ReportSpec{
		ID:     reportID,
		Title:  "Sales by Rep",
		Source: "SELECT 1",
	}), "seed report")

	// --- a page in _vibe_pages ---
	var err error
	pageID, err = app.CreatePage("Welcome", "# Welcome")
	require.NoError(t, err, "seed page")

	return formID, reportID, pageID
}

// TestListNavTargets asserts the nav-target catalog enumerates all three
// bindable object categories from a real container.
func TestListNavTargets(t *testing.T) {
	app := newOpenApp(t)
	formID, reportID, pageID := seedNavTargets(t, app)

	cat, err := app.ListNavTargets()
	require.NoError(t, err, "ListNavTargets must read the bindable-object catalog")

	// The form must be present and categorized as a form target.
	assertHasTarget(t, cat, "open_form", formID)
	// The report must be present and categorized as a report target.
	assertHasTarget(t, cat, "open_report", reportID)
	// The page must be present and categorized as a page target.
	assertHasTarget(t, cat, "open_page", pageID)
}

// assertHasTarget finds a target with the given action kind + id in the catalog.
func assertHasTarget(t *testing.T, cat []NavTarget, kind, id string) {
	t.Helper()
	for _, tgt := range cat {
		if tgt.Kind == kind && tgt.ID == id {
			assert.NotEmpty(t, tgt.Label, "catalog target %s/%s must carry a human label", kind, id)
			return
		}
	}
	t.Fatalf("ListNavTargets must include a %s target with id %q; got %#v", kind, id, cat)
}

// TestSaveNavRoundTrip persists a 3-item switchboard and reads it back via
// ListNav + GetNav(root).
func TestSaveNavRoundTrip(t *testing.T) {
	app := newOpenApp(t)
	formID, reportID, pageID := seedNavTargets(t, app)

	items := []schema.NavItem{
		{ID: "nav-1", ParentID: "", Label: "Open Customers", ActionKind: schema.NavOpenForm, TargetID: formID, SortOrder: 0},
		{ID: "nav-2", ParentID: "", Label: "Sales Report", ActionKind: schema.NavOpenReport, TargetID: reportID, SortOrder: 1},
		{ID: "nav-3", ParentID: "", Label: "Welcome Page", ActionKind: schema.NavOpenPage, TargetID: pageID, SortOrder: 2},
	}
	require.NoError(t, app.SaveNav(items), "SaveNav must persist the whole switchboard")

	// ListNav returns every nav item.
	all, err := app.ListNav()
	require.NoError(t, err)
	require.Len(t, all, 3, "ListNav must return all 3 persisted items")

	// GetNav("") returns the root-level items in sort order.
	root, err := app.GetNav("")
	require.NoError(t, err)
	require.Len(t, root, 3, "GetNav(root) must return the 3 root switchboard items")
	assert.Equal(t, "nav-1", root[0].ID)
	assert.Equal(t, "Open Customers", root[0].Label)
	assert.Equal(t, schema.NavOpenForm, root[0].ActionKind)
	assert.Equal(t, formID, root[0].TargetID)
	assert.Equal(t, "nav-3", root[2].ID)
	assert.Equal(t, schema.NavOpenPage, root[2].ActionKind)
}

// TestSaveNavItemSingleUpsert asserts the single-item binding upserts one node.
func TestSaveNavItemSingleUpsert(t *testing.T) {
	app := newOpenApp(t)

	item := schema.NavItem{ID: "solo", Label: "Solo", ActionKind: schema.NavOpenPage, TargetID: "p1", SortOrder: 0}
	require.NoError(t, app.SaveNavItem(item), "SaveNavItem must upsert a single nav node")

	item.Label = "Renamed"
	require.NoError(t, app.SaveNavItem(item), "SaveNavItem must upsert (not duplicate)")

	all, err := app.ListNav()
	require.NoError(t, err)
	require.Len(t, all, 1, "upsert must not create a duplicate row")
	assert.Equal(t, "Renamed", all[0].Label)
}

// TestDeleteNavItem documents and asserts the delete semantics: deleting a
// parent removes that node; its children are documented as orphaned (the store
// does not cascade) — the test pins whichever behavior the implementation has.
func TestDeleteNavItem(t *testing.T) {
	app := newOpenApp(t)

	items := []schema.NavItem{
		{ID: "menu", ParentID: "", Label: "Reports Menu", ActionKind: schema.NavOpenSwitchboard, TargetID: "", SortOrder: 0},
		{ID: "child-a", ParentID: "menu", Label: "Report A", ActionKind: schema.NavOpenReport, TargetID: "ra", SortOrder: 0},
		{ID: "sibling", ParentID: "", Label: "Standalone", ActionKind: schema.NavOpenPage, TargetID: "p1", SortOrder: 1},
	}
	require.NoError(t, app.SaveNav(items))

	// Delete the standalone sibling — the targeted node must disappear and the
	// rest must remain intact.
	require.NoError(t, app.DeleteNavItem("sibling"), "DeleteNavItem must remove the targeted node")

	all, err := app.ListNav()
	require.NoError(t, err)
	for _, it := range all {
		assert.NotEqual(t, "sibling", it.ID, "the deleted node must be gone")
	}
	require.Len(t, all, 2, "only the targeted node may be removed")

	// Document cascade-vs-orphan: delete the parent "menu" and observe the child.
	require.NoError(t, app.DeleteNavItem("menu"))
	all, err = app.ListNav()
	require.NoError(t, err)

	var childStillPresent bool
	for _, it := range all {
		assert.NotEqual(t, "menu", it.ID, "parent node must be gone")
		if it.ID == "child-a" {
			childStillPresent = true
		}
	}
	// The P4.1 NavStore performs a single-row delete (no FK cascade), so the
	// child is orphaned rather than cascaded. Pin that behavior.
	assert.True(t, childStillPresent,
		"deleting a parent orphans (does not cascade) its children — single-row delete")
}

// TestNavBindingsNoFileOpen verifies every nav binding guards against a closed
// container, mirroring the existing GetForm/SaveForm guards.
func TestNavBindingsNoFileOpen(t *testing.T) {
	app := NewApp(testLogger())

	_, err := app.ListNavTargets()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	_, err = app.ListNav()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	_, err = app.GetNav("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	err = app.SaveNav([]schema.NavItem{{ID: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	err = app.SaveNavItem(schema.NavItem{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	err = app.DeleteNavItem("x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")
}
