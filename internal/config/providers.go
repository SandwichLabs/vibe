package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// Provider identifiers beyond the originals in config.go.
const (
	LLMProviderOllama     = "ollama"     // Ollama (OpenAI-compatible, local or Ollama Cloud)
	LLMProviderCloudflare = "cloudflare" // Cloudflare AI Gateway REST API
)

// DefaultOllamaEndpoint is Ollama's OpenAI-compatible base URL on this machine.
const DefaultOllamaEndpoint = "http://localhost:11434/v1"

// ProviderInfo describes one model provider. It is the single source of truth
// for the provider list: the Settings picker, `vibe config llm`, the key
// bindings and the key resolution order all read it.
type ProviderInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// NeedsKey is true when the provider cannot be used without a key;
	// KeyOptional is true when a key is accepted but not required.
	NeedsKey    bool `json:"needsKey"`
	KeyOptional bool `json:"keyOptional"`
	// KeyLabel names the credential ("API key", "Cloudflare API token").
	KeyLabel string `json:"keyLabel"`
	// KeyEnvVars are the provider's standard variables, in order.
	KeyEnvVars []string `json:"keyEnvVars"`
	// HasEndpoint is true when llm.endpoint applies.
	HasEndpoint     bool   `json:"hasEndpoint"`
	DefaultEndpoint string `json:"defaultEndpoint,omitempty"`
	// HasAccount is true for Cloudflare (llm.account_id, llm.gateway).
	HasAccount bool `json:"hasAccount"`
	// DefaultModel is used when llm.model is empty; "" means there is no
	// meaningful default and the person must pick one.
	DefaultModel string `json:"defaultModel"`
	// SuggestedModels is a short static list for the model field. Last checked
	// 2026-10-07; ids age as providers release models.
	SuggestedModels []string `json:"suggestedModels"`
}

var llmProviders = []ProviderInfo{
	{
		ID: LLMProviderLocal, Label: "Local server",
		Description: "An OpenAI-compatible server on your machine, such as llama-server or LM Studio.",
		KeyOptional: true, KeyLabel: "API key",
		HasEndpoint: true, DefaultEndpoint: DefaultLLMEndpoint, DefaultModel: "local",
	},
	{
		ID: LLMProviderOllama, Label: "Ollama",
		Description: "Models you have pulled into Ollama. Set the endpoint to https://ollama.com/v1 and a key for Ollama Cloud.",
		KeyOptional: true, KeyLabel: "API key", KeyEnvVars: []string{"OLLAMA_API_KEY"},
		HasEndpoint: true, DefaultEndpoint: DefaultOllamaEndpoint, DefaultModel: "",
	},
	{
		ID: LLMProviderOpenAI, Label: "OpenAI",
		Description: "GPT models with your OpenAI API key.",
		NeedsKey:    true, KeyLabel: "API key", KeyEnvVars: []string{"OPENAI_API_KEY"},
		DefaultModel: "gpt-5.5", SuggestedModels: []string{"gpt-5.5", "gpt-5.5-mini"},
	},
	{
		ID: LLMProviderAnthropic, Label: "Anthropic",
		Description: "Claude models with your Anthropic API key.",
		NeedsKey:    true, KeyLabel: "API key", KeyEnvVars: []string{"ANTHROPIC_API_KEY"},
		DefaultModel: "claude-sonnet-5-5", SuggestedModels: []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-4-5-20251001"},
	},
	{
		ID: LLMProviderGemini, Label: "Google Gemini",
		Description: "Gemini models with your Google AI API key.",
		NeedsKey:    true, KeyLabel: "API key", KeyEnvVars: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
		DefaultModel: "gemini-3.5-flash", SuggestedModels: []string{"gemini-3.5-flash"},
	},
	{
		ID: LLMProviderCloudflare, Label: "Cloudflare AI Gateway",
		Description: "Any model through your Cloudflare account, billed by Cloudflare. Uses a Cloudflare API token.",
		NeedsKey:    true, KeyLabel: "Cloudflare API token", KeyEnvVars: []string{"CLOUDFLARE_API_TOKEN"},
		HasAccount: true, DefaultModel: "openai/gpt-5.5",
		SuggestedModels: []string{"openai/gpt-5.5", "anthropic/claude-sonnet-5-5", "google/gemini-3.5-flash", "@cf/moonshotai/kimi-k2.6"},
	},
}

// LLMProviders returns the providers in picker order (a copy).
func LLMProviders() []ProviderInfo {
	out := make([]ProviderInfo, len(llmProviders))
	for i, p := range llmProviders {
		p.KeyEnvVars = append([]string(nil), p.KeyEnvVars...)
		p.SuggestedModels = append([]string(nil), p.SuggestedModels...)
		out[i] = p
	}
	return out
}

// LLMProvider looks one provider up by id.
func LLMProvider(id string) (ProviderInfo, bool) {
	for _, p := range LLMProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// LLMProviderIDs lists the provider ids in picker order.
func LLMProviderIDs() []string {
	ids := make([]string, len(llmProviders))
	for i, p := range llmProviders {
		ids[i] = p.ID
	}
	return ids
}

// DefaultModelFor is the model used when llm.model is empty. "" means the
// provider has no meaningful default (Ollama). Unknown providers get "local".
func DefaultModelFor(provider string) string {
	if p, ok := LLMProvider(provider); ok {
		return p.DefaultModel
	}
	return "local"
}

// EffectiveModel is the model that will actually be requested.
func (l LLMConfig) EffectiveModel() string {
	if m := strings.TrimSpace(l.Model); m != "" {
		return m
	}
	return DefaultModelFor(l.Provider)
}

// providerKeyEnv returns the first standard key variable that is set.
func providerKeyEnv(provider string) string {
	p, ok := LLMProvider(provider)
	if !ok {
		return ""
	}
	for _, name := range p.KeyEnvVars {
		if k := os.Getenv(name); k != "" {
			return k
		}
	}
	return ""
}

var (
	accountIDRe = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	gatewayIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// ValidateAccountID checks a Cloudflare account id (32 hex characters).
func ValidateAccountID(id string) error {
	if !accountIDRe.MatchString(id) {
		return errors.New("a Cloudflare account ID is 32 hexadecimal characters")
	}
	return nil
}

// ValidateGatewayID checks an AI Gateway id (a slug).
func ValidateGatewayID(id string) error {
	if !gatewayIDRe.MatchString(id) {
		return errors.New("a gateway ID is letters, digits, dashes and underscores (up to 64), starting with a letter or digit")
	}
	return nil
}

// CloudflareBaseURL builds the AI Gateway REST base URL for an account. The id
// is validated first, so no unchecked text ever reaches the URL.
func CloudflareBaseURL(accountID string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	return "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/ai/v1", nil
}

// ValidateEndpoint checks an http(s) URL with a host.
func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("the endpoint must be an http or https URL with a host")
	}
	if u.User != nil {
		return errors.New("the endpoint must not contain a user name or password")
	}
	return nil
}

// MaxModelLen bounds a model id.
const MaxModelLen = 200

// ValidateModel checks a model id: bounded, no control characters or spaces.
func ValidateModel(model string) error {
	if len(model) > MaxModelLen {
		return fmt.Errorf("the model id is longer than %d characters", MaxModelLen)
	}
	for _, r := range model {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("the model id has spaces, control or invisible formatting characters")
		}
	}
	return nil
}
