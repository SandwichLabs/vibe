package dotenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// State is where a workspace's .env stands for this person.
type State string

// The states. "changed" is an allow whose file content is no longer the
// approved content.
const (
	StateNone      State = "none"
	StateUndecided State = "undecided"
	StateAllowed   State = "allowed"
	StateChanged   State = "changed"
	StateDenied    State = "denied"
)

const (
	decAllow = "allow"
	decDeny  = "deny"
)

type decision struct {
	Decision string `json:"decision"`
	// SHA256 is set for an allow: the approved content.
	SHA256 string `json:"sha256,omitempty"`
}

type decisionFile struct {
	Version int `json:"version"`
	// Files is keyed by the absolute path of the .env.
	Files map[string]decision `json:"files"`
}

// Decisions remembers this person's allow and deny decisions for .env files.
// It is a small JSON file in the user config directory (next to the
// per-workspace settings), never inside a workspace folder. An empty path keeps
// decisions in memory only (tests).
//
// Allow is keyed by absolute path plus content hash, like `direnv allow`: any
// change to the file ends the allow. Deny is keyed by path alone and holds
// until revoked.
type Decisions struct {
	path string
	mu   sync.Mutex
	mem  decisionFile
}

// NewDecisions returns the store backed by path ("" = memory only).
func NewDecisions(path string) *Decisions {
	return &Decisions{path: path, mem: decisionFile{Version: 1, Files: map[string]decision{}}}
}

// Path is the backing file ("" for memory only).
func (d *Decisions) Path() string { return d.path }

func (d *Decisions) load() decisionFile {
	f := decisionFile{Version: 1, Files: map[string]decision{}}
	if d.path == "" {
		return d.mem
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		return f
	}
	var got decisionFile
	if json.Unmarshal(data, &got) != nil || got.Files == nil {
		return f
	}
	return got
}

func (d *Decisions) save(f decisionFile) error {
	if d.path == "" {
		d.mem = f
		return nil
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode .env decisions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(d.path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".dotenv-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to save .env decisions: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("failed to save .env decisions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("failed to save .env decisions: %w", err)
	}
	if err := os.Rename(name, d.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("failed to save .env decisions: %w", err)
	}
	return nil
}

// State reports the state of the file at path with the given content hash
// ("" when the file is absent or unusable).
func (d *Decisions) State(path, hash string) State {
	if hash == "" {
		return StateNone
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	dec, ok := d.load().Files[path]
	switch {
	case !ok:
		return StateUndecided
	case dec.Decision == decDeny:
		return StateDenied
	case dec.Decision == decAllow && dec.SHA256 == hash:
		return StateAllowed
	case dec.Decision == decAllow:
		return StateChanged
	}
	return StateUndecided
}

// Allow records an allow of path with exactly this content hash.
func (d *Decisions) Allow(path, hash string) error {
	if hash == "" {
		return errors.New("there is no .env to allow")
	}
	return d.set(path, decision{Decision: decAllow, SHA256: hash})
}

// Deny records a deny of path (until revoked).
func (d *Decisions) Deny(path string) error { return d.set(path, decision{Decision: decDeny}) }

// Revoke forgets any decision for path.
func (d *Decisions) Revoke(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.load()
	if _, ok := f.Files[path]; !ok {
		return nil
	}
	delete(f.Files, path)
	return d.save(f)
}

func (d *Decisions) set(path string, dec decision) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.load()
	f.Files[path] = dec
	return d.save(f)
}
