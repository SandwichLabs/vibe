// Package prefs stores app-global user preferences for Vibe Studio — the
// settings that belong to the person, not to any one workspace (theme,
// appearance). They live outside every workspace folder so that switching
// workspaces never changes how the app looks, and so a repository shared with
// someone else never carries the author's UI preferences along.
//
// Storage is a small JSON document at <config dir>/vibe/settings.json, where
// <config dir> is os.UserConfigDir() (~/.config on Linux, ~/Library/Application
// Support on macOS) unless VIBE_CONFIG_DIR overrides it. User-authored theme
// files live beside it in <config dir>/vibe/themes/*.json — the VS Code model:
// drop a file in the folder and it shows up in the picker.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ThemeSystem is the Preferences.Theme value meaning "follow the OS light/dark
// setting, using SystemLight / SystemDark".
const ThemeSystem = "system"

// Default theme ids. They must match built-in theme ids registered in the
// Studio frontend (studio/frontend/src/theme/builtin.ts).
const (
	DefaultTheme      = "schoolhouse-light"
	DefaultThemeLight = "schoolhouse-light"
	DefaultThemeDark  = "schoolhouse-dark"
)

// Preferences is the persisted document. Unknown fields round-trip untouched
// so an older Studio never strips settings written by a newer one.
type Preferences struct {
	// Theme is a theme id, or ThemeSystem to follow the OS appearance.
	Theme string `json:"theme"`
	// SystemLight / SystemDark are the theme ids used when Theme == ThemeSystem.
	SystemLight string `json:"systemLight"`
	SystemDark  string `json:"systemDark"`
	// ReduceMotion disables the hover/press transitions and slide-ins.
	ReduceMotion bool `json:"reduceMotion"`
	// UIScale is a percentage (100 = default). Bounded by Normalize.
	UIScale int `json:"uiScale"`

	// Extra carries fields this build does not know about.
	Extra map[string]json.RawMessage `json:"-"`
}

// Defaults returns the preferences a fresh install starts with.
func Defaults() Preferences {
	return Preferences{
		Theme:       DefaultTheme,
		SystemLight: DefaultThemeLight,
		SystemDark:  DefaultThemeDark,
		UIScale:     100,
	}
}

// Normalize fills blanks with defaults and clamps out-of-range values so a
// hand-edited settings file can never leave the UI in an unusable state.
func (p Preferences) Normalize() Preferences {
	d := Defaults()
	if strings.TrimSpace(p.Theme) == "" {
		p.Theme = d.Theme
	}
	if strings.TrimSpace(p.SystemLight) == "" {
		p.SystemLight = d.SystemLight
	}
	if strings.TrimSpace(p.SystemDark) == "" {
		p.SystemDark = d.SystemDark
	}
	switch {
	case p.UIScale == 0:
		p.UIScale = d.UIScale
	case p.UIScale < 75:
		p.UIScale = 75
	case p.UIScale > 150:
		p.UIScale = 150
	}
	return p
}

// MarshalJSON writes known fields plus any Extra fields carried through.
func (p Preferences) MarshalJSON() ([]byte, error) {
	type known Preferences
	base, err := json.Marshal(known(p))
	if err != nil {
		return nil, err
	}
	if len(p.Extra) == 0 {
		return base, nil
	}
	merged := map[string]json.RawMessage{}
	for k, v := range p.Extra {
		merged[k] = v
	}
	var ours map[string]json.RawMessage
	if err := json.Unmarshal(base, &ours); err != nil {
		return nil, err
	}
	for k, v := range ours { // known fields always win
		merged[k] = v
	}
	return json.Marshal(merged)
}

// UnmarshalJSON reads known fields and stashes the rest in Extra.
func (p *Preferences) UnmarshalJSON(data []byte) error {
	type known Preferences
	var k known
	if err := json.Unmarshal(data, &k); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for _, name := range []string{"theme", "systemLight", "systemDark", "reduceMotion", "uiScale"} {
		delete(all, name)
	}
	*p = Preferences(k)
	if len(all) > 0 {
		p.Extra = all
	}
	return nil
}

// ThemeFile is one user-authored theme found in the themes directory. The
// JSON is returned verbatim; the frontend owns validation so that the rules
// live in exactly one place (studio/frontend/src/theme/schema.ts).
type ThemeFile struct {
	// Path is the absolute path of the file (shown in Settings so the user can
	// find what they are editing).
	Path string `json:"path"`
	// Filename is the base name, e.g. "nord.json".
	Filename string `json:"filename"`
	// JSON is the raw file contents.
	JSON string `json:"json"`
}

