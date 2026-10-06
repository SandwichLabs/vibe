package main

// #98: revert to last save without closing the file, and the concurrency
// invariants that make it safe (dynamic engine adapter, a DuckDB-handle
// check on a racing approval).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/engine"
)

func TestRevertFile_DiscardsAndStaysOpen(t *testing.T) {
	app := newOpenApp(t)
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())

	_, err := app.InsertRow("orders", map[string]any{"id": 99, "status": "Void"})
	require.NoError(t, err)
	assert.True(t, app.IsDirty())

	prevPath := app.filePath
	prevSession := app.sessionID

	var events []string
	app.emit = func(name string, data ...any) {
		events = append(events, name)
	}

	require.NoError(t, app.RevertFile())

	assert.True(t, app.IsFileOpen())
	assert.False(t, app.IsDirty())
	assert.Equal(t, prevPath, app.filePath)
	assert.Equal(t, prevSession, app.sessionID)
	assert.Equal(t, 3, orderVoidCount(t, app))

	require.GreaterOrEqual(t, len(events), 3)
	idx := map[string]int{}
	for i, e := range events {
		if _, ok := idx[e]; !ok {
			idx[e] = i
		}
	}
	dirtyIdx, hasDirty := idx["file:dirty"]
	dataIdx, hasData := idx["data:changed"]
	revertedIdx, hasReverted := idx["file:reverted"]
	require.True(t, hasDirty)
	require.True(t, hasData)
	require.True(t, hasReverted)
	assert.Less(t, dirtyIdx, dataIdx)
	assert.Less(t, dataIdx, revertedIdx)

	for _, e := range events {
		assert.NotEqual(t, "file:closed", e)
		assert.NotEqual(t, "file:opened", e)
	}
}

func TestRevertFile_EngineRebuilt(t *testing.T) {
	app := newOpenApp(t)
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())

	require.NoError(t, app.RevertFile())

	rs, err := app.ExecuteQuery("SELECT count(*) FROM orders")
	require.NoError(t, err)
	require.NotNil(t, rs)
}

func TestRevertFile_HandlerBuiltBeforeRevertSeesRevertedData(t *testing.T) {
	app := newOpenApp(t)
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())

	_, err := app.InsertRow("orders", map[string]any{"id": 100, "status": "Void"})
	require.NoError(t, err)

	app.mu.RLock()
	handler := app.wrapAgentHandler(&agents.SandboxConfig{Mode: agents.ModeReadOnly, MaxQueryRows: 10000}, agents.ModeReadOnly)
	app.mu.RUnlock()

	require.NoError(t, app.RevertFile())

	req := &agents.Request{JSONRPC: "2.0", ID: "call-1", Method: "run_query", Params: map[string]any{"sql": "SELECT count(*) AS n FROM orders WHERE status = 'Void'"}}
	resp := handler.Handle(context.Background(), req)
	require.Nil(t, resp.Error)
}

func TestRevertFile_MCPServerSurvives(t *testing.T) {
	app := newOpenApp(t)
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())

	url, err := app.StartMCPServer()
	require.NoError(t, err)
	before := app.GetMCPStatus()
	t.Cleanup(func() { _ = app.StopMCPServer() })

	_, err = app.InsertRow("orders", map[string]any{"id": 101, "status": "Void"})
	require.NoError(t, err)

	require.NoError(t, app.RevertFile())

	after := app.GetMCPStatus()
	assert.Equal(t, true, after["running"])
	assert.Equal(t, url, after["url"])
	assert.Equal(t, before["token"], after["token"])
}

func TestRevertFile_CancelsPendingApproval(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())
	app.approvalTimeout = time.Minute

	var mu sync.Mutex
	var announced bool
	app.emit = func(name string, data ...any) {
		if name != "agent:approval" {
			return
		}
		mu.Lock()
		announced = true
		mu.Unlock()
	}

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("revert-t1")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-r1", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	respCh := make(chan *agents.Response, 1)
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return announced
	}, 3*time.Second, 10*time.Millisecond, "approval card never announced")

	require.NoError(t, app.RevertFile())

	select {
	case resp := <-respCh:
		require.NotNil(t, resp.Error)
	case <-time.After(3 * time.Second):
		t.Fatal("pending Handle never returned after RevertFile")
	}

	assert.Equal(t, 3, orderVoidCount(t, app))
}

func TestRevertFile_ApprovedRacingRevertIsCancelled(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())
	app.approvalTimeout = time.Minute

	var mu sync.Mutex
	var id string
	var doneEvents []map[string]any
	app.emit = func(name string, data ...any) {
		if len(data) != 1 {
			return
		}
		m, ok := data[0].(map[string]any)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch name {
		case "agent:approval":
			if req, ok := m["request"].(agents.ApprovalRequest); ok {
				id = req.ID
			}
		case "agent:approval_done":
			doneEvents = append(doneEvents, m)
		}
	}

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("revert-t2")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-r2", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	respCh := make(chan *agents.Response, 1)
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return id != ""
	}, 3*time.Second, 10*time.Millisecond)

	mu.Lock()
	resolveID := id
	mu.Unlock()

	// Make the swap happen BEFORE Resolve, deterministically, the same way
	// TestAgentApproval_OutcomeFollowsIdentityCheck swaps the container
	// before resolving: do RevertFile's own session swap directly under
	// app.mu.Lock() (not app.RevertFile() itself, which would CancelAll the
	// card first and short-circuit the race this test exists to cover).
	// This proves the DuckDB-handle check, not just timing luck — with
	// nothing ordering Resolve against RevertFile, the approver can win the
	// race and run the write before the revert discards it, which is
	// correct product behaviour but makes the test flaky (reviewer-caught).
	app.mu.Lock()
	require.NoError(t, app.container.Revert())
	app.engine = engine.NewManager(app.container.Session(), engine.WithLogger(app.logger))
	app.mu.Unlock()

	require.NoError(t, app.ResolveAgentApproval(resolveID, true))

	resp := <-respCh
	require.NotNil(t, resp.Error)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(doneEvents) == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "cancelled", doneEvents[0]["outcome"])

	assert.Equal(t, 3, orderVoidCount(t, app))
	assert.False(t, app.IsDirty())
}

func TestRevertFile_NotebookSaveStillAccepted(t *testing.T) {
	app := newOpenApp(t)
	seedVoidOrders(t, app)
	require.NoError(t, app.SaveFile())

	sessionID := app.sessionID
	require.NoError(t, app.RevertFile())

	nb := map[string]any{
		"title": "Revert test",
		"cells": []any{},
	}
	_, err := app.SaveNotebook(nb, sessionID)
	assert.NoError(t, err)
}

func TestRevertFile_NoFileOpen(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.closeFile(true))

	err := app.RevertFile()
	assert.Error(t, err)
}
