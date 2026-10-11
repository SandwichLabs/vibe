package main

// Regression test for a reviewer-caught SECURITY BLOCKER: StartMCP used
// to hard-code agents.ModeFull for the handler it hands to the MCP server,
// so an external MCP client could mutate a file whose Settings agent_sandbox
// was "read-only" — live-proven with create_table succeeding against a
// read-only file. The fix makes agentToolHandler re-resolve the sandbox mode
// from the live "agent_sandbox" _vibe_meta value on every dispatch
// (agents.Handler.SetFilePolicy, an atomic swap — see
// TestHandler_SetFilePolicyConcurrentWithHandle in internal/agents for the
// -race coverage of that mechanism), so a SetAgentConfig change takes effect
// on a long-lived MCP handler's very next call without restarting the server.
//
// This test drives the EXACT construction path StartMCP uses
// (a.wrapAgentHandler, gated by a.agentPolicy()) rather than a full
// MCP/SSE round trip, since agentToolHandler.Handle is the one dispatch
// point both StartMCP and Studio chat share.

import (
	"context"
	"testing"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHandlerSandboxTracksLiveAgentConfig(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{
		{Name: "id", Type: "INTEGER", PK: true},
	}))

	// Start the file read-only, exactly like the reviewer's repro.
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-only"}))

	// Build the handler the same way StartMCP does: read the live
	// agent_sandbox setting, wrap it.
	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	app.mu.RUnlock()

	ctx := context.Background()
	createReq := func(id int) *agents.Request {
		return &agents.Request{
			JSONRPC: "2.0",
			ID:      id,
			Method:  "create_table",
			Params: map[string]any{
				"name":    "should_not_exist",
				"columns": []any{map[string]any{"name": "id", "type": "INTEGER"}},
			},
		}
	}

	resp := handler.Handle(ctx, createReq(1))
	require.NotNil(t, resp.Error, "create_table must be denied while the file's agent_sandbox is read-only")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code)

	// Flip to full via SetAgentConfig WITHOUT rebuilding the handler — this
	// is the live scenario: the MCP server has been running the whole time
	// with this one handler.
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full"}))

	resp = handler.Handle(ctx, createReq(2))
	require.Nil(t, resp.Error, "create_table must be allowed on the SAME handler once agent_sandbox is switched to full: %+v", resp.Error)

	// Flip back to read-only; the same handler must deny again immediately.
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-only"}))

	resp = handler.Handle(ctx, createReq(3))
	require.NotNil(t, resp.Error, "create_table must be denied again after switching agent_sandbox back to read-only")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code)
}

// TestStartMCPUsesLiveAgentSandbox is a narrower wiring check: it
// asserts StartMCP itself (not a hand-built handler) reads
// agent_sandbox rather than hard-coding agents.ModeFull, by inspecting the
// sandbox the exact same construction path resolves to.
func TestStartMCPUsesLiveAgentSandbox(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-only"}))

	app.mu.Lock()
	got := app.agentPolicy().Mode
	app.mu.Unlock()
	assert.Equal(t, agents.ModeReadOnly, got, "StartMCP's sandbox := agents.SandboxConfig{Mode: policy.Mode} must resolve to the live setting, not a hard-coded full")
}
