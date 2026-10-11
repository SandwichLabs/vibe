package llm

import (
	"context"
	"encoding/json"
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
)

const (
	cfTestToken   = "cf-token-NEEDLE-0123456789"
	cfTestAccount = "0123456789abcdef0123456789abcdef"
)

type recorded struct {
	path, auth, gateway string
	model               string
}

// recordingLLM wraps fakeLLMServer, recording what each request carried.
func recordingLLM(t *testing.T) (*httptest.Server, func() []recorded) {
	t.Helper()
	inner := fakeLLMServer(t, "list_skills", "{}", "You have a Sales dashboard skill.")
	t.Cleanup(inner.Close)
	var mu sync.Mutex
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &b)
		mu.Lock()
		got = append(got, recorded{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("cf-aig-gateway-id"), b.Model})
		mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recorded { mu.Lock(); defer mu.Unlock(); return append([]recorded(nil), got...) }
}

func TestCloudflare_URLHeaderModelAndToken(t *testing.T) {
	srv, seen := recordingLLM(t)
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	r.cloudflareBaseURL = srv.URL + "/client/v4/accounts/" + cfTestAccount + "/ai/v1"

	res, err := r.Chat(context.Background(), Config{
		Provider: ProviderCloudflare, APIKey: cfTestToken, AccountID: cfTestAccount, Gateway: "my-gw", Sandbox: SandboxReadOnly,
	}, "What skills do I have?", "")
	require.NoError(t, err)
	assert.Contains(t, res.Text, "Sales dashboard")
	got := seen()
	require.Len(t, got, 2)
	for _, g := range got {
		assert.Equal(t, "/client/v4/accounts/"+cfTestAccount+"/ai/v1/chat/completions", g.path)
		assert.Equal(t, "Bearer "+cfTestToken, g.auth)
		assert.Equal(t, "my-gw", g.gateway)
		assert.Equal(t, "openai/gpt-5.5", g.model, "default model in provider/model form")
	}
}

func TestCloudflare_NoGatewayHeaderWhenUnset(t *testing.T) {
	srv, seen := recordingLLM(t)
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	r.cloudflareBaseURL = srv.URL + "/ai/v1"
	_, err := r.Chat(context.Background(), Config{
		Provider: ProviderCloudflare, APIKey: cfTestToken, AccountID: cfTestAccount, Model: "@cf/moonshotai/kimi-k2.6", Sandbox: SandboxReadOnly,
	}, "hi", "")
	require.NoError(t, err)
	for _, g := range seen() {
		assert.Empty(t, g.gateway)
		assert.Equal(t, "@cf/moonshotai/kimi-k2.6", g.model)
	}
}

func TestCloudflare_RealBaseURLAndValidation(t *testing.T) {
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	ctx := context.Background()
	_, err := r.languageModel(ctx, Config{Provider: ProviderCloudflare})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CLOUDFLARE_API_TOKEN")
	_, err = r.languageModel(ctx, Config{Provider: ProviderCloudflare, APIKey: cfTestToken})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account ID")
	_, err = r.languageModel(ctx, Config{Provider: ProviderCloudflare, APIKey: cfTestToken, AccountID: "../../evil"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), cfTestToken)
	_, err = r.languageModel(ctx, Config{Provider: ProviderCloudflare, APIKey: cfTestToken, AccountID: cfTestAccount, Gateway: "a b\r\nX: y"})
	require.Error(t, err)
	m, err := r.languageModel(ctx, Config{Provider: ProviderCloudflare, APIKey: cfTestToken, AccountID: cfTestAccount})
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestOllama_EndpointKeyAndModel(t *testing.T) {
	srv, seen := recordingLLM(t)
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	_, err := r.Chat(context.Background(), Config{Provider: ProviderOllama, Endpoint: srv.URL + "/v1", Model: "qwen3:0.6b", Sandbox: SandboxReadOnly}, "hi", "")
	require.NoError(t, err)
	for _, g := range seen() {
		assert.Equal(t, "/v1/chat/completions", g.path)
		assert.Equal(t, "qwen3:0.6b", g.model)
	}
}

func TestOllama_NoModelIsAClearError(t *testing.T) {
	r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
	_, err := r.languageModel(context.Background(), Config{Provider: ProviderOllama})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pick a model in Settings")
}

// Every new provider's key stays out of returned, streamed and task errors.
func TestNewProviders_ErrorsNeverContainTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"bad credential %s","type":"auth"}}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}))
	defer srv.Close()
	cases := map[string]Config{
		"cloudflare": {Provider: ProviderCloudflare, APIKey: cfTestToken, AccountID: cfTestAccount},
		"ollama":     {Provider: ProviderOllama, APIKey: cfTestToken, Endpoint: srv.URL + "/v1", Model: "m"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewRunner(newTestHandler(t, agents.ModeReadOnly), nil)
			r.cloudflareBaseURL = srv.URL + "/ai/v1"
			cfg.Sandbox = SandboxReadOnly
			_, err := r.Chat(context.Background(), cfg, "hi", "")
			require.Error(t, err)
			assert.NotContains(t, err.Error(), cfTestToken)
			assert.Contains(t, err.Error(), "bad credential")

			var streamErrs, taskErrs []string
			_, err = r.ChatStream(context.Background(), cfg, "hi", "", nil, func(ev StreamEvent) {
				if ev.Kind == "error" {
					streamErrs = append(streamErrs, ev.Text)
				}
			})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), cfTestToken)
			require.NotEmpty(t, streamErrs)
			for _, s := range streamErrs {
				assert.NotContains(t, s, cfTestToken)
			}
			_, err = r.Task(context.Background(), cfg, TaskRequest{Mode: "create", Description: "x"}, func(ev TaskEvent) {
				if ev.Kind == "error" {
					taskErrs = append(taskErrs, ev.Text)
				}
			})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), cfTestToken)
			for _, s := range taskErrs {
				assert.NotContains(t, s, cfTestToken)
			}
		})
	}
}
