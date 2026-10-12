package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatTurns_OneInFlightAndCancelByID(t *testing.T) {
	var c chatTurns
	ctx, release, err := c.begin(context.Background(), "t1")
	require.NoError(t, err)

	_, _, err = c.begin(context.Background(), "t2")
	require.Error(t, err, "a second concurrent turn must be refused")

	c.cancelTurn("other")
	assert.NoError(t, ctx.Err(), "cancelling another id must not touch this turn")
	c.cancelTurn("t1")
	assert.ErrorIs(t, ctx.Err(), context.Canceled)

	release()
	_, release2, err := c.begin(context.Background(), "t3")
	require.NoError(t, err, "a finished turn frees the slot")
	release2()
	c.cancelTurn("") // no-op with none running
}

// A cancelled turn answers its pending approval card "cancelled" and the
// write never runs.
func TestCancelAgentQuery_DeniesPendingApproval(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = 30 * time.Second

	var mu sync.Mutex
	var outcomes []any
	asked := 0
	app.emit = func(name string, data ...any) {
		mu.Lock()
		defer mu.Unlock()
		if name == "agent:approval" {
			asked++
		}
		if name != "agent:approval_done" || len(data) != 1 {
			return
		}
		if m, ok := data[0].(map[string]any); ok {
			outcomes = append(outcomes, m["outcome"])
		}
	}
	before := orderVoidCount(t, app)
	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("turn-c")
	app.mu.RUnlock()

	ctx, release, err := app.chat.begin(context.Background(), "turn-c")
	require.NoError(t, err)
	defer release()

	respCh := make(chan *agents.Response, 1)
	go func() {
		respCh <- handler.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: "c1", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}})
	}()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return asked == 1 }, 2*time.Second, 10*time.Millisecond)

	app.CancelAgentQuery("turn-c")

	select {
	case resp := <-respCh:
		require.NotNil(t, resp.Error)
	case <-time.After(3 * time.Second):
		t.Fatal("tool call did not return after CancelAgentQuery")
	}
	mu.Lock()
	assert.Equal(t, []any{"cancelled"}, outcomes)
	mu.Unlock()
	assert.Equal(t, before, orderVoidCount(t, app), "the cancelled write must not have run")
}

func TestCloseWorkspace_CancelsInFlightChatTurn(t *testing.T) {
	app := newOpenApp(t)
	ctx, release, err := app.chat.begin(context.Background(), "turn-x")
	require.NoError(t, err)
	defer release()

	require.NoError(t, app.CloseWorkspace())
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

// "Do it" arriving together with a Stop must never run the write: the approver
// re-checks the turn context after Wait says approved.
func TestApproval_ApprovedButTurnCancelled_DoesNotRun(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = 5 * time.Second
	before := orderVoidCount(t, app)

	for i := 0; i < 20; i++ {
		var mu sync.Mutex
		var outcome any
		turn := "race-" + string(rune('a'+i))
		app.emit = func(name string, data ...any) {
			switch name {
			case "agent:approval":
				// Both the approval and the cancel are pending before Wait selects.
				m, _ := data[0].(map[string]any)
				req, _ := m["request"].(agents.ApprovalRequest)
				app.approvals.Resolve(req.ID, true)
				app.chat.cancelTurn(turn)
			case "agent:approval_done":
				mu.Lock()
				m, _ := data[0].(map[string]any)
				outcome = m["outcome"]
				mu.Unlock()
			}
		}
		app.mu.RLock()
		handler := app.newAgentToolHandler(agents.ModeFull)
		handler.enableApprovals(turn)
		app.mu.RUnlock()
		ctx, release, err := app.chat.begin(context.Background(), turn)
		require.NoError(t, err)

		resp := handler.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: "c", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}})
		release()
		require.NotNil(t, resp.Error, "iteration %d: write ran after cancel", i)
		mu.Lock()
		assert.Equal(t, "cancelled", outcome)
		mu.Unlock()
		require.Equal(t, before, orderVoidCount(t, app))
	}
}
