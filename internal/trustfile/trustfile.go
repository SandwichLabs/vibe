// Package trustfile remembers one person's allow and deny decisions about
// things that arrive in a workspace folder and could act on their behalf: a
// workspace .env, a custom artifact view. It is a leaf (standard library only).
//
// Allow is keyed by an absolute path plus a content hash, like `direnv allow`:
// any change to the content ends the allow. Deny is keyed by path alone and
// holds until revoked. The decisions are a small JSON file in the user config
// directory, never inside a workspace folder or a bundle. An empty file path
// keeps them in memory only (tests). Load-modify-save runs under an advisory
// lock on a file beside the decisions file (flock; a no-op where there is none),
// so Studio and the CLI cannot lose each other's decisions. It is a leaf: the
// standard library only.
package trustfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// State is where a path stands for this person.
type State string

// The states. StateChanged is an allow whose content is no longer the approved
// content.
const (
	StateUndecided State = "undecided"
	StateAllowed   State = "allowed"
	StateChanged   State = "changed"
	StateDenied    State = "denied"
)

const (
	decAllow = "allow"
	decDeny  = "deny"
)

// ErrStale is returned by AllowIf when the content is no longer the content the
// person was shown.
var ErrStale = errors.New("the content changed since it was shown")

type decision struct {
	Decision string `json:"decision"`
	// SHA256 is set for an allow: the approved content.
	SHA256 string `json:"sha256,omitempty"`
}

type decisionFile struct {
	Version int `json:"version"`
	// Files is keyed by the absolute path of the thing decided on.
	Files map[string]decision `json:"files"`
}

// Entry is one stored decision, for listing.
type Entry struct {
	Path string
	// Decision is "allow" or "deny".
	Decision string
	// SHA256 is the approved content hash of an allow.
	SHA256 string
}

// Decisions is the store.
type Decisions struct {
	path string
	mu   sync.Mutex
	mem  decisionFile
}

// New returns the store backed by path ("" = memory only).
func New(path string) *Decisions {
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
		return fmt.Errorf("failed to encode decisions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(d.path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".trust-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to save decisions: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("failed to save decisions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("failed to save decisions: %w", err)
	}
	if err := os.Rename(name, d.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("failed to save decisions: %w", err)
	}
	return nil
}

// State reports where path stands for the content with the given hash.
func (d *Decisions) State(path, hash string) State {
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

// lock serialises a load-modify-save across processes. Memory-only stores need
// none.
func (d *Decisions) lock() (func(), error) {
	if d.path == "" {
		return func() {}, nil
	}
	unlock, err := lockPath(d.path)
	if err != nil {
		return nil, fmt.Errorf("failed to save decisions: %w", err)
	}
	return unlock, nil
}

// Allow records an allow of path with exactly this content hash.
func (d *Decisions) Allow(path, hash string) error {
	return d.set(path, decision{Decision: decAllow, SHA256: hash})
}

// AllowIf records the allow only when current() still returns shown, checked
// while the store is locked: the person approved the content with hash shown,
// and a file that changed since is refused with ErrStale. current is called
// once, with the lock held, and must not call back into d.
func (d *Decisions) AllowIf(path, shown string, current func() (string, error)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	unlock, err := d.lock()
	if err != nil {
		return err
	}
	defer unlock()
	now, err := current()
	if err != nil {
		return err
	}
	if shown == "" || now != shown {
		return ErrStale
	}
	f := d.load()
	f.Files[path] = decision{Decision: decAllow, SHA256: shown}
	return d.save(f)
}

// Deny records a deny of path (until revoked).
func (d *Decisions) Deny(path string) error { return d.set(path, decision{Decision: decDeny}) }

// Revoke forgets any decision for path.
func (d *Decisions) Revoke(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	unlock, err := d.lock()
	if err != nil {
		return err
	}
	defer unlock()
	f := d.load()
	if _, ok := f.Files[path]; !ok {
		return nil
	}
	delete(f.Files, path)
	return d.save(f)
}

// List returns every stored decision ordered by path.
func (d *Decisions) List() []Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.load()
	out := make([]Entry, 0, len(f.Files))
	for p, dec := range f.Files {
		out = append(out, Entry{Path: p, Decision: dec.Decision, SHA256: dec.SHA256})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (d *Decisions) set(path string, dec decision) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	unlock, err := d.lock()
	if err != nil {
		return err
	}
	defer unlock()
	f := d.load()
	f.Files[path] = dec
	return d.save(f)
}
