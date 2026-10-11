package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingHandler wraps another Handler and counts calls per method, so a
// test can assert a tool ran exactly once even though it's dispatched
// through the real agents.Handler underneath (never a hand-rolled double
// that might not exercise real dispatch semantics).
type countingHandler struct {
	inner Handler
	mu    sync.Mutex
	calls map[string]int
}

func newCountingHandler(inner Handler) *countingHandler {
	return &countingHandler{inner: inner, calls: map[string]int{}}
}

func (c *countingHandler) Handle(ctx context.Context, req *agents.Request) *agents.Response {
	c.mu.Lock()
	c.calls[req.Method]++
	c.mu.Unlock()
	return c.inner.Handle(ctx, req)
}

func (c *countingHandler) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[method]
}

// jsonAPIError writes an OpenAI-shaped {"error":{...}} body with the given
// status code — openai-go's apierror.Error.Error() embeds the raw response
// body verbatim, so a message here reaches mightBeParamRejection unchanged.
// Used for both Generate (Chat) and Stream (ChatStream) calls: the SDK
// checks the HTTP status code before ever attempting to decode a
// streaming body, so a >=400 response never needs to be SSE-shaped.
func jsonAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": message, "type": "invalid_request_error"},
	})
}

// --- SSE (Server-Sent Events) response helpers for ChatStream tests. Chat
// (Generate) talks to the plain, non-streaming chat/completions shape (a
// single JSON object) — the fakeLLMServer helper above and jsonAPIError
// cover that. ChatStream uses fantasy's Stream, which sends `stream: true`
// and requires the response body be "data: {...}\n\n"-framed chunks
// (openai-go's ssestream decoder), ending with "data: [DONE]\n\n" — a plain
// JSON body here produces a client-side "unexpected EOF" parse error, not a
// clean success. ---

func sseWriteChunk(t *testing.T, w http.ResponseWriter, chunk map[string]any) {
	t.Helper()
	b, err := json.Marshal(chunk)
	require.NoError(t, err)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseWriteDone(w http.ResponseWriter) {
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// sseToolCallResponse streams a single tool-call step: a delta carrying the
// tool call, then a finish_reason=tool_calls chunk, then [DONE].
func sseToolCallResponse(t *testing.T, w http.ResponseWriter, toolName, toolArgs string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	sseWriteChunk(t, w, map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1, "model": "local",
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"index": 0, "id": "call_1", "type": "function",
					"function": map[string]any{"name": toolName, "arguments": toolArgs},
				}},
			},
		}},
	})
	sseWriteChunk(t, w, map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1, "model": "local",
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
	})
	sseWriteDone(w)
}

// sseFinalTextResponse streams a plain text final answer.
func sseFinalTextResponse(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	sseWriteChunk(t, w, map[string]any{
		"id": "chatcmpl-2", "object": "chat.completion.chunk", "created": 2, "model": "local",
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}}},
	})
	sseWriteChunk(t, w, map[string]any{
		"id": "chatcmpl-2", "object": "chat.completion.chunk", "created": 2, "model": "local",
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	})
	sseWriteDone(w)
}

// --- Scenario 1: the very first request is rejected for reasoning_effort,
// before any tool ever ran. Must retry once, without the option, and
// succeed. ---

func fakeLLMServer_RejectFirstRequestThenSucceed(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var requests int32
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		_, _ = io.ReadAll(r.Body)
		if n == 1 {
			jsonAPIError(w, http.StatusBadRequest, "Unsupported parameter: 'reasoning_effort' is not supported with this model.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "x", "object": "chat.completion", "created": 1, "model": "local",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "all good"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	return srv, new(int32)
}

func TestChat_ReasoningRejection_RetriesOnceWithoutOption(t *testing.T) {
	srv, _ := fakeLLMServer_RejectFirstRequestThenSucceed(t)
	defer srv.Close()

	handler := newCountingHandler(newTestHandler(t, agents.ModeReadOnly))
	runner := NewRunner(handler, nil)

	res, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "hello", "")
	require.NoError(t, err)
	assert.Equal(t, "all good", res.Text)
	assert.True(t, runner.reasoningIsRejected(Config{Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly}),
		"the rejection must be remembered on the Runner for later calls")
}

// fakeSSEServer_RejectFirstRequestThenSucceed is
// fakeLLMServer_RejectFirstRequestThenSucceed's ChatStream (SSE) equivalent.
func fakeSSEServer_RejectFirstRequestThenSucceed(t *testing.T) *httptest.Server {
	t.Helper()
	var requests int32
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		_, _ = io.ReadAll(r.Body)
		if n == 1 {
			jsonAPIError(w, http.StatusBadRequest, "Unsupported parameter: 'reasoning_effort' is not supported with this model.")
			return
		}
		sseFinalTextResponse(t, w, "all good")
	}))
}

