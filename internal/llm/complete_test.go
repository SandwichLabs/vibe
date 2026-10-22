package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func completionServer(t *testing.T, bodies *[]map[string]any, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		*bodies = append(*bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c1", "object": "chat.completion", "created": 1, "model": "local",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "an answer"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestComplete_SendsNoToolsAndCapsTheOutput(t *testing.T) {
	var bodies []map[string]any
	var mu sync.Mutex
	srv := completionServer(t, &bodies, &mu)
	cfg := Config{Provider: ProviderLocal, Endpoint: srv.URL, Sandbox: "full"}
	text, err := NewRunner(nil, nil).Complete(context.Background(), cfg, "be brief", "hello", 999999)
	require.NoError(t, err)
	assert.Equal(t, "an answer", text)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	b := bodies[0]
	assert.Empty(t, b["tools"], "a completion carries no tools")
	assert.Nil(t, b["tool_choice"])
	for _, k := range []string{"max_tokens", "max_completion_tokens"} {
		if v, ok := b[k]; ok {
			assert.EqualValues(t, MaxCompletionTokens, v, "the output is capped at 4,000 tokens whatever was asked")
		}
	}
	msgs, _ := b["messages"].([]any)
	require.Len(t, msgs, 2)
	first, _ := msgs[0].(map[string]any)
	second, _ := msgs[1].(map[string]any)
	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "user", second["role"])
}

func TestComplete_NoProviderIsAnErrorNotARequest(t *testing.T) {
	for _, cfg := range []Config{
		{Provider: ProviderOpenAI},
		{Provider: ProviderAnthropic, APIKey: "  "},
		{Provider: ProviderLocal},
		{Provider: ProviderCloudflare, APIKey: "k"},
	} {
		assert.False(t, Configured(cfg), cfg.Provider)
		_, err := NewRunner(nil, nil).Complete(context.Background(), cfg, "", "hi", 10)
		assert.ErrorIs(t, err, ErrNoProvider)
	}
	assert.True(t, Configured(Config{Provider: ProviderOpenAI, APIKey: "k"}))
	assert.True(t, Configured(Config{Provider: ProviderLocal, Endpoint: "http://x"}))
}

func TestComplete_ErrorsCarryNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad key sk-NEEDLE-123"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	cfg := Config{Provider: ProviderLocal, Endpoint: srv.URL, APIKey: "sk-NEEDLE-123"}
	_, err := NewRunner(nil, nil).Complete(context.Background(), cfg, "", "hi", 10)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-NEEDLE-123")
}