// MaxThemeFileBytes bounds a single theme file. Anything larger is skipped
// (and reported) rather than read into memory — a theme is a few KB.
const MaxThemeFileBytes = 256 * 1024

// Store reads and writes the preferences document and lists user themes.
type Store struct {
	mu  sync.Mutex
	dir string
}

// DefaultDir returns the per-user config directory Vibe uses:
// $VIBE_CONFIG_DIR if set to an absolute path (a relative one is ignored with
// a warning to logger, which may be nil), else os.UserConfigDir()/vibe.
func DefaultDir(logger *slog.Logger) (string, error) {
	if d := strings.TrimSpace(os.Getenv("VIBE_CONFIG_DIR")); d != "" {
		if filepath.IsAbs(d) {
			return d, nil
		}
		// A relative directory would resolve against whatever the working
		// directory happens to be, so a different folder per launch (and
		// inside a cloned repository when run from one): ignore it.
		if logger != nil {
			logger.Warn("ignoring VIBE_CONFIG_DIR: it must be an absolute path", "value", d)
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to locate user config dir: %w", err)
	}
	return filepath.Join(base, "vibe"), nil
}

// NewStore returns a Store rooted at dir. The directory is created lazily on
// first Save / EnsureThemesDir, never on construction.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the store's root directory.
func (s *Store) Dir() string { return s.dir }

// SettingsPath returns the path of the settings document.
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, "settings.json") }

// ThemesDir returns the directory scanned for user theme files.
func (s *Store) ThemesDir() string { return filepath.Join(s.dir, "themes") }

// Load reads the preferences. A missing file yields Defaults(); a corrupt file
// yields Defaults() plus the parse error so the caller can surface it without
// the UI failing to start.
func (s *Store) Load() (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.SettingsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("failed to read preferences: %w", err)
	}
	var p Preferences
	if err := json.Unmarshal(data, &p); err != nil {
		return Defaults(), fmt.Errorf("failed to parse %s: %w", s.SettingsPath(), err)
	}
	return p.Normalize(), nil
}

// Save writes the preferences atomically (temp file + rename) with 0600 perms.
func (s *Store) Save(p Preferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p = p.Normalize()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode preferences: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.dir, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp preferences file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to write preferences: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to set preferences permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("failed to close preferences file: %w", err)
	}
	if err := os.Rename(tmpName, s.SettingsPath()); err != nil {
		cleanup()
		return fmt.Errorf("failed to replace preferences file: %w", err)
	}
	return nil
}

// EnsureThemesDir creates the themes directory (and a README explaining the
// format) so "Open themes folder" in Settings always lands somewhere real.
func (s *Store) EnsureThemesDir() (string, error) {
	dir := s.ThemesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create themes dir: %w", err)
	}
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); errors.Is(err, fs.ErrNotExist) {
		_ = os.WriteFile(readme, []byte(themesReadme), 0o600) // best-effort
	}
	return dir, nil
}

// ListUserThemes returns every *.json file in the themes directory, sorted by
// filename. Files that cannot be read, or exceed MaxThemeFileBytes, are
// reported in the second return value and otherwise skipped. A missing
// directory is not an error: it simply yields no themes.
func (s *Store) ListUserThemes() ([]ThemeFile, []string) {
	dir := s.ThemesDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}

	var out []ThemeFile
	var problems []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if info.Size() > MaxThemeFileBytes {
			problems = append(problems, fmt.Sprintf("%s: file is larger than %d KB", name, MaxThemeFileBytes/1024))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if !json.Valid(data) {
			problems = append(problems, fmt.Sprintf("%s: not valid JSON", name))
			continue
		}
		out = append(out, ThemeFile{Path: path, Filename: name, JSON: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out, problems
}

const themesReadme = `# Vibe Studio themes

Drop a *.json theme file in this folder and it appears under
Settings → Appearance the next time you press "Reload themes".

A theme file looks like:

    {
      "name": "My Theme",
      "type": "dark",
      "extends": "schoolhouse-dark",
      "colors": {
        "accent": "#7C9CFF",
        "bg": "#14161E"
      }
    }

"extends" names a built-in theme (schoolhouse-light, schoolhouse-dark,
signal, chalkboard) whose values fill in every key you leave out, so a theme
can be as small as one colour. Colours must be hex (#RGB, #RRGGBB or
#RRGGBBAA). The full key list is documented in docs/reference/themes.md
in the Vibe repository.
`
