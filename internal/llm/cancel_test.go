package llm

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelAfterFirstTool cancels the turn as soon as its first tool call
// returns, standing in for the person pressing Cancel mid-turn.
type cancelAfterFirstTool struct {
	inner  Handler
	cancel context.CancelFunc
	calls  atomic.Int32
}

func (h *cancelAfterFirstTool) Handle(ctx context.Context, req *agents.Request) *agents.Response {
	resp := h.inner.Handle(ctx, req)
	if req.ID == "system-prompt-skills" { // prompt assembly, not a model tool call
		return resp
	}
	h.calls.Add(1)
	h.cancel()
	return resp
}

func TestChatStream_CancelMidTurnStopsBeforeNextStep(t *testing.T) {
	srv := newFakeResponsesServer(t, "list_skills", "{}", "never reached")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &cancelAfterFirstTool{inner: newTestHandler(t, agents.ModeReadOnly), cancel: cancel}
	before := runtime.NumGoroutine()
	runner := NewRunner(h, nil)
	runner.openAIBaseURL = srv.URL

	var kinds []string
	res, err := runner.ChatStream(ctx, Config{Provider: ProviderOpenAI, APIKey: openAITestKey, Sandbox: SandboxReadOnly},
		"what skills?", "", nil, func(ev StreamEvent) { kinds = append(kinds, ev.Kind) })

	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	assert.Equal(t, int32(1), h.calls.Load(), "no tool may run after the cancel")
	_, _, bodies := srv.snapshot()
	assert.Len(t, bodies, 1, "no model step may start after the cancel")
	assert.Contains(t, kinds, "cancelled")
	assert.NotContains(t, kinds, "error")
	assert.NotContains(t, kinds, "final")

	srv.CloseClientConnections()
	http.DefaultClient.CloseIdleConnections()
	assert.Eventually(t, func() bool { return runtime.NumGoroutine() <= before+2 }, 3*time.Second, 50*time.Millisecond, "goroutines leaked after cancel")
}

func TestHandlerTool_RefusesToRunOnCancelledContext(t *testing.T) {
	h := &cancelAfterFirstTool{inner: newTestHandler(t, agents.ModeReadOnly), cancel: func() {}}
	tools := NewRunner(h, nil).buildTools(&recorder{})
	require.NotEmpty(t, tools)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tools[0].Run(ctx, fantasy.ToolCall{ID: "c1", Name: tools[0].Info().Name, Input: "{}"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(0), h.calls.Load())
}
