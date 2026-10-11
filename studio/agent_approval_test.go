package main

// #79: agent write safety — per-file "ask before changes" default, the
// approval gate wired through App, and the concurrency invariant that makes
// it safe (the approver releases a.mu.RLock for the whole wait so Save/
// Close/SetAgentConfig never queue behind a pending card).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func TestAgentPolicy_FreshFile(t *testing.T) {
	app := newOpenApp(t)
	cfg, err := app.GetAgentConfig()
	require.NoError(t, err)
	assert.Equal(t, "read-write", cfg.Sandbox)
	assert.True(t, cfg.AskBeforeWrites)
}

func TestSetAgentConfig_ValidatesAndEmits(t *testing.T) {
	app := newOpenApp(t)

	err := app.SetAgentConfig(AgentConfig{Sandbox: "godmode"})
	assert.Error(t, err)
	cfg, _ := app.GetAgentConfig()
	assert.Equal(t, "read-write", cfg.Sandbox, "an invalid sandbox value must not be written")

	var events []map[string]any
	app.emit = func(name string, data ...any) {
		if name == "agent:config" && len(data) == 1 {
			if m, ok := data[0].(map[string]any); ok {
				events = append(events, m)
			}
		}
	}

	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: false}))
	require.Len(t, events, 1)
	assert.Equal(t, "full", events[0]["sandbox"])
	assert.Equal(t, false, events[0]["askBeforeWrites"])

	cfg, err = app.GetAgentConfig()
	require.NoError(t, err)
	assert.Equal(t, "full", cfg.Sandbox)
	assert.False(t, cfg.AskBeforeWrites)
}

func TestResolveAgentApproval_UnknownID(t *testing.T) {
	app := newOpenApp(t)
	err := app.ResolveAgentApproval("nope", true)
	assert.Error(t, err)
}

// seedVoidOrders creates a 3-Void-row "orders" table for the approval tests
// below.
func seedVoidOrders(t *testing.T, app *App) {
	t.Helper()
	_, err := app.engine.Execute(context.Background(),
		"CREATE TABLE orders (id INTEGER, status VARCHAR); INSERT INTO orders VALUES (1,'Void'),(2,'Paid'),(3,'Void'),(4,'Void')", nil)
	require.NoError(t, err)
}

func orderVoidCount(t *testing.T, app *App) int {
	t.Helper()
	rs, err := app.engine.Execute(context.Background(), "SELECT count(*) FROM orders WHERE status = 'Void'", nil)
	require.NoError(t, err)
	n, _ := rs.Rows[0][0].(int64)
	return int(n)
}

// TestAgentApproval_Chat drives the exact construction AgentChatStream uses
// (newAgentToolHandler + enableApprovals), dispatching delete_record
// directly rather than through a real LLM turn — the LLM/runner layer is
// exercised separately by the QA script; this test is about the approval
// plumbing and the lock-release invariant.
func TestAgentApproval_Chat(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = 2 * time.Second

	var mu sync.Mutex
	var approvalEvents []map[string]any
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
			approvalEvents = append(approvalEvents, m)
		case "agent:approval_done":
			doneEvents = append(doneEvents, m)
		}
	}

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("t1")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-1", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}

	respCh := make(chan *agents.Response, 1)
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(approvalEvents) == 1
	}, 2*time.Second, 10*time.Millisecond, "expected one agent:approval event")

	mu.Lock()
	ev := approvalEvents[0]
	mu.Unlock()
	assert.Equal(t, "t1", ev["turnId"])
	req, ok := ev["request"].(agents.ApprovalRequest)
	require.True(t, ok)
	assert.Equal(t, "call-1", req.CallID)
	assert.Equal(t, int64(3), req.Rows)

	// While the card is pending, Save and SetAgentConfig must complete
	// promptly — proving enableApprovals actually released a.mu.RLock for
	// the wait, not just around it.
	require.Eventually(t, func() bool {
		_, err := app.GetWorkspaceInfo()
		return err == nil
	}, 2*time.Second, 10*time.Millisecond, "GetWorkspaceInfo must not block behind a pending approval")
	require.Eventually(t, func() bool {
		return app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}) == nil
	}, 2*time.Second, 10*time.Millisecond, "SetAgentConfig must not block behind a pending approval")

	require.NoError(t, app.ResolveAgentApproval(req.ID, true))

	resp := <-respCh
	assert.Nil(t, resp.Error)
	assert.Equal(t, 0, orderVoidCount(t, app))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(doneEvents) == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "approved", doneEvents[0]["outcome"])
}

