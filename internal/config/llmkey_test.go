package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

const needle = "sk-ant-NEEDLE-123456"

func cfgFile(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VIBE_CONFIG_FILE", "")
	t.Setenv("VIBE_LLM_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	p := filepath.Join(home, ".vibe.yaml")
	if body != "" {
		require.NoError(t, os.WriteFile(p, []byte(body), 0o640))
	}
	return p
}

func TestLLMConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VIBE_CONFIG_FILE", "")
	assert.Equal(t, filepath.Join(home, ".vibe.yaml"), LLMConfigPath())
	t.Setenv("VIBE_CONFIG_FILE", "/x/y.yaml")
	assert.Equal(t, "/x/y.yaml", LLMConfigPath())
}

func TestResolveLLMKeyOrder(t *testing.T) {
	ctx := context.Background()
	cfgFile(t, "llm:\n  provider: anthropic\n  api_key: "+needle+"-file\n")
	cfg := Load()
	require.Equal(t, LLMProviderAnthropic, cfg.LLM.Provider)
	require.Equal(t, needle+"-file", cfg.LLM.FileAPIKey)

	store := &secrets.Mock{}
	require.NoError(t, SetLLMKey(ctx, store, "anthropic", needle+"-keychain"))

	// 4 file < 3 keychain < 2 provider env < 1 VIBE_LLM_API_KEY
	k, src := ResolveLLMKey(ctx, store, cfg.LLM)
	assert.Equal(t, KeySourceKeychain, src)
	assert.Equal(t, needle+"-keychain", k)

	t.Setenv("ANTHROPIC_API_KEY", needle+"-provider-env")
	k, src = ResolveLLMKey(ctx, store, cfg.LLM)
	assert.Equal(t, KeySourceEnv, src)
	assert.Equal(t, needle+"-provider-env", k)

	t.Setenv("VIBE_LLM_API_KEY", needle+"-vibe-env")
	k, src = ResolveLLMKey(ctx, store, cfg.LLM)
	assert.Equal(t, KeySourceEnv, src)
	assert.Equal(t, needle+"-vibe-env", k)

	t.Setenv("VIBE_LLM_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	require.NoError(t, ClearLLMKey(ctx, store, "anthropic"))
	k, src = ResolveLLMKey(ctx, store, cfg.LLM)
	assert.Equal(t, KeySourceFile, src)
	assert.Equal(t, needle+"-file", k)

	cfg.LLM.FileAPIKey = ""
	k, src = ResolveLLMKey(ctx, store, cfg.LLM)
	assert.Equal(t, KeySourceNone, src)
	assert.Empty(t, k)
}

func TestResolveLLMKeyKeychainErrorFallsThrough(t *testing.T) {
	cfgFile(t, "")
	store := &secrets.Mock{GetFn: func(context.Context, secrets.Ref) ([]byte, error) { return nil, secrets.ErrUnavailable }}
	k, src := ResolveLLMKey(context.Background(), store, LLMConfig{Provider: "anthropic", FileAPIKey: "filekey"})
	assert.Equal(t, KeySourceFile, src)
	assert.Equal(t, "filekey", k)
	_, src = ResolveLLMKey(context.Background(), nil, LLMConfig{Provider: "gemini"})
	assert.Equal(t, KeySourceNone, src)
}

func TestResolveGeminiStandardEnvs(t *testing.T) {
	cfgFile(t, "")
	t.Setenv("GOOGLE_API_KEY", "g-google")
	k, _ := ResolveLLMKey(context.Background(), nil, LLMConfig{Provider: "gemini"})
	assert.Equal(t, "g-google", k)
	t.Setenv("GEMINI_API_KEY", "g-gemini")
	k, _ = ResolveLLMKey(context.Background(), nil, LLMConfig{Provider: "gemini"})
	assert.Equal(t, "g-gemini", k)
}

func TestKeychainKeysPerProvider(t *testing.T) {
	cfgFile(t, "")
	ctx := context.Background()
	store := &secrets.Mock{}
	require.NoError(t, SetLLMKey(ctx, store, "anthropic", "a-key"))
	require.NoError(t, SetLLMKey(ctx, store, "gemini", "g-key"))
	assert.Equal(t, secrets.AppRef("llm-gemini"), LLMKeyRef("gemini"))
	assert.Equal(t, secrets.AppRef("llm-local"), LLMKeyRef(""))
	k, _ := ResolveLLMKey(ctx, store, LLMConfig{Provider: "gemini"})
	assert.Equal(t, "g-key", k)
	assert.Error(t, SetLLMKey(ctx, store, "anthropic", "   "))
}

const commented = `# my vibe config
verbose: false   # keep quiet

llm:
  # which model
  provider: anthropic   # cloud
  model: claude-x
  api_key: "` + needle + `"   # TODO move this
  endpoint: http://localhost:8080/v1
duckdb:
  memory_limit: 1GB
`

func TestMigrateFileKeyPreservesTheRestOfTheFile(t *testing.T) {
	ctx := context.Background()
	p := cfgFile(t, commented)
	store := &secrets.Mock{}

	require.True(t, HasFileKey(p))
	require.NoError(t, MigrateFileKey(ctx, store, p, "anthropic", false))

	got, _ := os.ReadFile(p)
	want := `# my vibe config
verbose: false   # keep quiet

llm:
  # which model
  provider: anthropic   # cloud
  model: claude-x
  api_key: ""   # TODO move this
  endpoint: http://localhost:8080/v1
duckdb:
  memory_limit: 1GB
`
	assert.Equal(t, want, string(got), "only the key changed; comments and layout stay")
	assert.False(t, HasFileKey(p))

	raw, err := store.Get(ctx, LLMKeyRef("anthropic"))
	require.NoError(t, err)
	assert.Equal(t, needle, string(raw))

	bak, err := os.ReadFile(p + ".bak")
	require.NoError(t, err)
	assert.Equal(t, want, string(bak), "the backup does not keep the secret")
	info, _ := os.Stat(p + ".bak")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	info, _ = os.Stat(p)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "mode preserved")

	cfg := Load()
	assert.Empty(t, cfg.LLM.FileAPIKey)
	assert.Equal(t, "claude-x", cfg.LLM.Model)
	k, src := ResolveLLMKey(ctx, store, cfg.LLM)
	assert.Equal(t, KeySourceKeychain, src)
	assert.Equal(t, needle, k)

	assert.ErrorIs(t, MigrateFileKey(ctx, store, p, "anthropic", false), ErrNothingToMigrate)
}

