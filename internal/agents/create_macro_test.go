package agents_test

// P5.8 — Agent Macro Wizard tools (plan §5.4 / §4.3).
//
// INTEGRATION test: stands up the real agent Handler over a real .vibe
// container (SQLCipher) + real DuckDB engine, and drives the new create_macro /
// list_macros wizard tools through the JSON-RPC tool surface (Handler.Handle) —
// NOT by calling any Go backend method directly, mirroring
// create_switchboard_test.go / create_report_test.go.
//
// Acceptance behavior asserted here:
//   - create_macro / list_macros are registered agent tools in a NEW
//     "automation" category, allowed in full / read-write but NOT read-only.
//   - create_macro, dispatched through Handler.Handle against a real .vibe +
//     DuckDB, validates EVERY action target against the relevant catalog
//     (forms / reports / macros / queries) before persisting, then writes the
//     spec atomically via the automation.MacroStore (a rejected spec leaves no
//     macro).
//   - list_macros reads back stored macros via the shared MacroStore.
//   - The "automation" category is gated by the sandbox (denied in read-only)
//     and routed through the approval queue as a write op when RequireApproval
//     is set.

import (
	"context"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMacroTargets seeds an orders_form (FormStore) and a big_orders named query
// (queries registry) so create_macro has valid catalog targets for its
// OpenForm + RunQuery actions.
func seedMacroTargets(t *testing.T, c *container.Container) {
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

	require.NoError(t, queries.NewRegistry(db, wizLogger()).Create(context.Background(), &queries.Query{
		ID:         "big_orders",
		Title:      "big_orders",
		SQL:        "SELECT * FROM orders WHERE total > 50",
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}))
}

// macroActions returns a 2-action spec: an OpenForm targeting orders_form plus an
// If/Then RunQuery targeting the big_orders named query.
func macroActions() []any {
	return []any{
		map[string]any{
			"kind": automation.ActionOpenForm,
			"args": map[string]any{"form": "orders_form"},
		},
		map[string]any{
			"kind":      automation.ActionRunQuery,
			"args":      map[string]any{"query": "big_orders"},
			"condition": map[string]any{"expr": "1 = 1"},
		},
	}
}

// TestCreateMacroTool_RegisteredWithSchema asserts create_macro / list_macros are
// discoverable on the tool surface with documented schemas in the new
// "automation" category.
func TestCreateMacroTool_RegisteredWithSchema(t *testing.T) {
	for _, name := range []string{"create_macro", "list_macros"} {
		tool := agents.GetTool(name)
		require.NotNil(t, tool, "%s must be registered as an agent tool", name)
		assert.NotEmpty(t, tool.Description, "%s must have a documented description", name)
		assert.Equal(t, "automation", tool.Category,
			"%s must be in the new 'automation' category", name)

		found := false
		for _, td := range agents.ListTools() {
			if td.Name == name {
				found = true
				break
			}
		}
		assert.True(t, found, "%s must appear in ListTools()", name)
	}

	cm := agents.GetTool("create_macro")
	byName := map[string]agents.ParamDef{}
	for _, p := range cm.Params {
		byName[p.Name] = p
	}
	require.Contains(t, byName, "title", "create_macro must document a 'title' param")
	require.Contains(t, byName, "actions", "create_macro must document an 'actions' param")
}

// TestCreateMacroTool_PersistsValidatedSpec drives create_macro through the
// JSON-RPC surface against a real .vibe + DuckDB with a 2-action spec, then
// asserts created=true and that the MacroStore holds the spec. list_macros must
// then read it back.
func TestCreateMacroTool_PersistsValidatedSpec(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedMacroTargets(t, c)
	ctx := context.Background()

	const macroID = "open_orders"
	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "create_macro",
		Params: map[string]any{
			"id":      macroID,
			"title":   "Open Orders",
			"actions": macroActions(),
		},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "create_macro should succeed in full sandbox: %+v", resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok, "result should be a map, got %T", resp.Result)
	assert.Equal(t, true, result["created"], "create_macro should report created=true")

	// The MacroStore must hold exactly the spec we created (atomic persist).
	store := automation.NewMacroStore(c.SQLiteDB())
	spec, err := store.Get(macroID)
	require.NoError(t, err, "the macro must be persisted via the shared MacroStore")
	require.Len(t, spec.Actions, 2, "both actions must be persisted")
	assert.Equal(t, automation.ActionOpenForm, spec.Actions[0].Kind)
	assert.Equal(t, automation.ActionRunQuery, spec.Actions[1].Kind)

	// list_macros must read it back via the shared store.
	resp = handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "list_macros",
		Params:  map[string]any{},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "list_macros should succeed: %+v", resp.Error)

	listResult, ok := resp.Result.(map[string]any)
	require.True(t, ok, "list_macros result should be a map, got %T", resp.Result)
	macros := toMaps(t, listResult["macros"])
	var found bool
	for _, m := range macros {
		if asStr(m["id"]) == macroID {
			found = true
		}
	}
	assert.True(t, found, "list_macros must return the created macro %q", macroID)
}

