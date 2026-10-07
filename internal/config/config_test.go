package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateHome points HOME at an empty temp dir so Load() doesn't pick up a real
// ~/.vibe.yaml on the developer's machine.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestLoad_Defaults(t *testing.T) {
	isolateHome(t)
	cfg := Load()
	assert.Equal(t, LLMProviderLocal, cfg.LLM.Provider, "default provider is local")
	assert.Equal(t, DefaultLLMEndpoint, cfg.LLM.Endpoint)
	assert.Empty(t, cfg.LLM.Model, "model default is per-provider, resolved later")
}

func TestLoad_EnvOverridesProviderAndModel(t *testing.T) {
	isolateHome(t)
	t.Setenv("VIBE_LLM_PROVIDER", "gemini")
	t.Setenv("VIBE_LLM_MODEL", "gemini-2.5-pro")
	cfg := Load()
	assert.Equal(t, LLMProviderGemini, cfg.LLM.Provider)
	assert.Equal(t, "gemini-2.5-pro", cfg.LLM.Model)
}

func TestLoad_APIKeyFallsBackToStandardEnv(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		isolateHome(t)
		t.Setenv("VIBE_LLM_PROVIDER", "anthropic")
		t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
		cfg := Load()
		assert.Equal(t, "sk-ant-test", cfg.LLM.APIKey)
	})

	t.Run("openai", func(t *testing.T) {
		isolateHome(t)
		t.Setenv("VIBE_LLM_PROVIDER", "openai")
		t.Setenv("VIBE_LLM_API_KEY", "")
		t.Setenv("OPENAI_API_KEY", "sk-oai-test")
		cfg := Load()
		assert.Equal(t, LLMProviderOpenAI, cfg.LLM.Provider)
		assert.Equal(t, "sk-oai-test", cfg.LLM.APIKey)
	})

	t.Run("gemini", func(t *testing.T) {
		isolateHome(t)
		t.Setenv("VIBE_LLM_PROVIDER", "gemini")
		t.Setenv("GEMINI_API_KEY", "g-test")
		cfg := Load()
		assert.Equal(t, "g-test", cfg.LLM.APIKey)
	})

	t.Run("explicit llm.api_key wins over standard env", func(t *testing.T) {
		isolateHome(t)
		t.Setenv("VIBE_LLM_PROVIDER", "anthropic")
		t.Setenv("VIBE_LLM_API_KEY", "explicit")
		t.Setenv("ANTHROPIC_API_KEY", "from-env")
		cfg := Load()
		assert.Equal(t, "explicit", cfg.LLM.APIKey)
	})

	t.Run("local provider needs no key", func(t *testing.T) {
		isolateHome(t)
		cfg := Load()
		require.Equal(t, LLMProviderLocal, cfg.LLM.Provider)
		assert.Empty(t, cfg.LLM.APIKey)
	})
}
