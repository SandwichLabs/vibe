package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
)

// promptCeiling is the size ceiling (bytes) for the chat system prompt without a
// skills block. Before #212 the prompt was 17-18 KB, of which ~7.4 KB repeated
// every tool description the provider already receives. Raise it only on
// purpose, with a reason.
const promptCeiling = 11500

func TestSystemPromptFor_SizeAndContent(t *testing.T) {
	const dir = "/home/me/ws/data"
	for _, sandbox := range []string{SandboxReadOnly, SandboxReadWrite, SandboxFull} {
		p := systemPromptFor(sandbox, true, "", dir)
		assert.LessOrEqual(t, len(p), promptCeiling, "%s prompt grew past the ceiling", sandbox)

		// Every chat tool is named (the map hides nothing)...
		for _, td := range agents.ListToolsForMode(agents.ModeChat) {
			assert.Contains(t, p, td.Name, "tool %s missing from the map", td.Name)
			// ...but its description is not repeated: the provider has it.
			assert.NotContains(t, p, td.Description, "description of %s repeated", td.Name)
		}
		// Task-only tools are never promised in chat.
		for _, n := range []string{"write_cell", "need_more_info"} {
			assert.NotContains(t, p, n)
		}

		assert.Contains(t, p, dir, "absolute data/ path")
		assert.Contains(t, p, fmt.Sprintf("at most %d steps", maxSteps), "step budget")
		assert.Contains(t, p, "alias every aggregate", "aggregate alias rule")
		assert.Contains(t, p, "`SELECT count(*) days`", "keyword alias example")
		assert.Contains(t, p, "AS days")
		assert.Contains(t, p, "$name", "run_query binds $name")
		if sandbox != SandboxReadOnly {
			assert.Contains(t, p, "add_markdown_cell")
			assert.NotContains(t, p, "NO tool that writes a markdown cell")
		}
	}
	assert.GreaterOrEqual(t, maxSteps, 16)
	assert.LessOrEqual(t, maxSteps, 24)
}

func TestSystemPromptFor_DataDirFallback(t *testing.T) {
	p := systemPromptFor(SandboxReadOnly, false, "", "")
	assert.Contains(t, p, "list_connections returns its path")
	// A path that could break the SQL literal or the prompt is not echoed.
	p = systemPromptFor(SandboxReadOnly, false, "", "/x/it's\nbad")
	assert.NotContains(t, p, "it's\nbad")
	assert.Contains(t, p, "list_connections returns its path")
}

// alwaysToolCallSSE answers every request with a list_skills tool call, so the
// loop only ends when the step cap stops it.
func alwaysToolCallSSE(t *testing.T, requests *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		n := atomic.AddInt32(requests, 1)
		if int(n) > maxSteps+5 {
			sseFinalTextResponse(t, w, "runaway")
			return
		}
		sseToolCallResponse(t, w, "list_skills", "{}")
	}))
}

func TestChatStream_StepCapEndsWithVisibleNote(t *testing.T) {
	var requests int32
	srv := alwaysToolCallSSE(t, &requests)
	defer srv.Close()

	runner := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	var events []StreamEvent
	res, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "keep going", "", nil, func(ev StreamEvent) { events = append(events, ev) })
	require.NoError(t, err)

	assert.Equal(t, int32(maxSteps), atomic.LoadInt32(&requests), "the loop must stop at the cap")
	assert.Len(t, res.Steps, maxSteps)
	assert.Contains(t, res.Text, stepLimitNote())
	assert.Contains(t, res.Text, fmt.Sprintf("Stopped after %d steps", maxSteps))
	last := events[len(events)-1]
	assert.Equal(t, "final", last.Kind)
	assert.Contains(t, last.Text, stepLimitNote())
}

func TestChatStream_NormalTurnHasNoStepLimitNote(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if atomic.AddInt32(&requests, 1) == 1 {
			sseToolCallResponse(t, w, "list_skills", "{}")
			return
		}
		sseFinalTextResponse(t, w, "You have one skill.")
	}))
	defer srv.Close()

	runner := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	res, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "skills?", "", nil, nil)
	require.NoError(t, err)
	assert.NotContains(t, res.Text, "Stopped after")
	assert.False(t, strings.Contains(res.Text, stepLimitNote()))
}

func TestAppendStepLimitNote(t *testing.T) {
	assert.Equal(t, stepLimitNote(), appendStepLimitNote("  "))
	assert.Equal(t, "partial\n\n"+stepLimitNote(), appendStepLimitNote("partial\n"))
}

func TestDataDirFor_DegradesToEmpty(t *testing.T) {
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	// The mock access has no session, so list_connections errors; no panic, "".
	assert.Equal(t, "", r.dataDirFor(context.Background()))
}

// stubHandler answers list_connections with a fixed result.
type stubHandler struct{ result map[string]any }

func (h stubHandler) Handle(_ context.Context, req *agents.Request) *agents.Response {
	return agents.NewResponse(req.ID, h.result)
}

func TestDataDirFor_ReadsDataDirKey(t *testing.T) {
	r := NewRunner(stubHandler{map[string]any{"data_dir": "/x/data", "connections": []any{}}}, nil)
	dir := r.dataDirFor(context.Background())
	assert.Equal(t, "/x/data", dir)
	assert.Contains(t, systemPromptFor(SandboxReadOnly, false, "", dir), "/x/data/sales.csv")
}
