package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/config"
)

const openAITestKey = "sk-test-NEEDLE-0123456789"

// fakeResponsesServer emulates OpenAI's POST /v1/responses. On a request with
// no function_call_output in its input it asks for the named tool; once the
// tool result is in the input it answers with finalText. With stream=true it
// speaks the SSE event protocol, otherwise plain JSON. It records the
// Authorization header and each request body.
type fakeResponsesServer struct {
	*httptest.Server
	mu     sync.Mutex
	auths  []string
	paths  []string
	bodies []map[string]any
}

func newFakeResponsesServer(t *testing.T, toolName, toolArgs, finalText string) *fakeResponsesServer {
	t.Helper()
	f := &fakeResponsesServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.paths = append(f.paths, r.URL.Path)
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()

		sawResult := strings.Contains(string(raw), `"function_call_output"`)
		stream, _ := body["stream"].(bool)

		var item, event map[string]any
		if !sawResult {
			item = map[string]any{
				"id": "fc_1", "type": "function_call", "status": "completed",
				"call_id": "call_1", "name": toolName, "arguments": toolArgs,
			}
		} else {
			item = map[string]any{
				"id": "msg_1", "type": "message", "status": "completed", "role": "assistant",
				"content": []map[string]any{{"type": "output_text", "text": finalText, "annotations": []any{}}},
			}
		}
		resp := map[string]any{
			"id": "resp_1", "object": "response", "created_at": 1, "status": "completed",
			"model": "gpt-5.5", "output": []any{item},
			"usage": map[string]any{
				"input_tokens": 1, "output_tokens": 1, "total_tokens": 2,
				"input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 0},
			},
		}
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(e map[string]any) {
			b, _ := json.Marshal(e)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e["type"], b)
		}
		inProgress := map[string]any{"id": "resp_1", "object": "response", "status": "in_progress", "model": "gpt-5.5", "output": []any{}}
		send(map[string]any{"type": "response.created", "sequence_number": 0, "response": inProgress})
		if !sawResult {
			added := map[string]any{"id": "fc_1", "type": "function_call", "status": "in_progress", "call_id": "call_1", "name": toolName, "arguments": ""}
			send(map[string]any{"type": "response.output_item.added", "sequence_number": 1, "output_index": 0, "item": added})
			send(map[string]any{"type": "response.function_call_arguments.delta", "sequence_number": 2, "output_index": 0, "item_id": "fc_1", "delta": toolArgs})
			event = map[string]any{"type": "response.output_item.done", "sequence_number": 3, "output_index": 0, "item": item}
		} else {
			added := map[string]any{"id": "msg_1", "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}
			send(map[string]any{"type": "response.output_item.added", "sequence_number": 1, "output_index": 0, "item": added})
			send(map[string]any{"type": "response.output_text.delta", "sequence_number": 2, "output_index": 0, "content_index": 0, "item_id": "msg_1", "delta": finalText})
			event = map[string]any{"type": "response.output_item.done", "sequence_number": 3, "output_index": 0, "item": item}
		}
		send(event)
		send(map[string]any{"type": "response.completed", "sequence_number": 4, "response": resp})
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeResponsesServer) snapshot() (auths, paths []string, bodies []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...), append([]string(nil), f.paths...), append([]map[string]any(nil), f.bodies...)
}

func openAIRunner(t *testing.T, baseURL string, mode agents.SandboxMode) *Runner {
	t.Helper()
	r := NewRunner(newTestHandler(t, mode), nil)
	r.openAIBaseURL = baseURL
	return r
}

func TestOpenAI_Chat_ToolCallRoundTrip(t *testing.T) {
	srv := newFakeResponsesServer(t, "list_skills", "{}", "You have a Sales dashboard skill.")
	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)

	res, err := r.Chat(context.Background(), Config{
		Provider: ProviderOpenAI, APIKey: openAITestKey, Sandbox: SandboxReadOnly,
	}, "What skills do I have?", "")
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "list_skills", res.Steps[0].Tool)
	assert.Contains(t, res.Steps[0].Output, "sales-dashboard")
	assert.Contains(t, res.Text, "Sales dashboard")

	auths, paths, bodies := srv.snapshot()
	require.Len(t, bodies, 2, "one request for the tool call, one for the answer")
	for i := range bodies {
		assert.Equal(t, "Bearer "+openAITestKey, auths[i])
		assert.Equal(t, "/responses", paths[i], "the Responses API, not chat completions")
		assert.Equal(t, config.DefaultModelFor(ProviderOpenAI), bodies[i]["model"])
	}
	// gpt-5.5 is a reasoning model: the per-mode effort is forwarded.
	reasoning, _ := bodies[0]["reasoning"].(map[string]any)
	assert.Equal(t, "medium", reasoning["effort"])
}

