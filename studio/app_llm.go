package main

import (
	"fmt"

	"github.com/SandwichLabs/vibe/internal/config"
	"github.com/SandwichLabs/vibe/internal/llm"
)

// LLMSettingsView is Settings → App → "The agent's model" as the form needs
// it. It works without a workspace open: the provider is app-global. Nothing
// here is secret (the key is write-only and never appears).
type LLMSettingsView struct {
	Provider string `json:"provider"`
	// Model is what is configured ("" = the provider's default);
	// EffectiveModel is what will be requested ("" only when the provider has
	// no default, i.e. Ollama before a model is picked).
	Model          string `json:"model"`
	EffectiveModel string `json:"effectiveModel"`
	DefaultModel   string `json:"defaultModel"`
	Endpoint       string `json:"endpoint"`
	AccountID      string `json:"accountId"`
	Gateway        string `json:"gateway"`
	// AccountIDFromEnv is true when AccountID comes from CLOUDFLARE_ACCOUNT_ID.
	AccountIDFromEnv bool `json:"accountIdFromEnv"`
	// EnvOverrides maps a field (provider, model, endpoint, accountId,
	// gateway) to the environment variable that sets it; the UI disables it.
	EnvOverrides map[string]string     `json:"envOverrides"`
	Providers    []config.ProviderInfo `json:"providers"`
}

// GetLLMSettings returns the effective model settings and the provider
// catalogue with suggested models. No workspace needed.
func (a *App) GetLLMSettings() LLMSettingsView {
	a.mu.RLock()
	c := a.llmCfg
	a.mu.RUnlock()
	return LLMSettingsView{
		Provider:         c.Provider,
		Model:            c.Model,
		EffectiveModel:   c.EffectiveModel(),
		DefaultModel:     config.DefaultModelFor(c.Provider),
		Endpoint:         c.Endpoint,
		AccountID:        c.AccountID,
		Gateway:          c.Gateway,
		AccountIDFromEnv: c.AccountIDFromEnv,
		EnvOverrides:     config.LLMEnvOverrides(),
		Providers:        config.LLMProviders(),
	}
}

// SetLLMSettings validates and saves the settings to the config file (never
// the API key), reloads them and tells the UI ("llm:config"). A field set by
// an environment variable keeps the file's own value. No workspace needed.
func (a *App) SetLLMSettings(s config.LLMSettings) error {
	if err := config.SaveLLMSettingsKeepingEnv(config.LLMConfigPath(), s); err != nil {
		return err
	}
	cfg := config.Load().LLM
	a.mu.Lock()
	a.llmCfg = cfg
	a.mu.Unlock()
	a.setLLMSnapshot(cfg)
	a.keyCache.invalidate()
	a.emitEvent("llm:config", map[string]any{"provider": cfg.Provider, "endpoint": cfg.Endpoint, "model": cfg.EffectiveModel()})
	return nil
}

// ListLLMModels lists the models a local server or Ollama has installed. Other
// providers return an empty list (the form uses the static suggestions).
func (a *App) ListLLMModels(provider, endpoint string) llm.ModelList {
	if _, err := a.llmProviderFor(provider); err != nil {
		return llm.ModelList{Error: fmt.Sprint(err)}
	}
	return llm.ListModels(a.ctxOrBackground(), provider, endpoint)
}
