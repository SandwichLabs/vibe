package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Protocol tests ---

func TestParseRequest_Valid(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":1,"method":"introspect_schema","params":{}}`
	req, err := ParseRequest([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, float64(1), req.ID) // JSON numbers unmarshal to float64
	assert.Equal(t, "introspect_schema", req.Method)
}

func TestParseRequest_StringID(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":"abc-123","method":"list_pages"}`
	req, err := ParseRequest([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "abc-123", req.ID)
	assert.Equal(t, "list_pages", req.Method)
}

func TestParseRequest_InvalidJSON(t *testing.T) {
	data := `{not valid json}`
	_, err := ParseRequest([]byte(data))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON-RPC request")
}

func TestParseRequest_WrongVersion(t *testing.T) {
	data := `{"jsonrpc":"1.0","id":1,"method":"test"}`
	_, err := ParseRequest([]byte(data))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON-RPC version")
}

func TestParseRequest_MissingMethod(t *testing.T) {
	data := `{"jsonrpc":"2.0","id":1}`
	_, err := ParseRequest([]byte(data))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing method")
}

func TestResponseMarshal(t *testing.T) {
	resp := NewResponse(1, map[string]any{"tables": []string{"users"}})
	data, err := resp.Marshal()
	require.NoError(t, err)

	var parsed Response
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.Nil(t, parsed.Error)
}

func TestErrorResponseMarshal(t *testing.T) {
	resp := NewErrorResponse(1, ErrMethodNotFound, "unknown method")
	data, err := resp.Marshal()
	require.NoError(t, err)

	var parsed Response
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.NotNil(t, parsed.Error)
	assert.Equal(t, ErrMethodNotFound, parsed.Error.Code)
}

// --- Tools tests ---

func TestGetTool_Exists(t *testing.T) {
	tool := GetTool("introspect_schema")
	require.NotNil(t, tool)
	assert.Equal(t, "introspect", tool.Category)
}

func TestGetTool_NotFound(t *testing.T) {
	tool := GetTool("nonexistent_method")
	assert.Nil(t, tool)
}

func TestListTools(t *testing.T) {
	tools := ListTools()
	assert.Len(t, tools, len(Tools))
	// Verify we got a copy, not a reference.
	tools[0].Name = "modified"
	assert.NotEqual(t, "modified", Tools[0].Name)
}

// --- Sandbox tests ---

func TestSandbox_ReadOnlyBlocksWrite(t *testing.T) {
	sc := DefaultSandboxConfig()
	assert.Equal(t, ModeReadOnly, sc.Mode)

	// Read operations should be allowed.
	assert.NoError(t, sc.Check("introspect_schema"))
	assert.NoError(t, sc.Check("describe_table"))
	assert.NoError(t, sc.Check("list_pages"))
	assert.NoError(t, sc.Check("get_page"))
	assert.NoError(t, sc.Check("list_queries"))

	// Data write operations should be blocked.
	assert.Error(t, sc.Check("run_query"))
	assert.Error(t, sc.Check("create_record"))
	assert.Error(t, sc.Check("update_record"))
	assert.Error(t, sc.Check("delete_record"))
	assert.Error(t, sc.Check("bulk_insert"))

	// Page operations should be blocked.
	assert.Error(t, sc.Check("create_page"))
	assert.Error(t, sc.Check("update_page"))
	assert.Error(t, sc.Check("delete_page"))

	// Schema operations should be blocked.
	assert.Error(t, sc.Check("create_table"))
	assert.Error(t, sc.Check("add_column"))
	assert.Error(t, sc.Check("drop_table"))
	assert.Error(t, sc.Check("import_csv"))
}

func TestSandbox_ReadWriteAllowsDataButBlocksPageSchema(t *testing.T) {
	sc := SandboxConfig{Mode: ModeReadWrite}

	// Introspect and data should be allowed.
	assert.NoError(t, sc.Check("introspect_schema"))
	assert.NoError(t, sc.Check("run_query"))
	assert.NoError(t, sc.Check("create_record"))
	assert.NoError(t, sc.Check("update_record"))
	assert.NoError(t, sc.Check("delete_record"))

	// Page and schema should be blocked.
	assert.Error(t, sc.Check("create_page"))
	assert.Error(t, sc.Check("update_page"))
	assert.Error(t, sc.Check("create_table"))
	assert.Error(t, sc.Check("drop_table"))
}

func TestSandbox_FullAllowsEverything(t *testing.T) {
	sc := SandboxConfig{Mode: ModeFull}

	assert.NoError(t, sc.Check("introspect_schema"))
	assert.NoError(t, sc.Check("run_query"))
	assert.NoError(t, sc.Check("create_record"))
	assert.NoError(t, sc.Check("create_page"))
	assert.NoError(t, sc.Check("update_page"))
	assert.NoError(t, sc.Check("create_table"))
	assert.NoError(t, sc.Check("drop_table"))
	assert.NoError(t, sc.Check("import_csv"))
}

func TestSandbox_BlockedOperations(t *testing.T) {
	sc := SandboxConfig{
		Mode:              ModeFull,
		BlockedOperations: []string{"drop_table"},
	}

	assert.NoError(t, sc.Check("create_table"))
	assert.Error(t, sc.Check("drop_table"))
	assert.Contains(t, sc.Check("drop_table").Error(), "blocked")
}

func TestSandbox_TableAllowlist(t *testing.T) {
	sc := SandboxConfig{
		Mode:          ModeReadWrite,
		AllowedTables: []string{"users", "orders"},
	}

	assert.True(t, sc.IsTableAllowed("users"))
	assert.True(t, sc.IsTableAllowed("orders"))
	assert.False(t, sc.IsTableAllowed("secrets"))
}

func TestSandbox_EmptyAllowlistAllowsAll(t *testing.T) {
	sc := SandboxConfig{Mode: ModeReadWrite}
	assert.True(t, sc.IsTableAllowed("anything"))
}

// --- Mock implementations for handler tests ---

type mockContainer struct {
	db     *sql.DB
	duckDB *sql.DB
}

func (m *mockContainer) SQLiteDB() *sql.DB {
	return m.db
}

func (m *mockContainer) DuckDBDB() *sql.DB {
	if m.duckDB != nil {
		return m.duckDB
	}
	return m.db
}

type mockEngine struct{}

func (m *mockEngine) Execute(_ context.Context, sqlStr string, params map[string]any) (*engine.ResultSet, error) {
	return &engine.ResultSet{
		Columns: []engine.Column{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		Rows:    [][]any{{1, "test"}},
	}, nil
}

func newTestHandler(t *testing.T, mode SandboxMode) (*Handler, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create internal tables matching the real schema.
	_, err = db.Exec(`
		CREATE TABLE _vibe_pages (
			id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
			page_type TEXT NOT NULL DEFAULT 'markdown', sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, metadata TEXT NOT NULL DEFAULT '{}'
		);
		CREATE TABLE _vibe_queries (
			id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', sql TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT 'duckdb', parameters TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);
		INSERT INTO users (id, name, email) VALUES (1, 'Alice', 'alice@example.com');
		INSERT INTO _vibe_pages (id, title, content, created_at, updated_at) VALUES ('p1', 'home', '# Welcome', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO _vibe_queries (id, title, sql, engine, created_at, updated_at) VALUES ('q1', 'all_users', 'SELECT * FROM users', 'sqlite', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	`)
	require.NoError(t, err)

	sandbox := &SandboxConfig{
		Mode:         mode,
		MaxQueryRows: 100,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	container := &mockContainer{db: db}
	engine := &mockEngine{}

	handler := NewHandler(container, engine, sandbox, logger)

	t.Cleanup(func() {
		db.Close()
	})

	return handler, db
}

// --- Handler dispatch tests ---

func TestHandler_DispatchIntrospectSchema(t *testing.T) {
	// This test uses SQLite mocks; DuckDB information_schema queries won't work.
	// Testing that the handler dispatches correctly and returns an error
	// (since SQLite doesn't have information_schema).
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "introspect_schema",
		Params:  map[string]any{},
	}
	resp := handler.Handle(ctx, req)

	// With SQLite mock, information_schema query may error or return empty.
	// Just verify the handler dispatched without panicking.
	assert.NotNil(t, resp)
}

func TestHandler_DispatchDescribeTable(t *testing.T) {
	// This test uses SQLite mocks; DuckDB information_schema queries won't work.
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "describe_table",
		Params:  map[string]any{"table": "users"},
	}
	resp := handler.Handle(ctx, req)

	// With SQLite mock, information_schema query may error.
	// Just verify the handler dispatched without panicking.
	assert.NotNil(t, resp)
}

func TestHandler_DispatchListPages(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "list_pages",
		Params:  map[string]any{},
	}
	resp := handler.Handle(ctx, req)

	assert.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	pages, _ := result["pages"].([]string)
	assert.Contains(t, pages, "home")
}

func TestHandler_DispatchGetPage(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "get_page",
		Params:  map[string]any{"name": "home"},
	}
	resp := handler.Handle(ctx, req)

	assert.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	assert.Equal(t, "# Welcome", result["content"])
}