// TestCreateMacroTool_RejectsMissingTarget asserts a create_macro whose OpenForm
// action points at a nonexistent form is rejected with ErrInvalidParams and
// leaves no persisted macro.
func TestCreateMacroTool_RejectsMissingTarget(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	seedMacroTargets(t, c)
	ctx := context.Background()

	const macroID = "ghost_macro"
	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "create_macro",
		Params: map[string]any{
			"id":    macroID,
			"title": "Ghost",
			"actions": []any{
				map[string]any{
					"kind": automation.ActionOpenForm,
					"args": map[string]any{"form": "does_not_exist"},
				},
			},
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "create_macro must reject an OpenForm bound to a nonexistent form")
	assert.Equal(t, agents.ErrInvalidParams, resp.Error.Code,
		"a bad action target should be an invalid-params error")

	// Nothing should have been persisted for the rejected macro.
	store := automation.NewMacroStore(c.SQLiteDB())
	_, err := store.Get(macroID)
	require.ErrorIs(t, err, automation.ErrMacroNotFound,
		"no macro should be persisted when an action target is invalid")
}

// TestCreateMacroTool_DeniedReadOnly asserts create_macro is rejected with a
// capability/permission error under a read-only sandbox (App-Mode gating: the
// 'automation' category is below the agent in read-only).
func TestCreateMacroTool_DeniedReadOnly(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeReadOnly)
	seedMacroTargets(t, c)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "create_macro",
		Params: map[string]any{
			"id":      "open_orders",
			"title":   "Open Orders",
			"actions": macroActions(),
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "create_macro must be denied in read-only sandbox")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code,
		"create_macro denial should be a capability/permission error")
}

// TestCreateMacroTool_SandboxCheckBlocksReadOnly asserts the sandbox itself
// classifies create_macro as a gated automation tool: Check errors in read-only
// but is allowed in read-write and full.
func TestCreateMacroTool_SandboxCheckBlocksReadOnly(t *testing.T) {
	ro := &agents.SandboxConfig{Mode: agents.ModeReadOnly}
	require.Error(t, ro.Check("create_macro"),
		"create_macro must be blocked under read-only")

	rw := &agents.SandboxConfig{Mode: agents.ModeReadWrite}
	require.NoError(t, rw.Check("create_macro"),
		"create_macro must be allowed under read-write")

	full := &agents.SandboxConfig{Mode: agents.ModeFull}
	require.NoError(t, full.Check("create_macro"),
		"create_macro must be allowed under full")
}

// TestCreateMacroTool_GatedByApprovalQueue asserts create_macro is a write op
// routed through the approval queue when RequireApproval is set: rejecting the
// pending approval yields an approval-needed error and persists no macro.
func TestCreateMacroTool_GatedByApprovalQueue(t *testing.T) {
	_, c := newWizardFixture(t, agents.ModeFull)
	handler := agents.NewHandler(c, nil, &agents.SandboxConfig{
		Mode:            agents.ModeFull,
		MaxQueryRows:    1000,
		RequireApproval: true,
	}, wizLogger())
	seedMacroTargets(t, c)
	ctx := context.Background()

	const macroID = "approval_macro"

	// Reject the pending approval as soon as it arrives.
	go func() {
		req := <-handler.Approvals().Pending()
		handler.Approvals().Reject(req.ID)
	}()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "create_macro",
		Params: map[string]any{
			"id":      macroID,
			"title":   "Approval Macro",
			"actions": macroActions(),
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "a rejected create_macro approval must produce an error")
	assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code,
		"create_macro must go through the approval queue as a write op")

	store := automation.NewMacroStore(c.SQLiteDB())
	_, err := store.Get(macroID)
	require.ErrorIs(t, err, automation.ErrMacroNotFound,
		"a rejected approval must leave no persisted macro")
}
