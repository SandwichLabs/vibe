package agents_test

// P2.7 — Agent Query Wizard tool (plan §3, §4.3, mockup #3).
//
// This is an INTEGRATION test: it stands up the real agent Handler over a real
// workspace + real DuckDB engine, and drives the wizard tool
// through the JSON-RPC tool surface (Handler.Handle) — NOT by calling any Go
// backend method directly.
//
// #68 removed the Schema Wizard tools (create_relationship/list_relationships
// — the relationships-UI layer). Foreign keys are plain SQL now (declared
// inline in create_table's/run_query's DDL); only build_query (the Query
// Wizard / QBE tool) remains from this file's original P2.7 scope.
//
// Acceptance behavior asserted here:
//   - build_query is a registered tool dispatched through Handler.Handle and
//     described in the tool catalog.
//   - build_query delegates to the P2.6 QBE/filter renderer: it creates a named
//     query that is then runnable via run_query.
//   - build_query is denied under a read-only sandbox.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wizLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newWizardFixture builds a real workspace with customers + orders DuckDB
// tables and returns an agent Handler wired to a real engine.Manager.
func newWizardFixture(t *testing.T, mode agents.SandboxMode) (*agents.Handler, workspace.Workspace) {
	t.Helper()

	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	res, err := workspace.Init(filepath.Join(t.TempDir(), "wizard"), workspace.WithName("Wizard Tools Test"))
	require.NoError(t, err)
	ws := res.Workspace
	t.Cleanup(func() { _ = ws.Close() })
	sess, _, err := workspace.OpenSession(context.Background(), ws, wizLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })

	ddb := sess.DB()
	require.NotNil(t, ddb, "DuckDB connection should be available")

	_, err = ddb.Exec(`
		CREATE TABLE customers (
			id   INTEGER PRIMARY KEY,
			name VARCHAR
		);
		CREATE TABLE orders (
			order_id    INTEGER PRIMARY KEY,
			customer_id INTEGER REFERENCES customers(id),
			total       DOUBLE
		);
		INSERT INTO customers (id, name) VALUES (1, 'Alice'), (2, 'Bob');
		INSERT INTO orders (order_id, customer_id, total) VALUES (10, 1, 99.5);
	`)
	require.NoError(t, err)

	eng := engine.NewManager(sess, engine.WithLogger(wizLogger()))
	sandbox := &agents.SandboxConfig{
		Mode:         mode,
		MaxQueryRows: 1000,
	}
	handler := agents.NewHandler(workspace.NewAgentAccess(ws, sess), eng, sandbox, wizLogger())

	return handler, ws
}

// buildQueryParams describes a QBE-style query over orders: select rows whose
// total is greater than 50.
func buildQueryParams() map[string]any {
	return map[string]any{
		"name":  "big_orders",
		"table": "orders",
		"conditions": []any{
			map[string]any{"column": "total", "op": "gt", "value": 50},
		},
	}
}

// TestWizardTools_RegisteredWithSchema asserts build_query is discoverable on
// the tool surface with a documented schema.
func TestWizardTools_RegisteredWithSchema(t *testing.T) {
	tool := agents.GetTool("build_query")
	require.NotNil(t, tool, "build_query must be registered as an agent tool")
	assert.NotEmpty(t, tool.Description, "build_query must have a documented description")

	found := false
	for _, td := range agents.ListTools() {
		if td.Name == "build_query" {
			found = true
			break
		}
	}
	assert.True(t, found, "build_query must appear in ListTools()")

	// build_query must document the bound table.
	bqByName := map[string]agents.ParamDef{}
	for _, p := range tool.Params {
		bqByName[p.Name] = p
	}
	require.Contains(t, bqByName, "table", "build_query must document a 'table' param")
	assert.True(t, bqByName["table"].Required, "'table' should be required")
}

// TestWizardTools_ForeignKeyEnforcedViaPlainSQL asserts the plain-SQL inline
// FK declared in newWizardFixture (orders.customer_id REFERENCES
// customers(id) — #68 dropped the create_relationship tool that used to do
// this) is actually enforced: an orphan create_record is rejected.
func TestWizardTools_ForeignKeyEnforcedViaPlainSQL(t *testing.T) {
	handler, _ := newWizardFixture(t, agents.ModeFull)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "create_record",
		Params: map[string]any{
			"table":  "orders",
			"values": map[string]any{"order_id": 99, "customer_id": 999, "total": 1.0},
		},
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "orphan insert violating the FK must be rejected")
}

// TestWizardTools_BuildQuery drives build_query through the tool surface, then
// confirms the named query was created and is runnable via run_query.
func TestWizardTools_BuildQuery(t *testing.T) {
	handler, _ := newWizardFixture(t, agents.ModeFull)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "build_query",
		Params:  buildQueryParams(),
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "build_query should succeed in full sandbox: %+v", resp.Error)

	// The named query must now appear in the registry.
	resp = handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "list_queries",
		Params:  map[string]any{},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "list_queries should succeed: %+v", resp.Error)

	result, _ := resp.Result.(map[string]any)
	queries, ok := result["queries"].([]map[string]any)
	require.True(t, ok, "queries should be a []map[string]any, got %T", result["queries"])

	var savedSQL string
	for _, q := range queries {
		if asStr(q["name"]) == "big_orders" {
			savedSQL = asStr(q["sql"])
		}
	}
	require.NotEmpty(t, savedSQL, "build_query must persist a named query 'big_orders' with SQL")

	// The saved SQL must be runnable via run_query and return the matching row.
	resp = handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "run_query",
		Params:  map[string]any{"sql": savedSQL},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "the query built by build_query should be runnable: %+v", resp.Error)

	runResult, _ := resp.Result.(map[string]any)
	rows, ok := runResult["rows"].([][]any)
	require.True(t, ok, "run_query rows should be [][]any, got %T", runResult["rows"])
	require.Len(t, rows, 1, "only the order with total > 50 should match")
}

// TestWizardTools_DeniedReadOnly asserts build_query is rejected with a
// capability/permission error under a read-only sandbox.
func TestWizardTools_DeniedReadOnly(t *testing.T) {
	handler, _ := newWizardFixture(t, agents.ModeReadOnly)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      100,
		Method:  "build_query",
		Params:  buildQueryParams(),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "build_query must be denied in read-only sandbox")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code,
		"build_query denial should be a capability/permission error")
}

// --- helpers ---

func asStr(v any) string {
	s, _ := v.(string)
	return s
}
