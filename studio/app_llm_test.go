package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/config"
)

func TestLLMSettings_NoWorkspaceNeeded_SaveReloads(t *testing.T) {
	a, _, path := secretsApp(t, "# keep me\nllm:\n  provider: local\n  api_key: \"sk-legacy-NEEDLE\"\n")
	for _, k := range []string{"VIBE_LLM_PROVIDER", "VIBE_LLM_MODEL", "VIBE_LLM_ENDPOINT", "VIBE_LLM_ACCOUNT_ID", "VIBE_LLM_GATEWAY", "CLOUDFLARE_ACCOUNT_ID"} {
		t.Setenv(k, "")
	}
	v := a.GetLLMSettings()
	assert.Equal(t, "local", v.Provider)
	assert.Equal(t, "local", v.EffectiveModel)
	assert.Len(t, v.Providers, 6)
	assert.Equal(t, "ollama", v.Providers[1].ID)

	acct := "0123456789abcdef0123456789abcdef"
	require.NoError(t, a.SetLLMSettings(config.LLMSettings{Provider: "cloudflare", AccountID: acct, Gateway: "gw"}))
	v = a.GetLLMSettings()
	assert.Equal(t, "cloudflare", v.Provider)
	assert.Equal(t, "openai/gpt-5.5", v.EffectiveModel)
	assert.Equal(t, acct, v.AccountID)
	assert.Equal(t, "cloudflare", a.llmSnapshot().Provider)
	b, _ := os.ReadFile(path)
	assert.Contains(t, string(b), "# keep me")
	assert.Contains(t, string(b), "sk-legacy-NEEDLE", "api_key node untouched")

	require.NoError(t, a.SetLLMSettings(config.LLMSettings{Provider: "ollama", Endpoint: "http://localhost:11434/v1"}))
	v = a.GetLLMSettings()
	assert.Equal(t, "", v.EffectiveModel, "ollama has no default model")

	assert.Error(t, a.SetLLMSettings(config.LLMSettings{Provider: "bogus"}))
}

func TestLLMSettings_EnvOverrideReportedAndKept(t *testing.T) {
	a, _, path := secretsApp(t, "llm:\n  provider: local\n  model: filemodel\n")
	t.Setenv("VIBE_LLM_MODEL", "envmodel")
	a.llmCfg = config.Load().LLM
	v := a.GetLLMSettings()
	assert.Equal(t, "envmodel", v.EffectiveModel)
	assert.Equal(t, "VIBE_LLM_MODEL", v.EnvOverrides["model"])
	require.NoError(t, a.SetLLMSettings(config.LLMSettings{Provider: "openai", Model: "envmodel"}))
	assert.Equal(t, "filemodel", config.ReadLLMSettings(path).Model)
}

func TestGetLLMKeyStatus_ForProviderBeingPicked(t *testing.T) {
	a, m, _ := secretsApp(t, "llm:\n  provider: local\n  api_key: \"sk-legacy-NEEDLE\"\n")
	st := a.GetLLMKeyStatus("cloudflare")
	assert.Equal(t, "cloudflare", st.Provider)
	assert.Equal(t, "file", st.Source, "the legacy file key would be sent to the picked provider")
	assert.NotContains(t, st.Detail, "NEEDLE")

	require.NoError(t, config.SetLLMKey(t.Context(), m, "cloudflare", "cf-token"))
	assert.Equal(t, "keychain", a.GetLLMKeyStatus("cloudflare").Source)
	assert.Equal(t, "file", a.GetLLMKeyStatus("").Source, "the configured provider is unchanged")
	assert.Equal(t, "file", a.GetLLMKeyStatus("nonsense").Source, "unknown falls back to configured")
}

func TestListLLMModels_RefusesUnknownProvider(t *testing.T) {
	a, _, _ := secretsApp(t, "")
	assert.NotEmpty(t, a.ListLLMModels("nope", "").Error)
}
