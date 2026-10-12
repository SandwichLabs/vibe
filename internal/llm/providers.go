package llm

import (
	"fmt"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"

	"github.com/SandwichLabs/vibe/internal/config"
)

// newProvider builds the fantasy provider for the named LLM provider.
func (r *Runner) newProvider(provider string, cfg Config) (fantasy.Provider, error) {
	var (
		p   fantasy.Provider
		err error
	)
	switch provider {
	case ProviderAnthropic:
		p, err = newAnthropic(cfg)
	case ProviderOpenAI:
		p, err = r.newOpenAI(cfg)
	case ProviderGemini:
		p, err = newGemini(cfg)
	case ProviderLocal:
		p, err = newLocal(cfg)
	case ProviderOllama:
		p, err = newOllama(cfg)
	case ProviderCloudflare:
		p, err = r.newCloudflare(cfg)
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (want %s)", provider, strings.Join(config.LLMProviderIDs(), ", "))
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func newAnthropic(cfg Config) (fantasy.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("anthropic provider requires an API key (set ANTHROPIC_API_KEY, or save a key in Settings > App)")
	}
	p, err := anthropic.New(anthropic.WithAPIKey(cfg.APIKey))
	return p, providerErr(ProviderAnthropic, err)
}

func (r *Runner) newOpenAI(cfg Config) (fantasy.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("openai provider requires an API key (set OPENAI_API_KEY, or save a key in Settings > App)")
	}
	opts := []openai.Option{
		openai.WithAPIKey(cfg.APIKey),
		// The Responses API for every model: it is OpenAI's current
		// tool-calling surface, and an id fantasy's built-in list does
		// not know yet (a new release, a fine-tune) must not silently
		// fall back to Chat Completions.
		openai.WithUseResponsesAPI(),
		openai.WithResponsesAPIFunc(func(string) bool { return true }),
	}
	if r.openAIBaseURL != "" {
		opts = append(opts, openai.WithBaseURL(r.openAIBaseURL))
	}
	p, err := openai.New(opts...)
	return p, providerErr(ProviderOpenAI, err)
}

func newGemini(cfg Config) (fantasy.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("gemini provider requires an API key (set GEMINI_API_KEY, or save a key in Settings > App)")
	}
	p, err := google.New(google.WithGeminiAPIKey(cfg.APIKey))
	return p, providerErr(ProviderGemini, err)
}

func newLocal(cfg Config) (fantasy.Provider, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("local provider requires an endpoint (llm.endpoint)")
	}
	apiKey := cfg.APIKey
	if strings.TrimSpace(apiKey) == "" {
		// Local OpenAI-compatible servers usually accept any non-empty key;
		// the underlying SDK rejects an empty one.
		apiKey = "sk-local"
	}
	p, err := openaicompat.New(
		openaicompat.WithBaseURL(endpoint),
		openaicompat.WithAPIKey(apiKey),
	)
	return p, providerErr(ProviderLocal, err)
}

func newOllama(cfg Config) (fantasy.Provider, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" || endpoint == config.DefaultLLMEndpoint {
		endpoint = config.DefaultOllamaEndpoint
	}
	apiKey := cfg.APIKey
	if strings.TrimSpace(apiKey) == "" {
		apiKey = "ollama" // a local Ollama needs none; the SDK rejects an empty one
	}
	p, err := openaicompat.New(
		openaicompat.WithName(ProviderOllama),
		openaicompat.WithBaseURL(endpoint),
		openaicompat.WithAPIKey(apiKey),
	)
	return p, providerErr(ProviderOllama, err)
}

func (r *Runner) newCloudflare(cfg Config) (fantasy.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("cloudflare provider requires a Cloudflare API token (set CLOUDFLARE_API_TOKEN, or save one in Settings → App)")
	}
	base := r.cloudflareBaseURL
	if base == "" {
		if strings.TrimSpace(cfg.AccountID) == "" {
			return nil, fmt.Errorf("cloudflare provider requires an account ID (set it in Settings → App, or CLOUDFLARE_ACCOUNT_ID)")
		}
		var berr error
		if base, berr = config.CloudflareBaseURL(strings.TrimSpace(cfg.AccountID)); berr != nil {
			return nil, fmt.Errorf("invalid Cloudflare account ID: %w", berr)
		}
	}
	opts := []openaicompat.Option{
		openaicompat.WithName(ProviderCloudflare),
		openaicompat.WithBaseURL(base),
		openaicompat.WithAPIKey(cfg.APIKey),
	}
	if gw := strings.TrimSpace(cfg.Gateway); gw != "" {
		if gerr := config.ValidateGatewayID(gw); gerr != nil {
			return nil, fmt.Errorf("invalid Cloudflare gateway ID: %w", gerr)
		}
		opts = append(opts, openaicompat.WithHeaders(map[string]string{"cf-aig-gateway-id": gw}))
	}
	p, err := openaicompat.New(opts...)
	return p, providerErr(ProviderCloudflare, err)
}

// providerErr adds the provider's name to a constructor error.
func providerErr(provider string, err error) error {
	if err != nil {
		return fmt.Errorf("failed to create %s provider: %w", provider, err)
	}
	return nil
}
