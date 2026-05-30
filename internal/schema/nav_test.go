package schema_test

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNavStoreRoundTrip exercises the full P4.1 acceptance behavior end-to-end
// against a real .vibe container (SQLCipher + DuckDB):
//   - the additive _vibe_nav migration creates the table
//   - SetNav stores hierarchical switchboard items (parent_id nesting, action
//     kind, target id, sort_order)
//   - ListNav returns the full tree with correct parent linkage + ordering
//   - GetNav(page) returns only that page's direct children, ordered
//   - all action kinds (open_form/open_report/open_page/open_switchboard/
//     run_macro) round-trip
//   - the tree survives Save + Close + reopen
func TestNavStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nav.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Nav Test"),
	)
	require.NoError(t, err)

	// Migration creates _vibe_nav in the SQLCipher container.
	require.True(t, tableExists(t, c, "_vibe_nav"),
		"_vibe_nav table should exist after migration")

	store := schema.NewNavStore(c.SQLiteDB())

	// Root switchboard page with 3 items, deliberately inserted out of
	// sort_order to prove ordering is applied on read.
	root := []schema.NavItem{
		{ID: "root_orders", ParentID: "", Label: "Enter Orders", ActionKind: schema.NavOpenForm, TargetID: "orders_form", SortOrder: 0},
		{ID: "root_admin", ParentID: "", Label: "Admin", ActionKind: schema.NavOpenSwitchboard, TargetID: "admin_page", SortOrder: 2},
		{ID: "root_sales", ParentID: "", Label: "Sales Report", ActionKind: schema.NavOpenReport, TargetID: "sales_report", SortOrder: 1},
	}
	for _, it := range root {
		require.NoError(t, store.SetNav(it))
	}

	// Nested admin switchboard page (parent_id = root_admin) with 2 items,
	// covering the remaining action kinds.
	admin := []schema.NavItem{
		{ID: "admin_users", ParentID: "root_admin", Label: "Users Page", ActionKind: schema.NavOpenPage, TargetID: "users_page", SortOrder: 1},
		{ID: "admin_backup", ParentID: "root_admin", Label: "Run Backup", ActionKind: schema.NavRunMacro, TargetID: "backup_macro", SortOrder: 0},
	}
	for _, it := range admin {
		require.NoError(t, store.SetNav(it))
	}

	// GetNav("") returns only the 3 root items, ordered by sort_order.
	rootChildren, err := store.GetNav("")
	require.NoError(t, err)
	require.Len(t, rootChildren, 3, "GetNav(root) should return only the 3 root items")
	assert.Equal(t, []string{"root_orders", "root_sales", "root_admin"},
		[]string{rootChildren[0].ID, rootChildren[1].ID, rootChildren[2].ID},
		"root children must be ordered by sort_order")
	// Action kinds + targets round-trip on the root.
	assert.Equal(t, schema.NavOpenForm, rootChildren[0].ActionKind)
	assert.Equal(t, "orders_form", rootChildren[0].TargetID)
	assert.Equal(t, schema.NavOpenReport, rootChildren[1].ActionKind)
	assert.Equal(t, "sales_report", rootChildren[1].TargetID)
	assert.Equal(t, schema.NavOpenSwitchboard, rootChildren[2].ActionKind)
	assert.Equal(t, "admin_page", rootChildren[2].TargetID)

	// GetNav("root_admin") returns only the 2 nested items, ordered.
	adminChildren, err := store.GetNav("root_admin")
	require.NoError(t, err)
	require.Len(t, adminChildren, 2, "GetNav(admin) should return only that page's children")
	assert.Equal(t, "admin_backup", adminChildren[0].ID, "nested children must be ordered by sort_order")
	assert.Equal(t, schema.NavRunMacro, adminChildren[0].ActionKind)
	assert.Equal(t, "backup_macro", adminChildren[0].TargetID)
	assert.Equal(t, "admin_users", adminChildren[1].ID)
	assert.Equal(t, schema.NavOpenPage, adminChildren[1].ActionKind)
	assert.Equal(t, "users_page", adminChildren[1].TargetID)

	// ListNav returns the full tree (5 items) with parent linkage intact.
	all, err := store.ListNav()
	require.NoError(t, err)
	require.Len(t, all, 5, "ListNav should return every nav item across all pages")
	byID := map[string]schema.NavItem{}
	for _, it := range all {
		byID[it.ID] = it
	}
	assert.Equal(t, "", byID["root_orders"].ParentID, "root items have empty parent_id")
	assert.Equal(t, "root_admin", byID["admin_users"].ParentID, "nested items link to their parent switchboard")
	assert.Equal(t, "root_admin", byID["admin_backup"].ParentID)
	assert.Equal(t, 1, byID["admin_users"].SortOrder)
	assert.Equal(t, 0, byID["admin_backup"].SortOrder)

	// Upsert: re-saving an existing id updates in place (no duplicate row).
	upd := byID["root_orders"]
	upd.Label = "Enter Sales Orders"
	require.NoError(t, store.SetNav(upd))
	afterUpsert, err := store.ListNav()
	require.NoError(t, err)
	require.Len(t, afterUpsert, 5, "upsert must not create a duplicate nav row")

	// Persist across Save + Close + reopen.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	store2 := schema.NewNavStore(c2.SQLiteDB())
	reopened, err := store2.ListNav()
	require.NoError(t, err)
	require.Len(t, reopened, 5, "nav tree must persist across Save/Close/Open")

	reopenedRoot, err := store2.GetNav("")
	require.NoError(t, err)
	require.Len(t, reopenedRoot, 3)
	assert.Equal(t, "Enter Sales Orders", reopenedRoot[0].Label, "upserted label must survive reopen")
	assert.Equal(t, "admin_page", reopenedRoot[2].TargetID)
}

