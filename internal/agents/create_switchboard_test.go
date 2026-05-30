package agents_test

// P4.5 — Agent Switchboard Wizard tool (plan §4.4 / §4.3).
//
// INTEGRATION test: stands up the real agent Handler over a real .vibe
// container (SQLCipher) + real DuckDB engine, and drives the new
// create_switchboard wizard tool through the JSON-RPC tool surface
// (Handler.Handle) — NOT by calling any Go backend method directly, mirroring
// wizard_tools_test.go / create_report_test.go.
//
// Acceptance behavior asserted here:
//   - create_switchboard is a registered agent tool (catalog + schema) in the
//     gated "schema" category, so it is denied in App Mode (read-only /
//     read-write) by the existing sandbox.
//   - create_switchboard, dispatched through Handler.Handle against a real
//     .vibe + DuckDB, validates each item target against the nav-target catalog
//     (forms + reports + switchboard pages) and persists a hierarchical nav
//     structure to _vibe_nav (readable via the P4.1 NavStore) with the correct
//     action_kind / target_id per item.
//   - It rejects bindings to nonexistent targets.
//   - It enforces the Access ≤8-items-per-page constraint.
//   - It is denied with a permission error under a read-write sandbox (App-Mode
//     gating).

import (
	"context"
	"fmt"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSwitchboardTargets seeds a form (orders_form) and a report (sales_report)
// into the container so create_switchboard has valid catalog targets to bind to.
func seedSwitchboardTargets(t *testing.T, c *container.Container) {
	t.Helper()
	db := c.SQLiteDB()

	require.NoError(t, schema.NewFormStore(db).SaveForm(schema.FormSpec{
		ID:        "orders_form",
		Title:     "Orders",
		Table:     "orders",
		Archetype: "columnar",
		OnSubmit:  "refresh",
		Fields:    []schema.FormFieldSpec{{Name: "order_id"}, {Name: "total"}},
	}))

	require.NoError(t, reports.NewReportStore(db).Save(reports.ReportSpec{
		ID:     "sales_report",
		Title:  "Sales Report",
		Source: "orders",
		Detail: reports.DetailBand{Columns: []reports.ReportColumn{{Field: "order_id"}}},
	}))
}

// switchboardItems returns the bound items for the Main Menu: a form, a report,
// and a nested switchboard page (an open_switchboard item pointing at a child
// switchboard page id).
func switchboardItems(nestedPageID string) []any {
	return []any{
		map[string]any{"label": "Enter Orders", "action": schema.NavOpenForm, "target": "orders_form"},
		map[string]any{"label": "Sales Report", "action": schema.NavOpenReport, "target": "sales_report"},
		map[string]any{"label": "Admin", "action": schema.NavOpenSwitchboard, "target": nestedPageID},
	}
}

// TestCreateSwitchboardTool_RegisteredWithSchema asserts create_switchboard is
// discoverable on the tool surface with a documented schema in the gated
// "schema" category.
func TestCreateSwitchboardTool_RegisteredWithSchema(t *testing.T) {
	tool := agents.GetTool("create_switchboard")
	require.NotNil(t, tool, "create_switchboard must be registered as an agent tool")
	assert.NotEmpty(t, tool.Description, "create_switchboard must have a documented description")
	assert.Equal(t, "schema", tool.Category,
		"create_switchboard must be in the gated 'schema' category (denied in App Mode)")

	found := false
	for _, td := range agents.ListTools() {
		if td.Name == "create_switchboard" {
			found = true
			break
		}
	}
	assert.True(t, found, "create_switchboard must appear in ListTools()")

	byName := map[string]agents.ParamDef{}
	for _, p := range tool.Params {
		byName[p.Name] = p
	}
	require.Contains(t, byName, "title", "create_switchboard must document a 'title' param")
	require.Contains(t, byName, "items", "create_switchboard must document an 'items' param")
}

// TestCreateSwitchboardTool_PersistsHierarchicalNav drives create_switchboard
// through the JSON-RPC surface against a real .vibe + DuckDB, then asserts a
// hierarchical nav structure lands in _vibe_nav (via the P4.1 NavStore) with the
// expected action/target per item, including the nested switchboard page.
func TestCreateSwitchboardTool_PersistsHierarchicalNav(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedSwitchboardTargets(t, c)
	ctx := context.Background()

	const pageID = "main_menu"
	const nestedID = "admin_menu"

	// First create the nested (child) switchboard page so it exists as a
	// bindable open_switchboard target for the Main Menu.
	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    nestedID,
			"title": "Admin Menu",
			"items": []any{
				map[string]any{"label": "Sales Report", "action": schema.NavOpenReport, "target": "sales_report"},
			},
		},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "creating the nested switchboard should succeed: %+v", resp.Error)

	// Now create the Main Menu, with an item that opens the nested switchboard.
	resp = handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    pageID,
			"title": "Main Menu",
			"items": switchboardItems(nestedID),
		},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "create_switchboard should succeed in full sandbox: %+v", resp.Error)

	// The nav structure must be persisted hierarchically: the Main Menu's items
	// live under the main_menu page id.
	store := schema.NewNavStore(c.SQLiteDB())
	items, err := store.GetNav(pageID)
	require.NoError(t, err)
	require.Len(t, items, 3, "Main Menu should have its three items persisted under %q", pageID)

	byLabel := map[string]schema.NavItem{}
	for _, it := range items {
		byLabel[it.Label] = it
	}

	require.Contains(t, byLabel, "Enter Orders")
	assert.Equal(t, schema.NavOpenForm, byLabel["Enter Orders"].ActionKind)
	assert.Equal(t, "orders_form", byLabel["Enter Orders"].TargetID)

	require.Contains(t, byLabel, "Sales Report")
	assert.Equal(t, schema.NavOpenReport, byLabel["Sales Report"].ActionKind)
	assert.Equal(t, "sales_report", byLabel["Sales Report"].TargetID)

	require.Contains(t, byLabel, "Admin")
	assert.Equal(t, schema.NavOpenSwitchboard, byLabel["Admin"].ActionKind)
	assert.Equal(t, nestedID, byLabel["Admin"].TargetID,
		"the nested switchboard item must target the child page id")

	// And the nested page's own item is persisted under the nested page id.
	nested, err := store.GetNav(nestedID)
	require.NoError(t, err)
	require.Len(t, nested, 1, "the nested switchboard should have its single item persisted")
	assert.Equal(t, schema.NavOpenReport, nested[0].ActionKind)
	assert.Equal(t, "sales_report", nested[0].TargetID)
}

