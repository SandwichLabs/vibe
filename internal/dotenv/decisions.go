package dotenv

import (
	"errors"

	"github.com/SandwichLabs/assay/internal/trustfile"
)

// State is where a workspace's .env stands for this person.
type State = trustfile.State

// The states. "changed" is an allow whose file content is no longer the
// approved content.
const (
	StateNone      State = "none"
	StateUndecided       = trustfile.StateUndecided
	StateAllowed         = trustfile.StateAllowed
	StateChanged         = trustfile.StateChanged
	StateDenied          = trustfile.StateDenied
)

// Decisions remembers this person's allow and deny decisions for .env files.
// It is the trustfile store (a small JSON file in the user config directory,
// next to the per-workspace settings, never inside a workspace folder). An
// empty path keeps decisions in memory only (tests).
//
// Allow is keyed by absolute path plus content hash, like `direnv allow`: any
// change to the file ends the allow. Deny is keyed by path alone and holds
// until revoked.
type Decisions struct {
	t *trustfile.Decisions
}

// NewDecisions returns the store backed by path ("" = memory only).
func NewDecisions(path string) *Decisions { return &Decisions{t: trustfile.New(path)} }

// Path is the backing file ("" for memory only).
func (d *Decisions) Path() string { return d.t.Path() }

// State reports the state of the file at path with the given content hash
// ("" when the file is absent or unusable).
func (d *Decisions) State(path, hash string) State {
	if hash == "" {
		return StateNone
	}
	return d.t.State(path, hash)
}

// Allow records an allow of path with exactly this content hash.
func (d *Decisions) Allow(path, hash string) error {
	if hash == "" {
		return errors.New("there is no .env to allow")
	}
	return d.t.Allow(path, hash)
}

// Deny records a deny of path (until revoked).
func (d *Decisions) Deny(path string) error { return d.t.Deny(path) }

// Revoke forgets any decision for path.
func (d *Decisions) Revoke(path string) error { return d.t.Revoke(path) }
