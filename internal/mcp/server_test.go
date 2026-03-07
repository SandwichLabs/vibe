package mcp

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockContainer struct {
	db *sql.DB
}

func (m *mockContainer) SQLiteDB() *sql.DB { return m.db }
func (m *mockContainer) DuckDBDB() *sql.DB { return m.db }

type mockEngine struct{}

func (m *mockEngine) Execute(_ context.Context, sqlStr string, params map[string]any) (*engine.ResultSet, error) {
	return &engine.ResultSet{
		Columns: []engine.Column{{Name: "id", Type: "INTEGER"}},
		Rows:    [][]any{{1}},
	}, nil
}

func TestToolRegistration(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE _vibe_pages (name TEXT PRIMARY KEY, content TEXT);
		CREATE TABLE _vibe_queries (name TEXT PRIMARY KEY, sql_text TEXT, engine TEXT);
	`)
	require.NoError(t, err)

	sandbox := &agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := agents.NewHandler(&mockContainer{db: db}, &mockEngine{}, sandbox, logger)

	srv := New(handler, logger)
	require.NotNil(t, srv)

	allTools := agents.ListTools()
	assert.Greater(t, len(allTools), 0)

	for _, td := range allTools {
		mcpTool := toolDefToMCPTool(td)
		assert.Equal(t, td.Name, mcpTool.Name)
		assert.Equal(t, td.Description, mcpTool.Description)
	}
}

func TestStartAndStop(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE _vibe_pages (name TEXT PRIMARY KEY, content TEXT);
		CREATE TABLE _vibe_queries (name TEXT PRIMARY KEY, sql_text TEXT, engine TEXT);
	`)
	require.NoError(t, err)

	sandbox := &agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := agents.NewHandler(&mockContainer{db: db}, &mockEngine{}, sandbox, logger)

	srv := New(handler, logger)
	err = srv.Start("127.0.0.1:0")
	require.NoError(t, err)

	addr := srv.Addr()
	assert.NotEmpty(t, addr)
	t.Logf("MCP server listening on %s", addr)

	err = srv.Stop()
	assert.NoError(t, err)
	assert.Empty(t, srv.Addr())
}

func TestParamTypeConversion(t *testing.T) {
	assert.Equal(t, "string", paramTypeToJSONSchema("string"))
	assert.Equal(t, "boolean", paramTypeToJSONSchema("boolean"))
	assert.Equal(t, "object", paramTypeToJSONSchema("object"))
	assert.Equal(t, "array", paramTypeToJSONSchema("array"))
	assert.Equal(t, "string", paramTypeToJSONSchema("unknown"))
}
