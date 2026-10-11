package cmd

import (
	"bytes"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"os"
	"strings"
	"testing"

	"github.com/SandwichLabs/vibe/internal/config"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSecretLineFromPipe(t *testing.T) {
	var errW bytes.Buffer
	got, err := readSecretLine(strings.NewReader("  sk-test-key \nrest\n"), &errW, "key: ")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-key", got)
	assert.Empty(t, errW.String(), "no prompt when not on a terminal")

	got, err = readSecretLine(strings.NewReader("no-newline"), &errW, "key: ")
	require.NoError(t, err)
	assert.Equal(t, "no-newline", got)

	_, err = readSecretLine(strings.NewReader(""), &errW, "key: ")
	assert.Error(t, err)
}

func TestCheckLLMKeyProvider(t *testing.T) {
	t.Cleanup(func() { llmKeyProvider = "" })
	for _, p := range []string{"", "local", "anthropic", "openai", "gemini", "ollama", "cloudflare"} {
		llmKeyProvider = p
		assert.NoError(t, checkLLMKeyProvider(nil, nil), p)
	}
	llmKeyProvider = "opnai"
	err := checkLLMKeyProvider(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openai")
}

func mockKeychain(t *testing.T) *secrets.Mock {
	t.Helper()
	m := &secrets.Mock{}
	old := newLLMKeyStore
	newLLMKeyStore = func() secrets.Store { return m }
	t.Cleanup(func() { newLLMKeyStore = old })
	for _, k := range []string{"VIBE_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OLLAMA_API_KEY", "CLOUDFLARE_API_TOKEN"} {
		t.Setenv(k, "")
	}
	return m
}

func TestConfigLLMSet_RefusesToRouteFileKeyToAnotherProvider(t *testing.T) {
	m := mockKeychain(t)
	path := t.TempDir() + "/vibe.yaml"
	t.Setenv("VIBE_CONFIG_FILE", path)
	for _, k := range []string{"VIBE_LLM_PROVIDER", "VIBE_LLM_MODEL", "VIBE_LLM_ENDPOINT", "VIBE_LLM_ACCOUNT_ID", "VIBE_LLM_GATEWAY", "CLOUDFLARE_ACCOUNT_ID"} {
		t.Setenv(k, "")
	}
	require.NoError(t, os.WriteFile(path, []byte("llm:\n  provider: openai\n  api_key: sk-legacy-NEEDLE\n"), 0o600))
	set := func(args ...string) error {
		llmSet.yes = false
		llmSetCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
		require.NoError(t, llmSetCmd.Flags().Parse(args))
		llmSetCmd.SetOut(&bytes.Buffer{})
		llmSetCmd.SetErr(&bytes.Buffer{})
		return llmSetCmd.RunE(llmSetCmd, nil)
	}
	err := set("--provider", "cloudflare")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "would be sent to Cloudflare AI Gateway")
	assert.Contains(t, err.Error(), "llm-key migrate")
	assert.NotContains(t, err.Error(), "NEEDLE")
	assert.Equal(t, "openai", config.ReadLLMSettings(path).Provider, "nothing saved")

	// A key in the keychain for the new provider is fine.
	require.NoError(t, config.SetLLMKey(t.Context(), m, "cloudflare", "cf"))
	require.NoError(t, set("--provider", "cloudflare"))
	// --yes accepts the file key.
	require.NoError(t, set("--provider", "gemini", "--yes"))
	assert.Equal(t, "gemini", config.ReadLLMSettings(path).Provider)
}

func TestConfigLLMShowSetModels(t *testing.T) {
	mockKeychain(t)
	path := t.TempDir() + "/vibe.yaml"
	t.Setenv("VIBE_CONFIG_FILE", path)
	for _, k := range []string{"VIBE_LLM_PROVIDER", "VIBE_LLM_MODEL", "VIBE_LLM_ENDPOINT", "VIBE_LLM_ACCOUNT_ID", "VIBE_LLM_GATEWAY", "CLOUDFLARE_ACCOUNT_ID"} {
		t.Setenv(k, "")
	}
	run := func(c *cobra.Command, args ...string) string {
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&out)
		c.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
		require.NoError(t, c.Flags().Parse(args))
		require.NoError(t, c.RunE(c, nil))
		return out.String()
	}
	out := run(llmShowCmd)
	assert.Contains(t, out, "local (default)", "")
	run(llmSetCmd, "--provider", "openai")
	out = run(llmShowCmd)
	assert.Contains(t, out, "openai")
	assert.Contains(t, out, "gpt-5.5 (default)")
	run(llmSetCmd, "--provider", "cloudflare", "--account-id", "0123456789abcdef0123456789abcdef", "--gateway", "gw")
	out = run(llmShowCmd)
	assert.Contains(t, out, "gateway:  gw")
	assert.Contains(t, run(llmModelsCmd), "openai/gpt-5.5")
	b, _ := os.ReadFile(path)
	assert.NotContains(t, string(b), "api_key")
}