// TestCreateSwitchboardTool_RejectsNonexistentTarget asserts an item binding to
// a target not present in the nav-target catalog is rejected.
func TestCreateSwitchboardTool_RejectsNonexistentTarget(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedSwitchboardTargets(t, c)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    "bad_menu",
			"title": "Bad Menu",
			"items": []any{
				map[string]any{"label": "Ghost Form", "action": schema.NavOpenForm, "target": "does_not_exist"},
			},
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error,
		"create_switchboard must reject a binding to a target absent from the catalog")

	// Nothing should have been persisted for the rejected switchboard page.
	store := schema.NewNavStore(c.SQLiteDB())
	items, err := store.GetNav("bad_menu")
	require.NoError(t, err)
	assert.Empty(t, items, "no nav items should be persisted when a target is invalid")
}

// TestCreateSwitchboardTool_EnforcesMaxItems asserts the Access ≤8-items-per-page
// constraint is enforced.
func TestCreateSwitchboardTool_EnforcesMaxItems(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedSwitchboardTargets(t, c)
	ctx := context.Background()

	// Nine items (all binding to the valid orders_form) must be rejected.
	nine := make([]any, 0, 9)
	for i := 0; i < 9; i++ {
		nine = append(nine, map[string]any{
			"label":  fmt.Sprintf("Item %d", i),
			"action": schema.NavOpenForm,
			"target": "orders_form",
		})
	}

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    "too_many",
			"title": "Too Many",
			"items": nine,
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error,
		"create_switchboard must reject more than 8 items per switchboard page")
}

// seedSwitchboardMacro saves a macro into the container's _vibe_macros store so a
// run_macro switchboard item has a valid macro target to bind to (P5.4).
func seedSwitchboardMacro(t *testing.T, c *container.Container, id string) {
	t.Helper()
	require.NoError(t, automation.NewMacroStore(c.SQLiteDB()).Save(automation.MacroSpec{
		ID:    id,
		Title: "Backup",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionMsgBox, Args: map[string]string{"message": "done"}},
		},
	}))
}

// TestCreateSwitchboardTool_AcceptsRunMacroTarget (P5.4 RED) asserts that
// create_switchboard accepts a run_macro item whose target is a macro saved via
// the MacroStore — i.e. validateNavTarget covers schema.NavRunMacro and no
// longer rejects it as an "unsupported switchboard action". The persisted nav
// item must carry action_kind=run_macro and the macro id as its target.
func TestCreateSwitchboardTool_AcceptsRunMacroTarget(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedSwitchboardTargets(t, c)
	seedSwitchboardMacro(t, c, "backup_macro")
	ctx := context.Background()

	const pageID = "macro_menu"
	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    pageID,
			"title": "Macro Menu",
			"items": []any{
				map[string]any{"label": "Run Backup", "action": schema.NavRunMacro, "target": "backup_macro"},
			},
		},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error,
		"create_switchboard must accept a run_macro item bound to an existing macro: %+v", resp.Error)

	store := schema.NewNavStore(c.SQLiteDB())
	items, err := store.GetNav(pageID)
	require.NoError(t, err)
	require.Len(t, items, 1, "the run_macro switchboard item must be persisted")
	assert.Equal(t, schema.NavRunMacro, items[0].ActionKind)
	assert.Equal(t, "backup_macro", items[0].TargetID)
}

// TestCreateSwitchboardTool_RejectsMissingRunMacroTarget (P5.4 RED) asserts that
// a run_macro item targeting a macro absent from the MacroStore is rejected, and
// that nothing is persisted for the rejected switchboard page.
func TestCreateSwitchboardTool_RejectsMissingRunMacroTarget(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedSwitchboardTargets(t, c)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    "ghost_macro_menu",
			"title": "Ghost Macro Menu",
			"items": []any{
				map[string]any{"label": "Run Ghost", "action": schema.NavRunMacro, "target": "no_such_macro"},
			},
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error,
		"create_switchboard must reject a run_macro item bound to a nonexistent macro")

	store := schema.NewNavStore(c.SQLiteDB())
	items, err := store.GetNav("ghost_macro_menu")
	require.NoError(t, err)
	assert.Empty(t, items, "no nav items should be persisted when the macro target is invalid")
}

// TestCreateSwitchboardTool_DeniedReadWrite asserts create_switchboard is
// rejected with a capability/permission error under a read-write sandbox
// (App-Mode gating: schema-category tools are denied).
func TestCreateSwitchboardTool_DeniedReadWrite(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeReadWrite)
	seedSwitchboardTargets(t, c)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "create_switchboard",
		Params: map[string]any{
			"id":    "main_menu",
			"title": "Main Menu",
			"items": switchboardItems("admin_menu"),
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "create_switchboard must be denied in read-write sandbox (App Mode)")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code,
		"create_switchboard denial should be a capability/permission error")
}
