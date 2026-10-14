package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_MissingFileYieldsDefaults(t *testing.T) {
	s := NewStore(t.TempDir())
	p, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, Defaults(), p)
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nested", "vibe"))
	want := Preferences{Theme: ThemeSystem, SystemLight: "signal", SystemDark: "chalkboard", ReduceMotion: true, UIScale: 110}
	require.NoError(t, s.Save(want))

	info, err := os.Stat(s.SettingsPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "settings are private to the user")

	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// No stray temp files left behind by the atomic write.
	entries, _ := os.ReadDir(s.Dir())
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".settings-"), "temp file %s leaked", e.Name())
	}
}

func TestLoad_CorruptFileYieldsDefaultsAndError(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, os.MkdirAll(s.Dir(), 0o700))
	require.NoError(t, os.WriteFile(s.SettingsPath(), []byte("{not json"), 0o600))
	p, err := s.Load()
	require.Error(t, err)
	assert.Equal(t, Defaults(), p, "a broken file must never take the UI down")
}

func TestNormalize_FillsBlanksAndClamps(t *testing.T) {
	p := Preferences{UIScale: 999}.Normalize()
	assert.Equal(t, DefaultTheme, p.Theme)
	assert.Equal(t, DefaultThemeLight, p.SystemLight)
	assert.Equal(t, DefaultThemeDark, p.SystemDark)
	assert.Equal(t, 150, p.UIScale)
	assert.Equal(t, 75, Preferences{UIScale: 10}.Normalize().UIScale)
	assert.Equal(t, 100, Preferences{}.Normalize().UIScale)
}

func TestUnknownFieldsRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, os.MkdirAll(s.Dir(), 0o700))
	require.NoError(t, os.WriteFile(s.SettingsPath(),
		[]byte(`{"theme":"signal","futureSetting":{"a":1},"uiScale":100}`), 0o600))

	p, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, "signal", p.Theme)
	require.Contains(t, p.Extra, "futureSetting")

	p.Theme = "chalkboard"
	require.NoError(t, s.Save(p))

	raw, err := os.ReadFile(s.SettingsPath())
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.JSONEq(t, `{"a":1}`, string(m["futureSetting"]), "a newer build's setting survives a save by this one")
	assert.JSONEq(t, `"chalkboard"`, string(m["theme"]))
}

func TestListUserThemes(t *testing.T) {
	s := NewStore(t.TempDir())

	themes, problems := s.ListUserThemes()
	assert.Empty(t, themes)
	assert.Empty(t, problems, "a missing themes dir is not an error")

	dir, err := s.EnsureThemesDir()
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "README.md"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "zed.json"), []byte(`{"name":"Zed"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.JSON"), []byte(`{"name":"Alpha"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{oops`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden.json"), []byte(`{}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(`x`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "huge.json"), []byte("["+strings.Repeat("1,", MaxThemeFileBytes/2)+"1]"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub.json"), 0o700))

	themes, problems = s.ListUserThemes()
	require.Len(t, themes, 2)
	assert.Equal(t, "alpha.JSON", themes[0].Filename)
	assert.Equal(t, "zed.json", themes[1].Filename)
	assert.Equal(t, `{"name":"Zed"}`, themes[1].JSON)
	assert.Equal(t, filepath.Join(dir, "zed.json"), themes[1].Path)

	require.Len(t, problems, 2)
	joined := strings.Join(problems, "\n")
	assert.Contains(t, joined, "broken.json: not valid JSON")
	assert.Contains(t, joined, "huge.json")
}

func TestDefaultDir_EnvOverride(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", "/tmp/vibe-test-cfg")
	d, err := DefaultDir(nil)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/vibe-test-cfg", d)

	t.Setenv("VIBE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	d, err = DefaultDir(nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg", "vibe"), d)
}

func TestDefaultDir_RelativeEnvIsIgnored(t *testing.T) {
	abs := t.TempDir()
	t.Setenv("VIBE_CONFIG_DIR", abs)
	got, err := DefaultDir(nil)
	require.NoError(t, err)
	assert.Equal(t, abs, got)

	t.Setenv("VIBE_CONFIG_DIR", "relative/dir")
	got, err = DefaultDir(nil)
	require.NoError(t, err)
	assert.NotContains(t, got, "relative/dir", "a relative VIBE_CONFIG_DIR falls back to the user config dir")
	assert.True(t, filepath.IsAbs(got))
}

func TestUpdateSettings_RoundTripAndSurviveFrontendSaves(t *testing.T) {
	s := NewStore(t.TempDir())
	r := NewRecents(s, nil)
	assert.Equal(t, UpdateSettings{}, r.UpdateSettings(), "unset until asked")

	require.NoError(t, r.ModifyUpdateSettings(func(u *UpdateSettings) { u.Check = UpdateCheckWeekly; u.LastChecked = "2026-10-13T00:00:00Z" }))
	require.NoError(t, r.ModifyUpdateSettings(func(u *UpdateSettings) { u.SkippedVersion = "v0.2.0" }))
	assert.Equal(t, UpdateSettings{Check: "weekly", LastChecked: "2026-10-13T00:00:00Z", SkippedVersion: "v0.2.0"}, r.UpdateSettings())

	// The frontend round-trips only the fields it knows; a stale copy must not erase the update answer.
	p, err := s.Load()
	require.NoError(t, err)
	p.Extra = nil
	p.Theme = "signal"
	require.NoError(t, r.SavePreferences(p))
	assert.Equal(t, "weekly", r.UpdateSettings().Check)
	loaded, _ := s.Load()
	assert.Equal(t, "signal", loaded.Theme)

	// Recents and updates share the document without clobbering each other.
	r.Record("/w", "id", "w")
	assert.Len(t, r.List(), 1)
	assert.Equal(t, "weekly", r.UpdateSettings().Check)
}

func TestUpdateSettings_BadValuesReadAsNeverAsked(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, os.MkdirAll(s.Dir(), 0o700))
	require.NoError(t, os.WriteFile(s.SettingsPath(), []byte(`{"updates":{"check":"hourly"}}`), 0o600))
	assert.Equal(t, "", NewRecents(s, nil).UpdateSettings().Check)
	require.NoError(t, os.WriteFile(s.SettingsPath(), []byte(`{"updates":"junk"}`), 0o600))
	assert.Equal(t, UpdateSettings{}, NewRecents(s, nil).UpdateSettings())

	var nilRecents *Recents
	assert.Equal(t, UpdateSettings{}, nilRecents.UpdateSettings())
	assert.Error(t, nilRecents.ModifyUpdateSettings(func(*UpdateSettings) {}))
}