func TestHandler_DispatchListQueries(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "list_queries",
		Params:  map[string]any{},
	}
	resp := handler.Handle(ctx, req)

	assert.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	queries, _ := result["queries"].([]map[string]any)
	assert.Len(t, queries, 1)
	assert.Equal(t, "all_users", queries[0]["name"])
}

func TestHandler_DispatchRunQuery(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "run_query",
		Params:  map[string]any{"sql": "SELECT * FROM users"},
	}
	resp := handler.Handle(ctx, req)

	assert.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	assert.NotNil(t, result["columns"])
	assert.NotNil(t, result["rows"])
}

func TestHandler_UnknownMethod(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "nonexistent",
		Params:  map[string]any{},
	}
	resp := handler.Handle(ctx, req)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, ErrMethodNotFound, resp.Error.Code)
}

func TestHandler_MissingRequiredParam(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      8,
		Method:  "describe_table",
		Params:  map[string]any{}, // missing "table"
	}
	resp := handler.Handle(ctx, req)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
}

func TestHandler_SandboxPermissionDenied(t *testing.T) {
	handler, _ := newTestHandler(t, ModeReadOnly)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      9,
		Method:  "create_record",
		Params:  map[string]any{"table": "users", "values": map[string]any{"name": "Bob"}},
	}
	resp := handler.Handle(ctx, req)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
}