func TestAgentApproval_Decline(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = 2 * time.Second

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("t2")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-2", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	respCh := make(chan *agents.Response, 1)
	var mu sync.Mutex
	var id string
	app.emit = func(name string, data ...any) {
		if name != "agent:approval" || len(data) != 1 {
			return
		}
		if m, ok := data[0].(map[string]any); ok {
			if req, ok := m["request"].(agents.ApprovalRequest); ok {
				mu.Lock()
				id = req.ID
				mu.Unlock()
			}
		}
	}
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return id != ""
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	resolveID := id
	mu.Unlock()
	require.NoError(t, app.ResolveAgentApproval(resolveID, false))

	resp := <-respCh
	require.NotNil(t, resp.Error)
	assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code)
	assert.Equal(t, 3, orderVoidCount(t, app), "a decline must leave the rows unchanged")
}

func TestAgentApproval_Expired(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = 50 * time.Millisecond

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("t3")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-3", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	resp := handler.Handle(context.Background(), deleteReq)
	require.NotNil(t, resp.Error)
	assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code)
	assert.Equal(t, 3, orderVoidCount(t, app))
}

// TestAgentApproval_CloseWhilePending proves CancelAll actually does its
// job, not just that the turn eventually gives up (round 2 review finding
// 5): approvalTimeout is set to a full minute, so if the response only came
// back because the OLD 2s test timeout expired, this test would time out
// waiting for it — passing here means CloseFile's CancelAll genuinely ended
// the wait early, well under that minute. Waits on the "agent:approval"
// event (Eventually) instead of a fixed sleep, so the assertion doesn't
// depend on how fast the approval goroutine happens to reach the wait.
func TestAgentApproval_CloseWhilePending(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
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
	handler.enableApprovals("t4")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-4", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	respCh := make(chan *agents.Response, 1)
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return announced
	}, 3*time.Second, 10*time.Millisecond, "approval card never announced")

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- app.CloseWorkspace() }()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("CloseFile deadlocked behind the pending approval")
	}

	select {
	case resp := <-respCh:
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "closed", "expected the cancelled-by-close message, got: %s", resp.Error.Message)
	case <-time.After(3 * time.Second):
		t.Fatal("pending Handle never returned after CloseFile")
	}

	// The whole round trip must finish in a few seconds, nowhere near the
	// 1-minute approvalTimeout — if it took anywhere close to that, CancelAll
	// didn't actually cancel anything and the turn only ended because
	// something else (a stray shorter deadline) intervened.
	assert.Less(t, time.Since(start), 5*time.Second, "CloseFile should end the pending approval immediately, not wait out approvalTimeout")
}

// TestAgentApproval_OutcomeFollowsIdentityCheck proves the container-identity
// check runs BEFORE the outcome is derived/emitted (round 2 review finding
// 4): the file is swapped out from under a pending card WITHOUT going
// through CloseFile/OpenFile's CancelAll (which would short-circuit the very
// path this test targets), so a bare Resolve(true) racing the swap must
// still surface as "cancelled" — never "approved" — and the write must never
// reach the container that's no longer the open one.
func TestAgentApproval_OutcomeFollowsIdentityCheck(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
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
	handler.enableApprovals("t5")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-5", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	respCh := make(chan *agents.Response, 1)
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return id != ""
	}, 3*time.Second, 10*time.Millisecond)

	// Swap the open container out from under the pending card directly
	// (bypassing CloseFile's CancelAll) — this is exactly the race
	// enableApprovals' post-wait identity check exists to close.
	staleWS, staleSess := app.ws, app.sess
	otherWS, err := workspace.Open(newWorkspaceDir(t, "Other"), workspace.WithLogger(app.logger))
	require.NoError(t, err)
	otherSess, _, err := workspace.OpenSession(context.Background(), otherWS, app.logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherSess.Close(); _ = otherWS.Close() })
	app.mu.Lock()
	app.ws, app.sess = otherWS, otherSess
	app.mu.Unlock()

	mu.Lock()
	resolveID := id
	mu.Unlock()
	require.NoError(t, app.ResolveAgentApproval(resolveID, true))

	resp := <-respCh
	require.NotNil(t, resp.Error)
	assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(doneEvents) == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "cancelled", doneEvents[0]["outcome"], "the outcome must follow the identity check, not the raw Resolve(true)")

	// The original (now-stale) container's rows must be untouched.
	app.mu.Lock()
	app.ws, app.sess = staleWS, staleSess
	app.mu.Unlock()
	assert.Equal(t, 3, orderVoidCount(t, app))
}

