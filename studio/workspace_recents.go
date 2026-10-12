package main

// The launcher's recent-workspaces list, kept in the preferences document
// outside every workspace (prefs.Recents).

import (
	"errors"

	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// RecentWorkspace is one entry of the launcher's list.
type RecentWorkspace struct {
	Path       string `json:"path"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LastOpened string `json:"lastOpened"` // RFC 3339
	GitBranch  string `json:"gitBranch"`
	// Missing is true when the folder (or its vibe.yaml) is gone: an unplugged
	// drive, a deleted folder. The entry is never removed automatically.
	Missing bool `json:"missing"`
}

func recentFrom(r prefs.Recent) RecentWorkspace {
	return RecentWorkspace{Path: r.Path, ID: r.ID, Name: r.Name, LastOpened: r.LastOpened}
}

// ListRecentWorkspaces returns the launcher's list, most recent first. It only
// looks at the disk; nothing is opened or dialled.
func (a *App) ListRecentWorkspaces() ([]RecentWorkspace, error) {
	recs := a.recents.List()
	out := make([]RecentWorkspace, 0, len(recs))
	for _, r := range recs {
		rw := recentFrom(r)
		rw.Missing = !workspace.HasConfigFile(r.Path)
		if !rw.Missing {
			rw.GitBranch, _ = workspace.GitInfo(r.Path)
		}
		out = append(out, rw)
	}
	return out, nil
}

// RemoveRecentWorkspace drops an entry from the list (never touching files) and
// returns it so the UI can offer Undo.
func (a *App) RemoveRecentWorkspace(path string) (*RecentWorkspace, error) {
	removed, err := a.recents.Remove(path)
	if err != nil || removed == nil {
		return nil, err
	}
	rw := recentFrom(*removed)
	return &rw, nil
}

// RestoreRecentWorkspace puts back an entry RemoveRecentWorkspace returned
// (Undo).
func (a *App) RestoreRecentWorkspace(r RecentWorkspace) error {
	return a.recents.Restore(prefs.Recent{Path: r.Path, ID: r.ID, Name: r.Name, LastOpened: r.LastOpened})
}

// RelocateRecentWorkspace re-points a missing entry at the folder the person
// found ("Locate…").
func (a *App) RelocateRecentWorkspace(oldPath, newPath string) error {
	root, ok := workspace.ExactRoot(newPath)
	if !ok {
		return errors.New("That folder isn't a Vibe workspace (it has no vibe.yaml).")
	}
	return a.recents.Relocate(oldPath, root)
}