func TestMigrateFileKeyQuotingForms(t *testing.T) {
	for name, line := range map[string]string{
		"plain":  "  api_key: " + needle + " # c",
		"single": "  api_key: '" + needle + "' # c",
		"flow":   "",
	} {
		t.Run(name, func(t *testing.T) {
			body := "llm:\n  provider: anthropic\n" + line + "\n"
			if name == "flow" {
				body = "llm: {provider: anthropic, api_key: " + needle + "}\n"
			}
			p := cfgFile(t, body)
			store := &secrets.Mock{}
			require.NoError(t, MigrateFileKey(context.Background(), store, p, "anthropic", false))
			got, _ := os.ReadFile(p)
			assert.NotContains(t, string(got), needle)
			assert.Contains(t, string(got), `api_key: ""`)
			assert.Contains(t, string(got), "provider: anthropic")
		})
	}
}

func TestMigrateFileKeyFailureLeavesFileUntouched(t *testing.T) {
	ctx := context.Background()
	p := cfgFile(t, commented)

	// Keychain down: nothing changes, no backup.
	down := &secrets.Mock{SetFn: func(context.Context, secrets.Ref, []byte) error { return secrets.ErrUnavailable }}
	err := MigrateFileKey(ctx, down, p, "anthropic", false)
	require.ErrorIs(t, err, secrets.ErrUnavailable)
	got, _ := os.ReadFile(p)
	assert.Equal(t, commented, string(got))
	_, serr := os.Stat(p + ".bak")
	assert.True(t, os.IsNotExist(serr))

	// Keychain that forgets what it was given: refuse to blank the file.
	liar := &secrets.Mock{SetFn: func(context.Context, secrets.Ref, []byte) error { return nil },
		GetFn: func(context.Context, secrets.Ref) ([]byte, error) { return nil, errors.New("gone") }}
	require.Error(t, MigrateFileKey(ctx, liar, p, "anthropic", false))
	got, _ = os.ReadFile(p)
	assert.Equal(t, commented, string(got))

	// A key repeated in a comment cannot be fully removed: refuse.
	p2 := cfgFile(t, "llm:\n  api_key: "+needle+" # was "+needle+"\n")
	require.Error(t, MigrateFileKey(ctx, &secrets.Mock{}, p2, "anthropic", false))
	got, _ = os.ReadFile(p2)
	assert.Contains(t, string(got), needle)

	// Multi-line block scalar: refuse politely.
	p3 := cfgFile(t, "llm:\n  api_key: |\n    "+needle+"\n")
	require.Error(t, MigrateFileKey(ctx, &secrets.Mock{}, p3, "anthropic", false))
}

func TestHasFileKeyMissingFile(t *testing.T) {
	assert.False(t, HasFileKey(filepath.Join(t.TempDir(), "nope.yaml")))
	assert.False(t, HasFileKey(""))
	p := cfgFile(t, "llm:\n  api_key: \"\"\n")
	assert.False(t, HasFileKey(p))
}

func TestMigrateFileKeyFollowsSymlink(t *testing.T) {
	ctx := context.Background()
	p := cfgFile(t, "")
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles-vibe.yaml")
	require.NoError(t, os.WriteFile(target, []byte(commented), 0o600))
	require.NoError(t, os.Symlink(target, p))
	t.Setenv("VIBE_CONFIG_FILE", p)

	store := &secrets.Mock{}
	require.True(t, HasFileKey(p))
	require.NoError(t, MigrateFileKey(ctx, store, p, "anthropic", false))

	fi, err := os.Lstat(p)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "the link survives")
	got, _ := os.ReadFile(target)
	assert.NotContains(t, string(got), needle, "the target itself is blanked")
	_, err = os.Stat(target + ".bak")
	assert.NoError(t, err, "the backup sits next to the target")
	assert.False(t, HasFileKey(p))
}

