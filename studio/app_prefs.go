package main

import (
	"fmt"

	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- Preferences (app-global, not per-file) ---

// prefsStore returns the preferences store, or an error when the app was
// built without one (no user config dir).
func (a *App) prefsStore() (*prefs.Store, error) {
	if a.prefs == nil {
		return nil, fmt.Errorf("preferences are unavailable: no user config directory")
	}
	return a.prefs, nil
}

// GetPreferences returns the user's app-level preferences (theme, appearance).
// A missing or unreadable settings file yields the defaults; a parse error is
// logged, never surfaced, so a bad edit can't lock the user out of the UI.
func (a *App) GetPreferences() (prefs.Preferences, error) {
	s, err := a.prefsStore()
	if err != nil {
		return prefs.Defaults(), nil
	}
	p, err := s.Load()
	if err != nil {
		a.logger.Warn("preferences: falling back to defaults", "error", err)
	}
	return p, nil
}

// SetPreferences persists the user's app-level preferences.
func (a *App) SetPreferences(p prefs.Preferences) error {
	// The frontend round-trips only the fields it knows (theme, appearance); the
	// recent-workspaces list lives beside them in the same document and always
	// comes from disk, so a stale copy never rolls it back.
	return a.recents.SavePreferences(p)
}

// UserThemesResult is what ListUserThemes returns: every readable theme file
// plus a human-readable line per file that was skipped.
type UserThemesResult struct {
	Themes   []prefs.ThemeFile `json:"themes"`
	Problems []string          `json:"problems"`
	Dir      string            `json:"dir"`
}

// ListUserThemes scans <config dir>/vibe/themes/*.json. Validation happens in
// the frontend (studio/frontend/src/theme/schema.ts) so the rules live once.
func (a *App) ListUserThemes() (UserThemesResult, error) {
	s, err := a.prefsStore()
	if err != nil {
		return UserThemesResult{}, nil
	}
	themes, problems := s.ListUserThemes()
	return UserThemesResult{Themes: themes, Problems: problems, Dir: s.ThemesDir()}, nil
}

// OpenThemesDir creates the themes folder (with a README describing the
// format) and opens it in the OS file manager. Returns the path either way.
func (a *App) OpenThemesDir() (string, error) {
	s, err := a.prefsStore()
	if err != nil {
		return "", err
	}
	dir, err := s.EnsureThemesDir()
	if err != nil {
		return "", err
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, "file://"+dir)
	}
	return dir, nil
}
