package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

const studioNeedle = "sk-ant-NEEDLE-studio-test"

func secretsApp(t *testing.T, cfgYAML string) (*App, *secrets.Mock, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, "vibe-test.yaml")
	t.Setenv("VIBE_CONFIG_FILE", cfgPath)
	for _, k := range []string{"VIBE_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"} {
		t.Setenv(k, "")
	}
	if cfgYAML != "" {
		require.NoError(t, os.WriteFile(cfgPath, []byte(cfgYAML), 0o600))
	}
	a := NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)))
	m := &secrets.Mock{}
	a.secrets = secrets.NewResolver(secrets.WithKeychain(m), secrets.WithRedactor(a.secrets.Redactor()))
	return a, m, cfgPath
}

func TestLLMKeyBindingsAreWriteOnly(t *testing.T) {
	a, m, _ := secretsApp(t, "llm:\n  provider: anthropic\n")

	st := a.GetLLMKeyStatus("")
	assert.Equal(t, "none", st.Source)
	assert.False(t, st.Set)
	assert.True(t, st.KeychainAvailable)

	require.NoError(t, a.SetLLMKey("", studioNeedle))
	assert.Equal(t, studioNeedle, a.llmAPIKey(), "the model client gets the key")
	st = a.GetLLMKeyStatus("")
	assert.Equal(t, "keychain", st.Source)
	assert.True(t, st.Set)
	b, _ := json.Marshal(st)
	assert.NotContains(t, string(b), "NEEDLE", "no binding result carries the key")
	probe, _ := json.Marshal(a.ProbeKeychain())
	assert.NotContains(t, string(probe), "NEEDLE")

	require.NoError(t, a.ClearLLMKey(""))
	assert.Equal(t, "", a.llmAPIKey())
	assert.Equal(t, "none", a.GetLLMKeyStatus("").Source)
	for _, c := range m.Calls {
		assert.NotContains(t, c, "NEEDLE")
	}

	assert.Error(t, a.SetLLMKey("nosuch", "x"))
	assert.Error(t, a.SetLLMKey("", "   "))
}

func TestLLMKeyEnvWins(t *testing.T) {
	a, _, _ := secretsApp(t, "llm:\n  provider: anthropic\n")
	require.NoError(t, a.SetLLMKey("", "from-keychain"))
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	a.keyCache.invalidate()
	assert.Equal(t, "from-env", a.llmAPIKey())
	assert.Equal(t, "env", a.GetLLMKeyStatus("").Source)
}

func TestLLMKeyMigration(t *testing.T) {
	yaml := "# keep me\nllm:\n  provider: anthropic\n  api_key: " + studioNeedle + "\n"
	a, _, cfgPath := secretsApp(t, yaml)

	st := a.GetLLMKeyStatus("")
	assert.Equal(t, "file", st.Source)
	assert.True(t, st.FileKey)
	assert.True(t, st.CanMigrate)

	require.NoError(t, a.MigrateLLMKeyToKeychain(false))
	got, _ := os.ReadFile(cfgPath)
	assert.NotContains(t, string(got), "NEEDLE")
	assert.Contains(t, string(got), "# keep me")

	st = a.GetLLMKeyStatus("")
	assert.Equal(t, "keychain", st.Source)
	assert.False(t, st.FileKey)
	assert.False(t, st.CanMigrate)
	assert.Equal(t, studioNeedle, a.llmAPIKey())
	assert.Error(t, a.MigrateLLMKeyToKeychain(false), "nothing left to move")
}

func TestLLMKeyKeychainDown(t *testing.T) {
	a, m, _ := secretsApp(t, "llm:\n  provider: anthropic\n  api_key: "+studioNeedle+"\n")
	m.GetFn = func(context.Context, secrets.Ref) ([]byte, error) { return nil, secrets.ErrUnavailable }
	m.SetFn = func(context.Context, secrets.Ref, []byte) error { return secrets.ErrUnavailable }

	st := a.GetLLMKeyStatus("")
	assert.False(t, st.KeychainAvailable)
	assert.False(t, st.CanMigrate, "no migration offer without a keychain")
	assert.Equal(t, "file", st.Source, "the legacy value keeps working")
	assert.NotEmpty(t, st.Detail)

	err := a.SetLLMKey("", studioNeedle)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "NEEDLE")
	require.Error(t, a.MigrateLLMKeyToKeychain(false))
}

func TestMigrationAsksBeforeReplacingAKeychainKey(t *testing.T) {
	a, _, _ := secretsApp(t, "llm:\n  provider: anthropic\n  api_key: "+studioNeedle+"\n")
	require.NoError(t, a.SetLLMKey("", "already-in-keychain"))
	st := a.GetLLMKeyStatus("")
	assert.True(t, st.KeychainHasKey)
	require.Error(t, a.MigrateLLMKeyToKeychain(false))
	assert.Equal(t, "already-in-keychain", a.llmAPIKey())
	require.NoError(t, a.MigrateLLMKeyToKeychain(true))
	assert.Equal(t, studioNeedle, a.llmAPIKey())
}

func TestInFlightResolveCannotRepopulateAfterClear(t *testing.T) {
	a, m, _ := secretsApp(t, "llm:\n  provider: anthropic\n")
	require.NoError(t, a.SetLLMKey("", "old-key-value"))
	started, release := make(chan struct{}), make(chan struct{})
	m.GetFn = func(ctx context.Context, ref secrets.Ref) ([]byte, error) {
		close(started)
		<-release
		return []byte("old-key-value"), nil
	}
	a.keyCache.invalidate()
	done := make(chan string)
	go func() { done <- a.llmAPIKey() }()
	<-started
	m.GetFn = nil
	require.NoError(t, a.ClearLLMKey(""))
	close(release)
	<-done
	assert.Equal(t, "", a.llmAPIKey(), "the stale in-flight result must not be served after Clear")
}

func TestLLMKeyBindings_OpenAI(t *testing.T) {
	a, m, _ := secretsApp(t, "llm:\n  provider: openai\n")
	st := a.GetLLMKeyStatus("")
	assert.Equal(t, "openai", st.Provider)
	assert.False(t, st.Set)

	// Explicit provider and configured (empty) provider both land on llm-openai.
	require.NoError(t, a.SetLLMKey("", studioNeedle))
	got, err := m.Get(context.Background(), secrets.AppRef("llm-openai"))
	require.NoError(t, err)
	assert.Equal(t, studioNeedle, string(got))
	assert.Equal(t, studioNeedle, a.llmAPIKey())

	st = a.GetLLMKeyStatus("")
	assert.True(t, st.Set)
	assert.Equal(t, "keychain", st.Source)
	raw, err := json.Marshal(st)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), studioNeedle)

	require.NoError(t, a.ClearLLMKey("openai"))
	assert.False(t, a.GetLLMKeyStatus("").Set)

	// An unknown provider is rejected, and the error never echoes a key.
	err = a.SetLLMKey("openia", studioNeedle)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), studioNeedle)
}

func TestLLMKeyBindings_OpenAIStandardEnv(t *testing.T) {
	a, _, _ := secretsApp(t, "llm:\n  provider: openai\n")
	t.Setenv("OPENAI_API_KEY", "from-openai-env")
	a.keyCache.invalidate()
	assert.Equal(t, "from-openai-env", a.llmAPIKey())
	assert.Equal(t, "env", a.GetLLMKeyStatus("").Source)
}