func TestMigrateFileKeyUnsupportedFormsAreExplained(t *testing.T) {
	ctx := context.Background()
	cases := map[string]string{
		"alias":     "base: &k " + needle + "\nllm:\n  provider: anthropic\n  api_key: *k\n",
		"tag":       "llm:\n  api_key: !!str " + needle + "\n",
		"multiline": "llm:\n  api_key: >\n    " + needle + "\n",
		"multidoc":  "llm:\n  api_key: " + needle + "\n---\nother: 1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p := cfgFile(t, body)
			store := &secrets.Mock{}
			err := MigrateFileKey(ctx, store, p, "anthropic", false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "by hand")
			assert.NotContains(t, err.Error(), needle)
			got, _ := os.ReadFile(p)
			assert.Equal(t, body, string(got), "file untouched")
			assert.Empty(t, store.Calls, "nothing was stored")
		})
	}
}

func TestMigrateDoesNotSilentlyReplaceAKeychainKey(t *testing.T) {
	ctx := context.Background()
	p := cfgFile(t, commented)
	store := &secrets.Mock{}
	require.NoError(t, SetLLMKey(ctx, store, "anthropic", "existing-keychain-key"))
	assert.True(t, KeychainHasKey(ctx, store, "anthropic"))

	require.ErrorIs(t, MigrateFileKey(ctx, store, p, "anthropic", false), ErrKeychainHasKey)
	got, _ := os.ReadFile(p)
	assert.Equal(t, commented, string(got))

	require.NoError(t, MigrateFileKey(ctx, store, p, "anthropic", true))
	raw, _ := store.Get(ctx, LLMKeyRef("anthropic"))
	assert.Equal(t, needle, string(raw))
}

func TestFileKeyWhitespaceIsTrimmedConsistently(t *testing.T) {
	ctx := context.Background()
	p := cfgFile(t, "llm:\n  provider: anthropic\n  api_key: \"  "+needle+" \"\n")
	assert.Equal(t, needle, Load().LLM.FileAPIKey)
	store := &secrets.Mock{}
	require.NoError(t, MigrateFileKey(ctx, store, p, "anthropic", false))
	raw, _ := store.Get(ctx, LLMKeyRef("anthropic"))
	assert.Equal(t, needle, string(raw))
}

func TestResolveLLMKey_OpenAIPrecedence(t *testing.T) {
	cfgFile(t, "")
	ctx := context.Background()
	store := &secrets.Mock{}
	cfg := LLMConfig{Provider: LLMProviderOpenAI, FileAPIKey: "file-key"}

	// 4. the legacy file key, last.
	k, src := ResolveLLMKey(ctx, store, cfg)
	assert.Equal(t, "file-key", k)
	assert.Equal(t, KeySourceFile, src)

	// 3. the keychain item llm-openai beats the file key.
	assert.Equal(t, secrets.AppRef("llm-openai"), LLMKeyRef(LLMProviderOpenAI))
	require.NoError(t, SetLLMKey(ctx, store, LLMProviderOpenAI, "kc-key"))
	k, src = ResolveLLMKey(ctx, store, cfg)
	assert.Equal(t, "kc-key", k)
	assert.Equal(t, KeySourceKeychain, src)

	// 2. OPENAI_API_KEY beats the keychain.
	t.Setenv("OPENAI_API_KEY", "std-env-key")
	k, src = ResolveLLMKey(ctx, store, cfg)
	assert.Equal(t, "std-env-key", k)
	assert.Equal(t, KeySourceEnv, src)

	// 1. VIBE_LLM_API_KEY beats everything.
	t.Setenv("VIBE_LLM_API_KEY", "vibe-env-key")
	k, src = ResolveLLMKey(ctx, store, cfg)
	assert.Equal(t, "vibe-env-key", k)
	assert.Equal(t, KeySourceEnv, src)
}

func TestResolveLLMKey_OpenAIKeysAreNotSharedAcrossProviders(t *testing.T) {
	cfgFile(t, "")
	ctx := context.Background()
	store := &secrets.Mock{}
	t.Setenv("OPENAI_API_KEY", "oai")
	require.NoError(t, SetLLMKey(ctx, store, LLMProviderOpenAI, "oai-kc"))

	// OPENAI_API_KEY and llm-openai are for openai only.
	_, src := ResolveLLMKey(ctx, store, LLMConfig{Provider: LLMProviderAnthropic})
	assert.Equal(t, KeySourceNone, src)
	_, src = ResolveLLMKey(ctx, store, LLMConfig{Provider: LLMProviderGemini})
	assert.Equal(t, KeySourceNone, src)

	require.NoError(t, ClearLLMKey(ctx, store, LLMProviderOpenAI))
	assert.False(t, KeychainHasKey(ctx, store, LLMProviderOpenAI))
}
