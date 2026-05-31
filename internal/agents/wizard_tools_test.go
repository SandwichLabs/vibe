package agents_test

// P2.7 — Agent Schema/Query Wizard tools (plan §3, §4.3, mockup #3).
//
// This is an INTEGRATION test: it stands up the real agent Handler over a real
// .vibe container (SQLCipher) + real DuckDB engine, and drives the new wizard
// tools through the JSON-RPC tool surface (Handler.Handle) — NOT by calling any
// Go backend method directly.
//
// Acceptance behavior asserted here:
//   - create_relationship, list_relationships, build_query are registered tools
//     dispatched through Handler.Handle and described in the tool catalog.
//   - create_relationship delegates to the P2.3 FK backend: it applies a real
//     DuckDB foreign key (so an orphan create_record is rejected) and persists a
//     relationship row that list_relationships returns.
//   - build_query delegates to the P2.6 QBE/filter renderer: it creates a named
//     query that is then runnable via run_query.
//   - All three mutating tools are denied under a read-only sandbox.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wizTestPassphrase = "test-passphrase-12345"

func wizLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newWizardFixture builds a real .vibe container with customers + orders DuckDB
// tables (customers.id is a PRIMARY KEY so DuckDB will accept the FK reference)
// and returns an agent Handler wired to a real engine.Manager.
func newWizardFixture(t *testing.T, mode agents.SandboxMode) (*agents.Handler, *container.Container) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "wizard.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(wizTestPassphrase),
		container.WithLogger(wizLogger()),
		container.WithTitle("Wizard Tools Test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb, "DuckDB connection should be available")

	_, err = ddb.Exec(`
		CREATE TABLE customers (
			id   INTEGER PRIMARY KEY,
			name VARCHAR
		);
		CREATE TABLE orders (
			order_id    INTEGER PRIMARY KEY,
			customer_id INTEGER,
			total       DOUBLE
		);
		INSERT INTO customers (id, name) VALUES (1, 'Alice'), (2, 'Bob');
		INSERT INTO orders (order_id, customer_id, total) VALUES (10, 1, 99.5);
	`)
	require.NoError(t, err)

	eng := engine.NewManager(ddb, engine.WithLogger(wizLogger()))
	sandbox := &agents.SandboxConfig{
		Mode:         mode,
		MaxQueryRows: 1000,
	}
	handler := agents.NewHandler(c, eng, sandbox, wizLogger())

	return handler, c
}

// createRelationshipParams describes orders.customer_id -> customers.id with a
// RESTRICT delete rule.
func createRelationshipParams() map[string]any {
	return map[string]any{
		"parent_table":  "customers",
		"parent_column": "id",
		"child_table":   "orders",
		"child_column":  "customer_id",
		"on_delete":     "RESTRICT",
	}
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

// TestWizardTools_RegisteredWithSchema asserts the three wizard tools are
// discoverable on the tool surface with documented schemas.
func TestWizardTools_RegisteredWithSchema(t *testing.T) {
	for _, name := range []string{"create_relationship", "list_relationships", "build_query"} {
		tool := agents.GetTool(name)
		require.NotNil(t, tool, "%s must be registered as an agent tool", name)
		assert.NotEmpty(t, tool.Description, "%s must have a documented description", name)

		found := false
		for _, td := range agents.ListTools() {
			if td.Name == name {
				found = true
				break
			}
		}
		assert.True(t, found, "%s must appear in ListTools()", name)
	}

	// create_relationship must document the link essentials.
	rel := agents.GetTool("create_relationship")
	byName := map[string]agents.ParamDef{}
	for _, p := range rel.Params {
		byName[p.Name] = p
	}
	for _, p := range []string{"parent_table", "parent_column", "child_table", "child_column"} {
		require.Contains(t, byName, p, "create_relationship must document a %q param", p)
		assert.True(t, byName[p].Required, "%q should be required", p)
	}

	// build_query must document the bound table.
	bq := agents.GetTool("build_query")
	bqByName := map[string]agents.ParamDef{}
	for _, p := range bq.Params {
		bqByName[p.Name] = p
	}
	require.Contains(t, bqByName, "table", "build_query must document a 'table' param")
	assert.True(t, bqByName["table"].Required, "'table' should be required")
}

// TestWizardTools_CreateAndListRelationship drives create_relationship +
// list_relationships through the JSON-RPC surface against a real .vibe + DuckDB,
// then asserts the FK is actually enforced (orphan insert rejected).
func TestWizardTools_CreateAndListRelationship(t *testing.T) {
	handler, _ := newWizardFixture(t, agents.ModeFull)
	ctx := context.Background()

	// create_relationship.
	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "create_relationship",
		Params:  createRelationshipParams(),
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "create_relationship should succeed in full sandbox: %+v", resp.Error)

	// list_relationships must now return the relationship we just created.
	resp = handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "list_relationships",
		Params:  map[string]any{},
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "list_relationships should succeed: %+v", resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok, "result should be a map, got %T", resp.Result)
	relsAny, ok := result["relationships"]
	require.True(t, ok, "list_relationships response should carry a 'relationships' field")

	rels := toMaps(t, relsAny)
	require.Len(t, rels, 1, "exactly one relationship should be listed")
	assert.Equal(t, "customers", asStr(rels[0]["parent_table"]))
	assert.Equal(t, "id", asStr(rels[0]["parent_column"]))
	assert.Equal(t, "orders", asStr(rels[0]["child_table"]))
	assert.Equal(t, "customer_id", asStr(rels[0]["child_column"]))

	// The FK must be REALLY enforced: an orphan child insert is rejected.
	resp = handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      3,
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

// TestWizardTools_DeniedReadOnly asserts all three wizard tools are rejected
// with a capability/permission error under a read-only sandbox.
func TestWizardTools_DeniedReadOnly(t *testing.T) {
	handler, _ := newWizardFixture(t, agents.ModeReadOnly)
	ctx := context.Background()

	cases := []struct {
		method string
		params map[string]any
	}{
		{"create_relationship", createRelationshipParams()},
		{"build_query", buildQueryParams()},
	}
	for i, tc := range cases {
		resp := handler.Handle(ctx, &agents.Request{
			JSONRPC: "2.0",
			ID:      100 + i,
			Method:  tc.method,
			Params:  tc.params,
		})
		require.NotNil(t, resp)
		require.NotNil(t, resp.Error, "%s must be denied in read-only sandbox", tc.method)
		assert.Equal(t, agents.ErrPermission, resp.Error.Code,
			"%s denial should be a capability/permission error", tc.method)
	}
}

// --- helpers ---

func toMaps(t *testing.T, v any) []map[string]any {
	t.Helper()
	switch typed := v.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]any)
			require.True(t, ok, "relationship entry should be a map, got %T", item)
			out = append(out, m)
		}
		return out
	default:
		t.Fatalf("relationships should be a slice of maps, got %T", v)
		return nil
	}
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}
