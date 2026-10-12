package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

// KeySource says where the LLM API key in effect came from.
type KeySource string

// Where a key can come from, in resolution order.
const (
	KeySourceEnv      KeySource = "env"
	KeySourceKeychain KeySource = "keychain"
	KeySourceFile     KeySource = "file"
	KeySourceNone     KeySource = "none"
)

// LLMKeyRef is the keychain reference for a provider's API key: {app,
// llm-<provider>}.
func LLMKeyRef(provider string) secrets.Ref {
	if provider == "" {
		provider = LLMProviderLocal
	}
	return secrets.AppRef("llm-" + provider)
}

// envKey returns the environment key in effect: VIBE_LLM_API_KEY first, then
// the provider's standard variable.
func envKey(provider string) string {
	if k := os.Getenv("VIBE_LLM_API_KEY"); k != "" {
		return k
	}
	return providerKeyEnv(provider)
}

// ResolveLLMKey finds the API key for cfg's provider, in this order:
// VIBE_LLM_API_KEY, the provider's standard env var (ANTHROPIC_API_KEY,
// OPENAI_API_KEY, GEMINI_API_KEY), the keychain item {app, llm-<provider>}, then the legacy
// plaintext llm.api_key in ~/.vibe.yaml (read-only). An unreachable keychain
// is skipped, not fatal. The key is for the model client only: it must never
// cross the Wails bridge, MCP or the agent.
func ResolveLLMKey(ctx context.Context, store secrets.Store, cfg LLMConfig) (string, KeySource) {
	if k := envKey(cfg.Provider); k != "" {
		return k, KeySourceEnv
	}
	if store != nil {
		if raw, err := store.Get(ctx, LLMKeyRef(cfg.Provider)); err == nil && len(raw) > 0 {
			return string(raw), KeySourceKeychain
		}
	}
	if cfg.FileAPIKey != "" {
		return cfg.FileAPIKey, KeySourceFile
	}
	return "", KeySourceNone
}

// ErrNothingToMigrate means ~/.vibe.yaml holds no plaintext API key.
var ErrNothingToMigrate = errors.New("there is no API key in the config file to move")

// SetLLMKey stores the provider's key in the keychain (write-only).
func SetLLMKey(ctx context.Context, store secrets.Store, provider, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("the API key is empty")
	}
	if err := store.Set(ctx, LLMKeyRef(provider), []byte(key)); err != nil {
		return fmt.Errorf("failed to store the API key in the keychain: %w", err)
	}
	return nil
}

// ClearLLMKey removes the provider's keychain key. It leaves the environment
// and the file alone.
func ClearLLMKey(ctx context.Context, store secrets.Store, provider string) error {
	if err := store.Delete(ctx, LLMKeyRef(provider)); err != nil {
		return fmt.Errorf("failed to remove the API key from the keychain: %w", err)
	}
	return nil
}

// ErrKeychainHasKey means the keychain already holds a different key for the
// provider; moving the file's key would replace it. Retry with overwrite once
// the person has agreed.
var ErrKeychainHasKey = errors.New("the keychain already has a different API key for this provider")

// HasFileKey reports whether the config file at path holds a plaintext
// llm.api_key, judged the way Load reads it (viper), so what the app uses and
// what the migration offer says cannot disagree.
func HasFileKey(path string) bool { return fileKeyViaViper(path) != "" }

// KeychainHasKey reports whether the keychain holds a key for provider.
func KeychainHasKey(ctx context.Context, store secrets.Store, provider string) bool {
	if store == nil {
		return false
	}
	raw, err := store.Get(ctx, LLMKeyRef(provider))
	return err == nil && len(raw) > 0
}

// MigrateFileKey moves the plaintext llm.api_key from the config file at
// path into the keychain under provider, then blanks it in the file. Order:
// store and read back, back up the file (<path>.bak, mode 0600, with the key
// already blanked so the backup does not keep the secret), edit only that
// scalar (comments and layout stay), verify the file still parses with the
// key empty and nothing else changed, then rename into place. If anything
// before the rename fails the file is untouched.
//
// A symlinked path is resolved and the target edited in place, so the link
// survives and no plaintext copy is left behind. A key the editor cannot
// safely blank (YAML alias, tag, block scalar, several documents, repeated in
// a comment) is reported with "remove it by hand"; nothing is stored then.
// A different key already in the keychain is only replaced with overwrite.
func MigrateFileKey(ctx context.Context, store secrets.Store, path, provider string, overwrite bool) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	path = real
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	want := strings.TrimSpace(fileKeyViaViper(path))
	if want == "" {
		return ErrNothingToMigrate
	}
	byHand := func(why error) error {
		return fmt.Errorf("Vibe can't safely edit %s (%w). Save the key with \"vibe config llm-key set\" and delete llm.api_key from the file by hand", path, why)
	}
	if n, derr := yamlDocCount(data); derr != nil || n != 1 {
		return byHand(errors.New("the file is not a single YAML document"))
	}
	loc, err := findYAMLScalar(data, "llm", "api_key")
	if err != nil {
		return byHand(err)
	}
	if strings.TrimSpace(loc.value) != want {
		return byHand(errors.New("the key is written in a form Vibe does not edit"))
	}
	blanked, err := loc.blank(data)
	if err != nil {
		return byHand(err)
	}
	if err := verifyBlanked(data, blanked); err != nil {
		return byHand(err)
	}
	if !overwrite {
		if raw, gerr := store.Get(ctx, LLMKeyRef(provider)); gerr == nil && string(raw) != want {
			return ErrKeychainHasKey
		}
	}
	if err := SetLLMKey(ctx, store, provider, want); err != nil {
		return err
	}
	got, err := store.Get(ctx, LLMKeyRef(provider))
	if err != nil || string(got) != want {
		return errors.New("the keychain did not return the key it just stored; the config file was not changed")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if err := writeFileAtomic(path+".bak", blanked, 0o600); err != nil {
		return fmt.Errorf("failed to write the backup: %w", err)
	}
	if err := writeFileAtomic(path, blanked, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to update %s: %w", path, err)
	}
	return nil
}

// fileKeyViaViper reads llm.api_key from path exactly as Load does.
func fileKeyViaViper(path string) string {
	if path == "" {
		return ""
	}
	fv := viper.New()
	fv.SetConfigFile(path)
	fv.SetConfigType("yaml")
	if fv.ReadInConfig() != nil {
		return ""
	}
	return strings.TrimSpace(fv.GetString("llm.api_key"))
}
