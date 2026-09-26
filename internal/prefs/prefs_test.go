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
	d, err := DefaultDir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/vibe-test-cfg", d)

	t.Setenv("VIBE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	d, err = DefaultDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg", "vibe"), d)
}