func TestOpenAI_ChatStream_ToolCallRoundTrip(t *testing.T) {
	srv := newFakeResponsesServer(t, "list_skills", "{}", "You have a Sales dashboard skill.")
	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)

	var mu sync.Mutex
	var deltas []string
	res, err := r.ChatStream(context.Background(), Config{
		Provider: ProviderOpenAI, APIKey: openAITestKey, Sandbox: SandboxReadOnly,
	}, "What skills do I have?", "", nil, func(ev StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		if ev.Kind == "delta" {
			deltas = append(deltas, ev.Text)
		}
	})
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "list_skills", res.Steps[0].Tool)
	assert.Contains(t, res.Text, "Sales dashboard")
	assert.NotEmpty(t, deltas)
}

func TestOpenAI_NonReasoningModelGetsNoReasoningParam(t *testing.T) {
	srv := newFakeResponsesServer(t, "list_skills", "{}", "ok")
	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)

	_, err := r.Chat(context.Background(), Config{
		Provider: ProviderOpenAI, APIKey: openAITestKey, Model: "gpt-4o", Sandbox: SandboxReadOnly,
	}, "hi", "")
	require.NoError(t, err)
	_, paths, bodies := srv.snapshot()
	require.NotEmpty(t, bodies)
	assert.Equal(t, "/responses", paths[0])
	assert.Equal(t, "gpt-4o", bodies[0]["model"])
	assert.NotContains(t, bodies[0], "reasoning")
}

func TestOpenAI_UnknownModelIDStillUsesResponses(t *testing.T) {
	srv := newFakeResponsesServer(t, "list_skills", "{}", "ok")
	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)

	_, err := r.Chat(context.Background(), Config{
		Provider: ProviderOpenAI, APIKey: openAITestKey, Model: "ft:my-model", Sandbox: SandboxReadOnly,
	}, "hi", "")
	require.NoError(t, err)
	_, paths, _ := srv.snapshot()
	require.NotEmpty(t, paths)
	assert.Equal(t, "/responses", paths[0])
}

func TestOpenAI_Task_WriteCell(t *testing.T) {
	args := `{"cell_type":"markdown","markdown":"# Hello","explanation":"a note"}`
	srv := newFakeResponsesServer(t, "write_cell", args, "unused")
	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)

	env, err := r.Task(context.Background(), Config{
		Provider: ProviderOpenAI, APIKey: openAITestKey, Sandbox: SandboxReadOnly,
	}, TaskRequest{Mode: "create", Description: "add an intro note"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "cell", env.Outcome)
	require.NotNil(t, env.Cell)
	assert.Equal(t, "markdown", env.Cell.Type)

	auths, paths, bodies := srv.snapshot()
	require.Len(t, bodies, 1, "write_cell is terminal: no second request")
	assert.Equal(t, "Bearer "+openAITestKey, auths[0])
	assert.Equal(t, "/responses", paths[0])
}