func TestChatStream_ReasoningRejection_RetriesOnceWithoutOption(t *testing.T) {
	srv := fakeSSEServer_RejectFirstRequestThenSucceed(t)
	defer srv.Close()

	handler := newCountingHandler(newTestHandler(t, agents.ModeReadOnly))
	runner := NewRunner(handler, nil)

	var events []StreamEvent
	res, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "hello", "", nil, func(ev StreamEvent) { events = append(events, ev) })
	require.NoError(t, err)
	assert.Equal(t, "all good", res.Text)
	for _, ev := range events {
		assert.NotEqual(t, "error", ev.Kind, "a retried-and-recovered turn must not leave a stray error event: %+v", ev)
	}
}

// --- Scenario 2: a tool runs successfully, THEN the follow-up request (now
// carrying the tool result) is rejected for reasoning_effort. Must NOT
// retry — the tool already ran, and retrying would resend the whole turn
// and re-run it a second time — so Chat returns an error and the tool ran
// exactly once. ---

func fakeLLMServer_ToolThenRejectFollowup(t *testing.T, toolName, toolArgs string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}
		if !sawTool {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "local",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role": "assistant", "content": "",
						"tool_calls": []map[string]any{{
							"id": "call_1", "type": "function",
							"function": map[string]any{"name": toolName, "arguments": toolArgs},
						}},
					},
					"finish_reason": "tool_calls",
				}},
				"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
			return
		}
		// Second request, now carrying the tool result: reject for
		// reasoning_effort. A naive retry-on-any-rejection would resend this
		// whole turn (including re-calling the tool) from scratch.
		jsonAPIError(w, http.StatusBadRequest, "Unsupported parameter: 'reasoning_effort' is not supported with this model.")
	}))
}

func TestChat_ReasoningRejectionAfterToolCall_DoesNotRetry_ToolRunsOnce(t *testing.T) {
	args := `{"sql":"SELECT 1","label":"Sales overview"}`
	srv := fakeLLMServer_ToolThenRejectFollowup(t, "add_query_cell", args)
	defer srv.Close()

	handler := newCountingHandler(newTestHandler(t, agents.ModeFull))
	runner := NewRunner(handler, nil)

	_, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxFull,
	}, "Add a sales overview cell.", "")
	require.Error(t, err, "a rejection AFTER a tool already ran must surface as a real error, not be silently retried")
	assert.Equal(t, 1, handler.count("add_query_cell"), "add_query_cell must have run exactly once — a retry here would duplicate the write")
}

// fakeSSEServer_ToolThenRejectFollowup is
// fakeLLMServer_ToolThenRejectFollowup's ChatStream (SSE) equivalent: the
// first (tool-calling) response must be SSE-framed since ChatStream always
// streams, but the rejecting second response can stay plain JSON — a
// >=400 status short-circuits before any SSE decoding is attempted.
func fakeSSEServer_ToolThenRejectFollowup(t *testing.T, toolName, toolArgs string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}
		if !sawTool {
			sseToolCallResponse(t, w, toolName, toolArgs)
			return
		}
		jsonAPIError(w, http.StatusBadRequest, "Unsupported parameter: 'reasoning_effort' is not supported with this model.")
	}))
}

func TestChatStream_ReasoningRejectionAfterToolCall_DoesNotRetry_ToolRunsOnce(t *testing.T) {
	args := `{"sql":"SELECT 1","label":"Sales overview"}`
	srv := fakeSSEServer_ToolThenRejectFollowup(t, "add_query_cell", args)
	defer srv.Close()

	handler := newCountingHandler(newTestHandler(t, agents.ModeFull))
	runner := NewRunner(handler, nil)

	_, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxFull,
	}, "Add a sales overview cell.", "", nil, func(StreamEvent) {})
	require.Error(t, err)
	assert.Equal(t, 1, handler.count("add_query_cell"), "add_query_cell must have run exactly once — a retry here would duplicate the write")
}

// TestMightBeParamRejection_UnrelatedErrorAfterToolCall_NeverRetries is a
// narrower regression check on the SAME scenario using a plain unrelated
// error (no "reasoning" text at all) after a tool call, confirming
// mightBeParamRejection itself gates this — not just the step-count check.
func TestChat_UnrelatedErrorAfterToolCall_NeverRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}
		if !sawTool {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "local",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role": "assistant", "content": "",
						"tool_calls": []map[string]any{{"id": "call_1", "type": "function", "function": map[string]any{"name": "list_queries", "arguments": "{}"}}},
					},
					"finish_reason": "tool_calls",
				}},
				"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
			return
		}
		jsonAPIError(w, http.StatusInternalServerError, "internal server error")
	}))
	defer srv.Close()

	handler := newCountingHandler(newTestHandler(t, agents.ModeReadOnly))
	runner := NewRunner(handler, nil)
	_, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "list my queries", "")
	require.Error(t, err)
	assert.Equal(t, 1, handler.count("list_queries"))
}