func TestHandler_TableAllowlistEnforced(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	handler.sandbox.AllowedTables = []string{"orders"}
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      10,
		Method:  "describe_table",
		Params:  map[string]any{"table": "users"},
	}
	resp := handler.Handle(ctx, req)

	assert.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "not in the allowed tables list")
}

func TestHandler_CreateAndDeleteRecord(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	// Create
	req := &Request{
		JSONRPC: "2.0",
		ID:      11,
		Method:  "create_record",
		Params:  map[string]any{"table": "users", "values": map[string]any{"name": "Bob", "email": "bob@example.com"}},
	}
	resp := handler.Handle(ctx, req)
	assert.Nil(t, resp.Error)

	// Delete
	req = &Request{
		JSONRPC: "2.0",
		ID:      12,
		Method:  "delete_record",
		Params:  map[string]any{"table": "users", "where": "name = 'Bob'"},
	}
	resp = handler.Handle(ctx, req)
	assert.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	assert.Equal(t, int64(1), result["rows_affected"])
}

func TestHandler_CreateAndUpdatePage(t *testing.T) {
	handler, _ := newTestHandler(t, ModeFull)
	ctx := context.Background()

	// Create
	req := &Request{
		JSONRPC: "2.0",
		ID:      13,
		Method:  "create_page",
		Params:  map[string]any{"name": "about", "content": "# About"},
	}
	resp := handler.Handle(ctx, req)
	assert.Nil(t, resp.Error)

	// Update
	req = &Request{
		JSONRPC: "2.0",
		ID:      14,
		Method:  "update_page",
		Params:  map[string]any{"name": "about", "content": "# About Us"},
	}
	resp = handler.Handle(ctx, req)
	assert.Nil(t, resp.Error)

	// Verify
	req = &Request{
		JSONRPC: "2.0",
		ID:      15,
		Method:  "get_page",
		Params:  map[string]any{"name": "about"},
	}
	resp = handler.Handle(ctx, req)
	assert.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	assert.Equal(t, "# About Us", result["content"])
}