// TestAgentApproval_CancelRaceWithRegistration stresses the narrow window
// between a request registering with the ApprovalQueue and enableApprovals
// releasing a.mu.RLock (round 2 review finding 6): CloseFile fires
// immediately after Handle starts, with no wait for the "agent:approval"
// event to land first, instead of only after the card is safely announced
// (as every other test in this file does). Every iteration must cancel
// promptly — never hang, never silently let the write through.
func TestAgentApproval_CancelRaceWithRegistration(t *testing.T) {
	for i := 0; i < 20; i++ {
		app := newOpenApp(t)
		require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
		seedVoidOrders(t, app)
		app.approvalTimeout = time.Minute

		app.mu.RLock()
		handler := app.newAgentToolHandler(agents.ModeFull)
		handler.enableApprovals("race")
		app.mu.RUnlock()

		deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-race", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
		respCh := make(chan *agents.Response, 1)
		go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

		require.NoError(t, app.CloseWorkspace(), "iteration %d", i)

		select {
		case resp := <-respCh:
			require.NotNil(t, resp.Error, "iteration %d: expected the pending write to be refused", i)
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d: Handle never returned after CloseFile", i)
		}
	}
}

// TestAgentApproval_PolicyTightenedMidWait proves the write is re-checked
// against the file's LIVE policy once approved, not the policy loaded before
// the wait started: the mode tightens to read-only WHILE the card is
// pending, after being built under "full". A human did click Do it, but
// that must not override a policy change that landed in between.
func TestAgentApproval_PolicyTightenedMidWait(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = time.Minute

	var mu sync.Mutex
	var id string
	app.emit = func(name string, data ...any) {
		if name != "agent:approval" || len(data) != 1 {
			return
		}
		if m, ok := data[0].(map[string]any); ok {
			if req, ok := m["request"].(agents.ApprovalRequest); ok {
				mu.Lock()
				id = req.ID
				mu.Unlock()
			}
		}
	}

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	handler.enableApprovals("t6")
	app.mu.RUnlock()

	deleteReq := &agents.Request{JSONRPC: "2.0", ID: "call-6", Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}}
	respCh := make(chan *agents.Response, 1)
	go func() { respCh <- handler.Handle(context.Background(), deleteReq) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return id != ""
	}, 3*time.Second, 10*time.Millisecond)

	// Tighten the file's mode WHILE the card is pending — the delete was
	// allowed under "full" when the card was built.
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "read-only", AskBeforeWrites: true}))

	mu.Lock()
	resolveID := id
	mu.Unlock()
	require.NoError(t, app.ResolveAgentApproval(resolveID, true))

	resp := <-respCh
	require.NotNil(t, resp.Error, "an approval under a since-tightened mode must not run the write")
	assert.Equal(t, 3, orderVoidCount(t, app), "the tightened policy must win: rows must be unchanged")
}

// TestStartMCP_WritesGoThroughApprovalCard: Studio's own MCP server asks the
// person, through the same approval card the chat agent uses (turn "mcp"),
// instead of refusing. A decline leaves the data alone; an approval runs it.
func TestStartMCP_WritesGoThroughApprovalCard(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: true}))
	seedVoidOrders(t, app)
	app.approvalTimeout = 5 * time.Second

	var mu sync.Mutex
	var cards []map[string]any
	app.emit = func(name string, data ...any) {
		if name != "agent:approval" || len(data) != 1 {
			return
		}
		if m, ok := data[0].(map[string]any); ok {
			mu.Lock()
			cards = append(cards, m)
			mu.Unlock()
		}
	}

	st, err := app.StartMCP()
	require.NoError(t, err)
	require.True(t, st.Running)
	require.NotEmpty(t, st.URL)
	t.Cleanup(func() { _ = app.StopMCP() })

	app.mu.RLock()
	handler := app.mcpTool
	app.mu.RUnlock()
	require.NotNil(t, handler)

	del := func(id int) chan *agents.Response {
		ch := make(chan *agents.Response, 1)
		go func() {
			ch <- handler.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: id, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status = 'Void'"}})
		}()
		return ch
	}
	cardID := func(n int) string {
		var id string
		require.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			if len(cards) < n {
				return false
			}
			assert.Equal(t, "mcp", cards[n-1]["turnId"])
			req, _ := cards[n-1]["request"].(agents.ApprovalRequest)
			id = req.ID
			return id != ""
		}, 3*time.Second, 10*time.Millisecond)
		return id
	}

	declined := del(1)
	require.NoError(t, app.ResolveAgentApproval(cardID(1), false))
	resp := <-declined
	require.NotNil(t, resp.Error, "a declined card must not run the write")
	assert.Equal(t, 3, orderVoidCount(t, app))

	approved := del(2)
	require.NoError(t, app.ResolveAgentApproval(cardID(2), true))
	resp = <-approved
	require.Nil(t, resp.Error, "%+v", resp.Error)
	assert.Equal(t, 0, orderVoidCount(t, app))
}
