package agents_test

// P1.7 — Agent create_form tool (Form Wizard), plan §1.6 + §4.3.
//
// This is an INTEGRATION test: it stands up the real agent Handler over a real
// .vibe container (SQLCipher) + real DuckDB engine, and drives the create_form
// tool through the JSON-RPC tool surface (Handler.Handle) — NOT by calling any
// Go method directly.
//
// Acceptance behavior asserted here:
//   - create_form is registered in ListTools with a documented schema
//   - Invoked via the tool surface in a write sandbox, it persists a valid
//     _vibe_forms spec retrievable by GetForm (round-trips the requested spec,
//     including the master_detail subform link + on_submit) and seeds
//     _vibe_field_meta captions derived from column names
//   - Denied in read-only sandbox (capability denied) without writing anything
//   - When approval is required, the operation enters the approval queue and is
//     gated consistently with other write tools (create_table / create_record)

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cfTestPassphrase = "test-passphrase-12345"

func cfLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newCreateFormFixture builds a real .vibe container with orders + order_items
// DuckDB tables and returns an agent Handler wired to a real engine.Manager.
func newCreateFormFixture(t *testing.T, mode agents.SandboxMode) (*agents.Handler, *container.Container, *agents.SandboxConfig) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "createform.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(cfTestPassphrase),
		container.WithLogger(cfLogger()),
		container.WithTitle("Create Form Test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb, "DuckDB connection should be available")

	_, err = ddb.Exec(`
		CREATE TABLE orders (
			order_id   INTEGER PRIMARY KEY,
			customer   VARCHAR,
			order_date DATE,
			total      DOUBLE
		);
		CREATE TABLE order_items (
			item_id   INTEGER PRIMARY KEY,
			order_id  INTEGER,
			product   VARCHAR,
			quantity  INTEGER
		);
	`)
	require.NoError(t, err)

	sandbox := &agents.SandboxConfig{
		Mode:         mode,
		MaxQueryRows: 100,
	}
	eng := engine.NewManager(ddb, engine.WithLogger(cfLogger()))
	handler := agents.NewHandler(c, eng, sandbox, cfLogger())

	return handler, c, sandbox
}

// createFormParams builds the requested create_form spec: a master_detail form
// on `orders` with a subform linked to `order_items` and on_submit=refresh.
func createFormParams() map[string]any {
	return map[string]any{
		"name":      "orders_form",
		"table":     "orders",
		"archetype": "master_detail",
		"on_submit": "refresh",
		"fields": []any{
			map[string]any{"name": "order_id"},
			map[string]any{"name": "customer"},
			map[string]any{"name": "order_date"},
			map[string]any{"name": "total"},
		},
		"subform": map[string]any{
			"child_table": "order_items",
			"child_key":   "order_id",
			"parent_key":  "order_id",
			"fields": []any{
				map[string]any{"name": "product"},
				map[string]any{"name": "quantity"},
			},
		},
	}
}

// TestAgentCreateForm_RegisteredWithSchema asserts the tool is discoverable on
// the tool surface with a documented schema (acceptance: registered in
// ListTools with a documented schema).
func TestAgentCreateForm_RegisteredWithSchema(t *testing.T) {
	tool := agents.GetTool("create_form")
	require.NotNil(t, tool, "create_form must be registered as an agent tool")
	assert.NotEmpty(t, tool.Description, "create_form must have a documented description")

	// Required params should at least cover the form binding essentials.
	byName := map[string]agents.ParamDef{}
	for _, p := range tool.Params {
		byName[p.Name] = p
	}
	require.Contains(t, byName, "name", "create_form must document a 'name' param")
	require.Contains(t, byName, "table", "create_form must document a 'table' param")
	require.Contains(t, byName, "archetype", "create_form must document an 'archetype' param")
	assert.True(t, byName["name"].Required, "'name' should be required")
	assert.True(t, byName["table"].Required, "'table' should be required")

	// It must be listed by ListTools.
	found := false
	for _, td := range agents.ListTools() {
		if td.Name == "create_form" {
			found = true
			break
		}
	}
	assert.True(t, found, "create_form must appear in ListTools()")
}

