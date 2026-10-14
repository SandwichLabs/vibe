package prefs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RecentsKey is where the recent-workspaces list lives in the preferences
// document (Preferences.Extra), outside every workspace.
const RecentsKey = "recentWorkspaces"

// MaxRecents caps the launcher's list.
const MaxRecents = 12

// Recent is one remembered workspace folder.
type Recent struct {
	Path       string `json:"path"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LastOpened string `json:"lastOpened"` // RFC 3339
}

// Recents is the launcher's list of recently opened workspaces, kept in the
// preferences document beside the theme. One mutex serialises every
// read-modify-write of that document (the list and SavePreferences), so a stale
// copy round-tripped by the frontend can never roll the list back.
//
// A nil *Recents, or one over a nil store, remembers nothing and lists nothing:
// preferences are optional (no user config dir).
type Recents struct {
	mu     sync.Mutex
	store  *Store
	logger *slog.Logger
}

// NewRecents returns the recents list over store (which may be nil). logger may
// be nil.
func NewRecents(store *Store, logger *slog.Logger) *Recents {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Recents{store: store, logger: logger}
}

// SamePath reports whether a and b name the same folder: equal strings, or equal
// after resolving symlinks.
func SamePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (r *Recents) loadLocked() (Preferences, []Recent) {
	if r == nil || r.store == nil {
		return Defaults(), nil
	}
	p, err := r.store.Load()
	if err != nil {
		r.logger.Warn("recent workspaces: preferences unreadable", "error", err)
	}
	var recs []Recent
	if raw, ok := p.Extra[RecentsKey]; ok {
		if err := json.Unmarshal(raw, &recs); err != nil {
			r.logger.Warn("recent workspaces: list unreadable, starting over", "error", err)
			recs = nil
		}
	}
	return p, recs
}

func (r *Recents) saveLocked(p Preferences, recs []Recent) error {
	if r == nil || r.store == nil {
		return nil
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].LastOpened > recs[j].LastOpened })
	if len(recs) > MaxRecents {
		recs = recs[:MaxRecents]
	}
	raw, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	if p.Extra == nil {
		p.Extra = map[string]json.RawMessage{}
	}
	p.Extra[RecentsKey] = raw
	return r.store.Save(p)
}

// List returns the list, most recent first. It only reads the preferences file.
func (r *Recents) List() []Recent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	_, recs := r.loadLocked()
	r.mu.Unlock()
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].LastOpened > recs[j].LastOpened })
	return recs
}

// Record puts the workspace at root at the top of the list.
func (r *Recents) Record(root, id, name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, recs := r.loadLocked()
	stamp := now()
	found := false
	for i := range recs {
		if SamePath(recs[i].Path, root) {
			recs[i].Path, recs[i].ID, recs[i].Name, recs[i].LastOpened = root, id, name, stamp
			found = true
		}
	}
	if !found {
		recs = append(recs, Recent{Path: root, ID: id, Name: name, LastOpened: stamp})
	}
	if err := r.saveLocked(p, recs); err != nil {
		r.logger.Warn("failed to remember the workspace", "error", err)
	}
}

// Remove drops path from the list (never touching files) and returns the entry
// so the caller can offer Undo. nil when path was not listed.
func (r *Recents) Remove(path string) (*Recent, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, recs := r.loadLocked()
	var removed *Recent
	kept := recs[:0:0]
	for _, e := range recs {
		if SamePath(e.Path, path) && removed == nil {
			e := e
			removed = &e
			continue
		}
		kept = append(kept, e)
	}
	if err := r.saveLocked(p, kept); err != nil {
		return nil, fmt.Errorf("failed to update the list: %w", err)
	}
	return removed, nil
}

// Restore puts back an entry Remove returned (Undo). A path already listed is
// left alone.
func (r *Recents) Restore(rec Recent) error {
	if strings.TrimSpace(rec.Path) == "" {
		return fmt.Errorf("nothing to restore")
	}
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, recs := r.loadLocked()
	for _, e := range recs {
		if SamePath(e.Path, rec.Path) {
			return nil
		}
	}
	if rec.LastOpened == "" {
		rec.LastOpened = now()
	}
	return r.saveLocked(p, append(recs, rec))
}

// Relocate re-points the entry at oldPath at newRoot.
func (r *Recents) Relocate(oldPath, newRoot string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, recs := r.loadLocked()
	for i := range recs {
		if SamePath(recs[i].Path, oldPath) || recs[i].Path == oldPath {
			recs[i].Path = newRoot
		}
	}
	return r.saveLocked(p, recs)
}

// IDs maps every remembered folder to its workspace id (entries without one are
// left out).
func (r *Recents) IDs() map[string]string {
	out := map[string]string{}
	for _, e := range r.List() {
		if e.ID != "" {
			out[e.Path] = e.ID
		}
	}
	return out
}

// SavePreferences saves p, but takes the recents list from the file on disk
// instead of p: the frontend round-trips only the fields it knows, and a stale
// copy of the list must never roll it back. The error is "unavailable" when
// there is no store.
func (r *Recents) SavePreferences(p Preferences) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("preferences are unavailable: no user config directory")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, _ := r.store.Load()
	extra := map[string]json.RawMessage{}
	for k, v := range p.Extra {
		extra[k] = v
	}
	// Keys the frontend does not round-trip come from disk, never from p.
	for _, key := range []string{RecentsKey, UpdatesKey} {
		if raw, ok := cur.Extra[key]; ok {
			extra[key] = raw
		} else {
			delete(extra, key)
		}
	}
	p.Extra = extra
	if err := r.store.Save(p); err != nil {
		return fmt.Errorf("failed to save preferences: %w", err)
	}
	return nil
}
