package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const acct = "0123456789abcdef0123456789abcdef"

func TestSaveLLMSettings_KeepsCommentsKeysAndAPIKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vibe.yaml")
	orig := "# my config\neditor: nano # keep\nllm:\n  # which provider\n  provider: local\n  api_key: \"sk-legacy-XYZ\"   # legacy\n  model: old\nweb:\n  port: 9\n"
	require.NoError(t, os.WriteFile(p, []byte(orig), 0o640))

	require.NoError(t, SaveLLMSettings(p, LLMSettings{Provider: "cloudflare", AccountID: acct, Gateway: "my-gw", Model: ""}))
	b, _ := os.ReadFile(p)
	s := string(b)
	assert.Contains(t, s, "# my config")
	assert.Contains(t, s, "editor: nano # keep")
	assert.Contains(t, s, "# which provider")
	assert.Contains(t, s, "api_key: \"sk-legacy-XYZ\"")
	assert.Contains(t, s, "# legacy")
	assert.Contains(t, s, "port: 9")
	assert.NotContains(t, s, "model:", "empty model removes the key")

	got := ReadLLMSettings(p)
	assert.Equal(t, LLMSettings{Provider: "cloudflare", AccountID: acct, Gateway: "my-gw"}, got)
	assert.Equal(t, "sk-legacy-XYZ", fileKeyViaViper(p))
	info, _ := os.Stat(p)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestSaveLLMSettings_CreatesFile0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "vibe.yaml")
	require.NoError(t, SaveLLMSettings(p, LLMSettings{Provider: "ollama", Model: "qwen3:0.6b", Endpoint: "http://localhost:11434/v1"}))
	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, "qwen3:0.6b", ReadLLMSettings(p).Model)
}

func TestSaveLLMSettings_FollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.yaml")
	link := filepath.Join(dir, "link.yaml")
	require.NoError(t, os.WriteFile(target, []byte("llm:\n  provider: local\n"), 0o600))
	require.NoError(t, os.Symlink(target, link))
	require.NoError(t, SaveLLMSettings(link, LLMSettings{Provider: "openai"}))
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "link survives")
	assert.Equal(t, "openai", ReadLLMSettings(target).Provider)
}

func TestSaveLLMSettings_Validation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.yaml")
	for name, s := range map[string]LLMSettings{
		"provider": {Provider: "nope"},
		"endpoint": {Provider: "local", Endpoint: "ftp://x"},
		"nohost":   {Provider: "local", Endpoint: "http://"},
		"model":    {Provider: "local", Model: "a\nb"},
		"account":  {Provider: "cloudflare", AccountID: "../evil"},
		"gateway":  {Provider: "cloudflare", Gateway: "a/b"},
	} {
		assert.Error(t, SaveLLMSettings(p, s), name)
	}
	_, err := os.Stat(p)
	assert.True(t, os.IsNotExist(err), "nothing written on a refused save")
}

func TestSaveLLMSettings_RefusesMultiDoc(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.yaml")
	require.NoError(t, os.WriteFile(p, []byte("a: 1\n---\nb: 2\n"), 0o600))
	assert.Error(t, SaveLLMSettings(p, LLMSettings{Provider: "local"}))
}

func TestSaveLLMSettingsKeepingEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.yaml")
	require.NoError(t, os.WriteFile(p, []byte("llm:\n  provider: local\n  model: filemodel\n"), 0o600))
	t.Setenv("VIBE_LLM_MODEL", "envmodel")
	require.NoError(t, SaveLLMSettingsKeepingEnv(p, LLMSettings{Provider: "openai", Model: "envmodel"}))
	got := ReadLLMSettings(p)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "filemodel", got.Model, "the env value is not frozen into the file")
	assert.Equal(t, map[string]string{"model": "VIBE_LLM_MODEL"}, LLMEnvOverrides())
}

func TestLoad_NewKeysFromEnvAndFallback(t *testing.T) {
	t.Setenv("VIBE_CONFIG_FILE", filepath.Join(t.TempDir(), "none.yaml"))
	t.Setenv("VIBE_LLM_ACCOUNT_ID", acct)
	t.Setenv("VIBE_LLM_GATEWAY", "gw1")
	c := Load().LLM
	assert.Equal(t, acct, c.AccountID)
	assert.Equal(t, "gw1", c.Gateway)
	assert.False(t, c.AccountIDFromEnv)

	t.Setenv("VIBE_LLM_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", acct)
	c = Load().LLM
	assert.Equal(t, acct, c.AccountID)
	assert.True(t, c.AccountIDFromEnv)
}

func TestOllamaDefaultsAndKeys(t *testing.T) {
	t.Setenv("VIBE_CONFIG_FILE", filepath.Join(t.TempDir(), "none.yaml"))
	t.Setenv("VIBE_LLM_PROVIDER", "ollama")
	t.Setenv("OLLAMA_API_KEY", "ok")
	c := Load().LLM
	assert.Equal(t, DefaultOllamaEndpoint, c.Endpoint)
	assert.Equal(t, "ok", c.APIKey)
	assert.Equal(t, "", c.EffectiveModel())
	assert.Equal(t, "gpt-5.5", LLMConfig{Provider: "openai"}.EffectiveModel())
	assert.Equal(t, "openai/gpt-5.5", DefaultModelFor("cloudflare"))
}

func TestCloudflareBaseURL(t *testing.T) {
	u, err := CloudflareBaseURL(acct)
	require.NoError(t, err)
	assert.Equal(t, "https://api.cloudflare.com/client/v4/accounts/"+acct+"/ai/v1", u)
	_, err = CloudflareBaseURL(acct + "/../x")
	assert.Error(t, err)
}

func TestSaveLLMSettings_KeepsHeaderTrailingCommentsAndDocStart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.yaml")
	orig := "---\n# header comment\n\neditor: nano\nllm:\n  provider: local\n\n# mcp:\n#   port: 1\n"
	require.NoError(t, os.WriteFile(p, []byte(orig), 0o600))
	require.NoError(t, SaveLLMSettings(p, LLMSettings{Provider: "openai"}))
	b, _ := os.ReadFile(p)
	s := string(b)
	assert.Contains(t, s, "# header comment\n")
	assert.Contains(t, s, "# mcp:\n#   port: 1")
	assert.Contains(t, s, "---")
	assert.Equal(t, "openai", ReadLLMSettings(p).Provider)
}

func TestSaveLLMSettings_DanglingSymlinkErrors(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link.yaml")
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing.yaml"), link))
	assert.Error(t, SaveLLMSettings(link, LLMSettings{Provider: "local"}))
	_, err := os.Stat(filepath.Join(dir, "missing.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestSaveLLMSettings_RefusesOddShapes(t *testing.T) {
	for name, content := range map[string]string{
		"mixed-case llm": "LLM:\n  provider: local\n",
		"anchor":         "llm:\n  provider: &p local\n",
		"alias":          "base: &b {provider: local}\nllm: *b\n",
		"anchored llm":   "llm: &l\n  provider: local\n",
		"alias value":    "x: &m gpt\nllm:\n  model: *m\n",
	} {
		p := filepath.Join(t.TempDir(), "v.yaml")
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		assert.Error(t, SaveLLMSettings(p, LLMSettings{Provider: "openai"}), name)
		b, _ := os.ReadFile(p)
		assert.Equal(t, content, string(b), name+": file untouched")
	}
}

func TestValidateModel_RejectsFormatCharacters(t *testing.T) {
	assert.Error(t, ValidateModel("gpt​-5"))
	assert.Error(t, ValidateModel("a‮b"))
	assert.NoError(t, ValidateModel("openai/gpt-5.5"))
}
