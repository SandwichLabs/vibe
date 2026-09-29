package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrFileInUse is returned (wrapped) when another process already holds a
// .vibe file's writer lock (#80).
var ErrFileInUse = errors.New("file is already open in another Vibe process")

// LockHolder is what the lock holder writes into <file>.vibe.lock, for
// informational display only: the kernel flock, not this content, is the
// actual lock, so a stale or unreadable LockHolder never blocks anything.
type LockHolder struct {
	App  string `json:"app"`
	PID  int    `json:"pid"`
	Host string `json:"host"`
}

// String renders the holder for user-facing messages, e.g.
// "Vibe Studio (pid 4242)" or "vibe mcp serve (pid 4242) on other-host".
func (h LockHolder) String() string {
	s := fmt.Sprintf("%s (pid %d)", h.App, h.PID)
	if h.Host != "" {
		if hostname, err := os.Hostname(); err != nil || h.Host != hostname {
			s += " on " + h.Host
		}
	}
	return s
}

// InUseError is returned by Open/Create when a writer lock is already held.
// Holder is nil when the lockfile couldn't be read or parsed — a race with
// the holder writing it, or leftover content from an older version.
type InUseError struct {
	Path   string
	Holder *LockHolder
}

func (e *InUseError) Error() string {
	who := "another Vibe process"
	if e.Holder != nil {
		who = e.Holder.String()
	}
	return fmt.Sprintf("%s is already open in %s; close it there first", filepath.Base(e.Path), who)
}

func (e *InUseError) Unwrap() error { return ErrFileInUse }

// lockPath returns the sidecar lock file path for a .vibe file. It resolves
// symlinks first so a symlinked .vibe shares its target's lock; if that
// fails (e.g. the file doesn't exist yet, as on Create), it falls back to
// the given path unresolved.
func lockPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	return resolved + ".lock"
}

// readHolder reads and parses the LockHolder written into a lock file,
// returning nil on any error (missing file, race with the writer, stale
// content) since Holder is informational only.
func readHolder(lp string) *LockHolder {
	data, err := os.ReadFile(lp)
	if err != nil {
		return nil
	}
	var h LockHolder
	if err := json.Unmarshal(data, &h); err != nil {
		return nil
	}
	return &h
}
