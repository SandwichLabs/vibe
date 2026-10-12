package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/config"
	"github.com/SandwichLabs/vibe/internal/secrets"
)

// keychainTimeout bounds the work behind one binding. The keychain store has
// its own per-call timeout; this covers a probe plus a lookup.
const keychainTimeout = 15 * time.Second

// llmKeyTTL is how long a resolved key is reused before the keychain is asked
// again (so a key changed from the CLI is noticed). Set/Clear/Migrate drop it
// at once.
const llmKeyTTL = 30 * time.Second

type llmKeyCache struct {
	mu     sync.Mutex
	gen    uint64 // bumped by every Set/Clear/Migrate; a resolve that began before one must not fill the cache
	key    string
	source config.KeySource
	at     time.Time
	valid  bool
}

func (c *llmKeyCache) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.key = ""
	c.gen++
	c.mu.Unlock()
}

// llmAPIKey resolves the model client's key: VIBE_LLM_API_KEY, the provider's
// standard env var, the keychain, then the legacy plaintext file value. The
// key is handed to the model client only; no binding returns it.
func (a *App) llmAPIKey() string {
	key, _ := a.llmKeyAndSource()
	return key
}

func (a *App) llmKeyAndSource() (string, config.KeySource) {
	a.keyCache.mu.Lock()
	if a.keyCache.valid && time.Since(a.keyCache.at) < llmKeyTTL {
		k, s := a.keyCache.key, a.keyCache.source
		a.keyCache.mu.Unlock()
		return k, s
	}
	gen := a.keyCache.gen
	a.keyCache.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	k, s := config.ResolveLLMKey(ctx, a.secretStore(), a.llmSnapshot())
	a.keyCache.mu.Lock()
	if a.keyCache.gen == gen {
		a.keyCache.key, a.keyCache.source, a.keyCache.at, a.keyCache.valid = k, s, time.Now(), true
	}
	a.keyCache.mu.Unlock()
	return k, s
}

// llmSnapshot is the LLM config as last set, readable without a.mu: callers
// such as AgentChat resolve the key while already holding a.mu.RLock, and a
// second RLock could deadlock behind a waiting writer.
func (a *App) llmSnapshot() config.LLMConfig {
	if p := a.llmSnap.Load(); p != nil {
		return *p
	}
	return config.LLMConfig{}
}

func (a *App) setLLMSnapshot(c config.LLMConfig) { a.llmSnap.Store(&c) }

func (a *App) secretStore() secrets.Store { return a.secrets.Keychain() }

// KeychainProbe is the answer to "does the keychain work here".
type KeychainProbe = secrets.Probe

// ProbeKeychain checks the OS keychain with one lookup. It never returns a
// secret.
func (a *App) ProbeKeychain() KeychainProbe {
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	return a.secrets.ProbeKeychain(ctx)
}

// LLMKeyStatus says where the agent's API key comes from, never what it is.
type LLMKeyStatus struct {
	Provider string `json:"provider"`
	// Source is env, keychain, file or none.
	Source string `json:"source"`
	Set    bool   `json:"set"`
	// FileKey is true when ~/.vibe.yaml still holds a plaintext key.
	FileKey bool `json:"fileKey"`
	// CanMigrate is FileKey with a working keychain: show the migration banner.
	CanMigrate bool `json:"canMigrate"`
	// KeychainAvailable is false when the keychain cannot be reached.
	KeychainAvailable bool `json:"keychainAvailable"`
	// KeychainHasKey is true when the keychain already holds a key; migrating
	// then replaces it, so the UI asks first.
	KeychainHasKey bool   `json:"keychainHasKey"`
	Detail         string `json:"detail,omitempty"`
}

// GetLLMKeyStatus reports the source of the agent's API key for provider ("" =
// the configured one), so the form can show it for a provider being picked
// before it is saved. Write-only contract: set / clear / is-set, never read.
func (a *App) GetLLMKeyStatus(provider string) LLMKeyStatus {
	snap := a.llmSnapshot()
	var src config.KeySource
	p, err := a.llmProviderFor(provider)
	if err != nil {
		p = snap.Provider // unknown: report the configured one
	}
	if p == snap.Provider {
		provider = p
		_, src = a.llmKeyAndSource()
	} else {
		// The provider being picked, not saved yet: resolve its key without
		// touching the cache of the configured one.
		provider = p
		snap.Provider = p
		snap.APIKey = ""
		ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
		_, src = config.ResolveLLMKey(ctx, a.secretStore(), snap)
		cancel()
	}
	probe := a.ProbeKeychain()
	st := LLMKeyStatus{
		Provider:          provider,
		Source:            string(src),
		Set:               src != config.KeySourceNone,
		FileKey:           config.HasFileKey(config.LLMConfigPath()),
		KeychainAvailable: probe.Available,
	}
	st.CanMigrate = st.FileKey && probe.Available
	if probe.Available {
		ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
		st.KeychainHasKey = config.KeychainHasKey(ctx, a.secretStore(), provider)
		cancel()
	}
	if !probe.Available {
		st.Detail = probe.Detail
	}
	return st
}

func (a *App) llmProviderFor(provider string) (string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = a.llmSnapshot().Provider
	}
	if _, ok := config.LLMProvider(provider); ok {
		return provider, nil
	}
	return "", fmt.Errorf("unknown provider %q", provider)
}

// SetLLMKey stores the agent's API key for provider in the OS keychain. It is
// write-only: nothing returns the key. An empty provider means the configured
// one.
func (a *App) SetLLMKey(provider, key string) error {
	p, err := a.llmProviderFor(provider)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	a.secrets.Redactor().Add(strings.TrimSpace(key))
	defer a.keyCache.invalidate()
	if err := config.SetLLMKey(ctx, a.secretStore(), p, key); err != nil {
		return fmt.Errorf("%s", a.secrets.Redactor().Redact(err.Error()))
	}
	return nil
}

// ClearLLMKey removes the agent's keychain key for provider (the environment
// and the config file are left as they are).
func (a *App) ClearLLMKey(provider string) error {
	p, err := a.llmProviderFor(provider)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	defer a.keyCache.invalidate()
	return config.ClearLLMKey(ctx, a.secretStore(), p)
}

// MigrateLLMKeyToKeychain moves the plaintext llm.api_key from ~/.vibe.yaml
// into the keychain and blanks it in the file, keeping the rest of the file
// (and a backup with the key removed). If the keychain already holds a
// different key it refuses unless overwrite is true (the UI asks first).
func (a *App) MigrateLLMKeyToKeychain(overwrite bool) error {
	snap := a.llmSnapshot()
	path := config.LLMConfigPath()
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	a.secrets.Redactor().Add(snap.FileAPIKey)
	defer a.keyCache.invalidate()
	if err := config.MigrateFileKey(ctx, a.secretStore(), path, snap.Provider, overwrite); err != nil {
		return fmt.Errorf("%s", a.secrets.Redactor().Redact(err.Error()))
	}
	snap.FileAPIKey = ""
	a.setLLMSnapshot(snap)
	a.mu.Lock()
	a.llmCfg.FileAPIKey = ""
	a.mu.Unlock()
	return nil
}