func TestOpenAI_RequiresKey(t *testing.T) {
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	_, err := r.languageModel(context.Background(), Config{Provider: ProviderOpenAI})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OPENAI_API_KEY")

	_, err = r.languageModel(context.Background(), Config{Provider: ProviderOpenAI, APIKey: "   "})
	require.Error(t, err)

	m, err := r.languageModel(context.Background(), Config{Provider: ProviderOpenAI, APIKey: openAITestKey})
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestOpenAI_ErrorsNeverContainTheKey(t *testing.T) {
	// A server that echoes the bearer token back in a 401, as some gateways do.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"Incorrect API key provided: %s","type":"invalid_request_error"}}`,
			strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}))
	defer srv.Close()
	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)
	cfg := Config{Provider: ProviderOpenAI, APIKey: openAITestKey, Sandbox: SandboxReadOnly}

	_, err := r.Chat(context.Background(), cfg, "hi", "")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), openAITestKey)
	assert.Contains(t, err.Error(), "Incorrect API key")

	var streamErrs []string
	_, err = r.ChatStream(context.Background(), cfg, "hi", "", nil, func(ev StreamEvent) {
		if ev.Kind == "error" {
			streamErrs = append(streamErrs, ev.Text)
		}
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), openAITestKey)
	require.NotEmpty(t, streamErrs, "the failure reaches the UI as an error event")
	for _, text := range streamErrs {
		assert.NotContains(t, text, openAITestKey, "agent:stream error event")
	}

	var taskErrs []string
	_, err = r.Task(context.Background(), cfg, TaskRequest{Mode: "create", Description: "x"}, func(ev TaskEvent) {
		if ev.Kind == "error" {
			taskErrs = append(taskErrs, ev.Text)
		}
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), openAITestKey)
	for _, text := range taskErrs {
		assert.NotContains(t, text, openAITestKey, "agent:task error event")
	}

	// Construction errors never mention a key either.
	_, err = r.languageModel(context.Background(), Config{Provider: ProviderOpenAI})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), openAITestKey)
}

func TestScrubKey(t *testing.T) {
	base := errors.New("boom")
	assert.Same(t, base, scrubKey(base, ""))
	assert.Same(t, base, scrubKey(base, "absent"))
	assert.Nil(t, scrubKey(nil, "k"))
	inner := errors.New("bad key sk-1 here")
	got := scrubKey(fmt.Errorf("wrap: %w", inner), "sk-1")
	assert.NotContains(t, got.Error(), "sk-1")
	assert.ErrorIs(t, got, inner)
}

func TestReasoningEffortAgentOptions_Providers(t *testing.T) {
	assert.Len(t, reasoningEffortAgentOptions(Config{Provider: ProviderOpenAI}), 1)
	assert.Len(t, reasoningEffortAgentOptions(Config{Provider: ProviderLocal}), 1)
	assert.Len(t, reasoningEffortAgentOptions(Config{}), 1)
	assert.Empty(t, reasoningEffortAgentOptions(Config{Provider: ProviderAnthropic}))
	assert.Empty(t, reasoningEffortAgentOptions(Config{Provider: ProviderGemini}))
}

func TestOpenAI_ReasoningRejectionRetriesOnce(t *testing.T) {
	var mu sync.Mutex
	var seenReasoning []bool
	inner := newFakeResponsesServer(t, "list_skills", "{}", "fine")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		_, has := body["reasoning"]
		mu.Lock()
		seenReasoning = append(seenReasoning, has)
		first := len(seenReasoning) == 1
		mu.Unlock()
		if first && has {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'reasoning.effort' is not supported with this model.","type":"invalid_request_error"}}`))
			return
		}
		req, _ := http.NewRequest(r.Method, inner.URL+r.URL.Path, strings.NewReader(string(raw)))
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		_, _ = io.Copy(w, resp.Body)
	}))
	defer srv.Close()

	r := openAIRunner(t, srv.URL, agents.ModeReadOnly)
	cfg := Config{Provider: ProviderOpenAI, APIKey: openAITestKey, Sandbox: SandboxReadOnly}
	res, err := r.Chat(context.Background(), cfg, "hi", "")
	require.NoError(t, err)
	assert.Contains(t, res.Text, "fine")
	assert.True(t, r.reasoningIsRejected(cfg))
	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(seenReasoning), 2)
	assert.True(t, seenReasoning[0])
	assert.False(t, seenReasoning[1], "the retry drops the reasoning parameter")
}

func TestDefaultModelFor_OpenAIAndRefreshed(t *testing.T) {
	assert.Equal(t, "claude-sonnet-5-5", config.DefaultModelFor(ProviderAnthropic))
	assert.Equal(t, "gpt-5.5", config.DefaultModelFor(ProviderOpenAI))
	assert.Equal(t, "gemini-3.5-flash", config.DefaultModelFor(ProviderGemini))
	assert.Equal(t, "local", config.DefaultModelFor(ProviderLocal))
}
