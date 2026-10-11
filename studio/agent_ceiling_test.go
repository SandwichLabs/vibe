package main

// Regression test for a reviewer-caught SECURITY BLOCKER (round 2 of the
// #68 review): the per-dispatch sandbox refresh added to fix MCP running at
// a hard-coded ModeFull (see mcp_sandbox_test.go) had a second-order bug —
// it also overwrote AgentTask's forced read-only mode on every dispatch,
// since AgentTask built its handler the same way as chat (newAgentToolHandler
// then re-reading the file's live agent_sandbox on every call). Against a
// "full" file, a task turn's run_query could run CREATE TABLE again — a
// reviewer proved it with a Go probe and live (the task created
// stock_summary).
//
// The fix gives agentToolHandler a ceiling (agents.SandboxMode) fixed at
// construction: AgentTask's is ModeReadOnly and can never be loosened by the
// per-dispatch refresh; chat/MCP/RunA2UIQuery's is ModeFull (no additional
// cap beyond the file). Every dispatch computes agents.Tighter(ceiling,
// fileMode), never fileMode alone.

import (
	"context"
	"testing"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ddlReq(id int) *agents.Request {
	return &agents.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "run_query",
		Params:  map[string]any{"sql": "CREATE TABLE stock_summary AS SELECT 1"},
	}
}

func createTableReq(id int) *agents.Request {
	return &agents.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "create_table",
		Params: map[string]any{
			"name":    "pwned",
			"columns": []any{map[string]any{"name": "id", "type": "INTEGER"}},
		},
	}
}

// TestAgentTaskCeiling_StaysReadOnlyAgainstFullFile is the direct regression
// guard: build the handler exactly the way AgentTask does
// (newAgentToolHandler(agents.ModeReadOnly), the fixed ceiling) against a
// file whose agent_sandbox is "full", and prove run_query DDL and
// create_table are BOTH denied, and no table is actually created.
func TestAgentTaskCeiling_StaysReadOnlyAgainstFullFile(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full"}))

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeReadOnly) // AgentTask's exact call
	app.mu.RUnlock()

	ctx := context.Background()

	resp := handler.Handle(ctx, ddlReq(1))
	require.NotNil(t, resp.Error, "task-mode run_query DDL must be denied even though the file is full")

	resp = handler.Handle(ctx, createTableReq(2))
	require.NotNil(t, resp.Error, "task-mode create_table must be denied even though the file is full")

	tables, err := app.ListTables()
	require.NoError(t, err)
	assert.NotContains(t, tables, "stock_summary", "the DDL must not have actually run")
	assert.NotContains(t, tables, "pwned", "create_table must not have actually run")
}

// TestChatCeiling_DeniedWhenFileIsReadOnly asserts chat's ceiling (ModeFull)
// does NOT grant anything beyond the file's own setting: with agent_sandbox
// = "read-only", a chat-built handler still denies a write.
func TestChatCeiling_DeniedWhenFileIsReadOnly(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-only"}))

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull) // AgentChat's exact call
	app.mu.RUnlock()

	resp := handler.Handle(context.Background(), createTableReq(1))
	require.NotNil(t, resp.Error, "chat must be denied create_table when the file is read-only")
}

// TestMCPCeiling_DeniedWhenFileIsReadOnly is the MCP-path equivalent, built
// the way StartMCP does (wrapAgentHandler + ceiling ModeFull).
func TestMCPCeiling_DeniedWhenFileIsReadOnly(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-only"}))

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull) // StartMCP's exact call
	app.mu.RUnlock()

	resp := handler.Handle(context.Background(), createTableReq(1))
	require.NotNil(t, resp.Error, "MCP must be denied create_table when the file is read-only")
}

// TestChatCeiling_ReadWriteFileWithFullCeilingGivesReadWrite proves Tighter
// resolves correctly in the middle case: a read-write file with a ModeFull
// ceiling (chat's own cap) must land at read-write — data tools allowed,
// schema tools still denied.
func TestChatCeiling_ReadWriteFileWithFullCeilingGivesReadWrite(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-write"}))

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	app.mu.RUnlock()

	ctx := context.Background()

	// Data category (create_record) must be ALLOWED at read-write.
	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0", ID: 1, Method: "create_record",
		Params: map[string]any{"table": "widgets", "values": map[string]any{"id": 1}},
	})
	require.Nil(t, resp.Error, "read-write must allow create_record: %+v", resp.Error)

	// Schema category (create_table) must still be DENIED at read-write.
	resp = handler.Handle(ctx, createTableReq(2))
	require.NotNil(t, resp.Error, "read-write must still deny create_table (schema category)")
}