// TestAgentCreateForm drives the create_form tool through the JSON-RPC surface
// against a real .vibe + DuckDB in a write sandbox, then verifies persistence,
// round-trip via GetForm, and seeded field-meta captions.
func TestAgentCreateForm(t *testing.T) {
	handler, c, _ := newCreateFormFixture(t, agents.ModeFull)
	ctx := context.Background()

	req := &agents.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "create_form",
		Params:  createFormParams(),
	}
	resp := handler.Handle(ctx, req)
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "create_form should succeed in full sandbox: %+v", resp.Error)

	// The response should identify the created form so callers can locate it.
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok, "result should be a map, got %T", resp.Result)
	idVal, ok := result["id"]
	require.True(t, ok, "response should carry the created form id")
	formID, ok := idVal.(string)
	require.True(t, ok && formID != "", "form id should be a non-empty string")

	// A _vibe_forms row must now exist and GetForm must round-trip the spec.
	store := schema.NewFormStore(c.SQLiteDB())
	spec, err := store.GetForm(formID)
	require.NoError(t, err, "GetForm should find the persisted form")
	require.NotNil(t, spec)

	assert.Equal(t, "orders", spec.Table)
	assert.Equal(t, "master_detail", spec.Archetype)
	assert.Equal(t, "refresh", spec.OnSubmit)
	require.Len(t, spec.Fields, 4, "all requested master fields should be persisted")

	require.NotNil(t, spec.Subform, "master_detail form must carry a subform link")
	assert.Equal(t, "order_items", spec.Subform.ChildTable)
	assert.Equal(t, "order_id", spec.Subform.ChildKey)
	assert.Equal(t, "order_id", spec.Subform.ParentKey)

	// Field-meta captions should be seeded from column names.
	metaStore := schema.NewStore(c.SQLiteDB())
	metas, err := metaStore.GetFieldMeta("orders")
	require.NoError(t, err)
	require.NotEmpty(t, metas, "create_form should seed _vibe_field_meta for the bound table")

	captions := map[string]string{}
	for _, m := range metas {
		captions[m.ColumnName] = m.Caption
	}
	require.Contains(t, captions, "customer", "field meta should be seeded for 'customer'")
	assert.NotEmpty(t, captions["customer"], "seeded caption should be non-empty (derived from column name)")
	assert.Contains(t, captions["order_id"], "Order", "caption should be a humanized form of the column name")
}

// TestAgentCreateForm_DeniedReadOnly asserts the same call is rejected with a
// capability-denied error in read-only sandbox and writes nothing.
func TestAgentCreateForm_DeniedReadOnly(t *testing.T) {
	handler, c, _ := newCreateFormFixture(t, agents.ModeReadOnly)
	ctx := context.Background()

	req := &agents.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "create_form",
		Params:  createFormParams(),
	}
	resp := handler.Handle(ctx, req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "create_form must be denied in read-only sandbox")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code, "denial should be a capability/permission error")

	// Nothing should have been persisted.
	store := schema.NewFormStore(c.SQLiteDB())
	forms, err := store.ListForms()
	require.NoError(t, err)
	assert.Empty(t, forms, "no form should be written when denied in read-only sandbox")
}

// TestAgentCreateForm_ApprovalQueue asserts create_form is gated by the approval
// queue like other write tools: with RequireApproval set, the operation enters
// the queue (Pending) and a rejection blocks the write.
func TestAgentCreateForm_ApprovalQueue(t *testing.T) {
	handler, c, sandbox := newCreateFormFixture(t, agents.ModeFull)
	sandbox.RequireApproval = true
	ctx := context.Background()

	req := &agents.Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "create_form",
		Params:  createFormParams(),
	}

	// Reject the pending approval so the call returns instead of blocking.
	done := make(chan *agents.Response, 1)
	go func() { done <- handler.Handle(ctx, req) }()

	select {
	case pending := <-handler.Approvals().Pending():
		require.NotNil(t, pending, "create_form should enter the approval queue")
		assert.Equal(t, "create_form", pending.Method, "queued request should be create_form")
		handler.Approvals().Reject(pending.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("create_form did not enter the approval queue (not gated like other write tools)")
	}

	resp := <-done
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "rejected approval should produce an error response")

	// Rejected => nothing persisted.
	store := schema.NewFormStore(c.SQLiteDB())
	forms, err := store.ListForms()
	require.NoError(t, err)
	assert.Empty(t, forms, "rejected create_form must not persist a form")
}