// TestNavStoreReplaceNavReconciles is the regression test for the
// remove-does-not-persist bug: SaveNav must reconcile the full nav set, not
// merely upsert. We seed 2 items, then ReplaceNav with one removed (the
// builder's "remove + save" path) and assert the removed item is gone after a
// fresh ListNav and that sort_order stays contiguous. A plain upsert loop would
// leave the removed row in place and fail this test.
func TestNavStoreReplaceNavReconciles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nav_replace.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	store := schema.NewNavStore(c.SQLiteDB())

	// Seed two root items.
	seed := []schema.NavItem{
		{ID: "nav_orders", ParentID: "", Label: "Orders", ActionKind: schema.NavOpenForm, TargetID: "orders_form", SortOrder: 0},
		{ID: "nav_sales", ParentID: "", Label: "Sales Report", ActionKind: schema.NavOpenReport, TargetID: "sales_report", SortOrder: 1},
	}
	require.NoError(t, store.ReplaceNav(seed))

	all, err := store.ListNav()
	require.NoError(t, err)
	require.Len(t, all, 2, "both seeded items should persist after ReplaceNav")

	// Remove "nav_orders" and renumber the survivor to 0 — exactly what the
	// AppShellView builder sends when an item is removed and saved.
	remaining := []schema.NavItem{
		{ID: "nav_sales", ParentID: "", Label: "Sales Report", ActionKind: schema.NavOpenReport, TargetID: "sales_report", SortOrder: 0},
	}
	require.NoError(t, store.ReplaceNav(remaining))

	after, err := store.ListNav()
	require.NoError(t, err)
	require.Len(t, after, 1, "removed nav item must NOT survive a reconcile save")
	assert.Equal(t, "nav_sales", after[0].ID, "the surviving item is the one still in the payload")
	assert.Equal(t, 0, after[0].SortOrder, "sort_order must be renumbered contiguous after removal")

	// The removed id is truly gone, verified via GetNav on the root page.
	rootChildren, err := store.GetNav("")
	require.NoError(t, err)
	require.Len(t, rootChildren, 1)
	for _, it := range rootChildren {
		assert.NotEqual(t, "nav_orders", it.ID, "removed item must not reappear on reload")
	}

	// Idempotent: re-running the same reconcile yields the same single row.
	require.NoError(t, store.ReplaceNav(remaining))
	again, err := store.ListNav()
	require.NoError(t, err)
	require.Len(t, again, 1, "ReplaceNav must be idempotent")

	// Removed deletion survives Save/Close/reopen.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	store2 := schema.NewNavStore(c2.SQLiteDB())
	reopened, err := store2.ListNav()
	require.NoError(t, err)
	require.Len(t, reopened, 1, "the removed nav item must stay gone across reopen")
	assert.Equal(t, "nav_sales", reopened[0].ID)
}

// TestNavMigrationNoClobber proves re-running runMigrations on a populated
// container is a no-op: the IF NOT EXISTS DDL must not drop or clobber existing
// _vibe_nav rows. We reopen a populated .vibe (which re-runs migrations) and
// assert the rows are intact.
func TestNavMigrationNoClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nav_noclobber.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)

	store := schema.NewNavStore(c.SQLiteDB())
	require.NoError(t, store.SetNav(schema.NavItem{
		ID: "root_only", ParentID: "", Label: "Only Item",
		ActionKind: schema.NavOpenForm, TargetID: "only_form", SortOrder: 0,
	}))

	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	// Reopen re-runs migrations against the populated container.
	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	store2 := schema.NewNavStore(c2.SQLiteDB())
	items, err := store2.ListNav()
	require.NoError(t, err)
	require.Len(t, items, 1, "re-running migrations must not clobber existing _vibe_nav rows")
	assert.Equal(t, "root_only", items[0].ID)
	assert.Equal(t, "only_form", items[0].TargetID)
}
