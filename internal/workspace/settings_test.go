package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

func baseSettings(ws Workspace) Settings {
	return Settings{Name: ws.Name(), DefaultConnection: "local", Sandbox: SandboxReadWrite, AskBeforeChanges: true}
}

func readConfigFile(t *testing.T, ws Workspace) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws.Root(), ConfigFile))
	require.NoError(t, err)
	return string(b)
}

func TestApplySettings_OnlyTightens(t *testing.T) {
	ws, _ := newWorkspace(t)

	// A tier above the team limit.
	s := baseSettings(ws)
	s.TeamSandbox, s.Sandbox = SandboxReadOnly, SandboxFull
	err := ApplySettings(ws, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't go above")
	assert.Equal(t, "", ws.Config().Sandbox, "nothing was written")

	// The gate cannot be turned off when the team requires it.
	s = baseSettings(ws)
	s.TeamAskBeforeChanges, s.AskBeforeChanges = true, false
	require.Error(t, ApplySettings(ws, s))
	assert.Nil(t, ws.Config().AskBeforeWrites)

	// A person may be tighter than the team, and loosen the gate when the team does not require it.
	s = baseSettings(ws)
	s.TeamSandbox, s.Sandbox, s.AskBeforeChanges = SandboxReadWrite, SandboxReadOnly, false
	require.NoError(t, ApplySettings(ws, s))
	assert.Equal(t, SandboxReadWrite, ws.Config().Sandbox)
	assert.Equal(t, SandboxReadOnly, ws.Policy().Sandbox)
	_, ask := ws.AgentPolicy()
	assert.False(t, ask)
	yml := readConfigFile(t, ws)
	assert.Contains(t, yml, "sandbox: read-write")
	assert.NotContains(t, yml, "ask_before_writes: false", "the loosening stays out of the committed file")
}

func TestApplySettings_ValidatesBeforeWriting(t *testing.T) {
	ws, _ := newWorkspace(t)
	before := readConfigFile(t, ws)
	cases := map[string]func(*Settings){
		"unknown team tier":          func(s *Settings) { s.TeamSandbox = "root" },
		"unknown own tier":           func(s *Settings) { s.Sandbox = "root" },
		"empty own tier":             func(s *Settings) { s.Sandbox = "" },
		"unknown default":            func(s *Settings) { s.DefaultConnection = "nope" },
		"gate off under a team gate": func(s *Settings) { s.TeamAskBeforeChanges, s.AskBeforeChanges = true, false },
	}
	for name, mutate := range cases {
		s := baseSettings(ws)
		s.Name = "Renamed"
		mutate(&s)
		require.Error(t, ApplySettings(ws, s), name)
		assert.Equal(t, before, readConfigFile(t, ws), name)
	}
	s := baseSettings(ws)
	s.Sandbox = "root"
	assert.EqualError(t, ApplySettings(ws, s), `unknown sandbox mode: "root" (want read-only, read-write, or full)`)
}

func TestApplySettings_FullIsNoLimitAndTheNameIsTrimmed(t *testing.T) {
	ws, _ := newWorkspace(t)
	s := baseSettings(ws)
	s.Name, s.TeamSandbox = "  Spaced  ", SandboxFull
	require.NoError(t, ApplySettings(ws, s))
	assert.Equal(t, "Spaced", ws.Config().Name)
	assert.Empty(t, ws.Config().Sandbox)
	assert.NotContains(t, readConfigFile(t, ws), "sandbox:")
	// Saving the same form again changes nothing on disk.
	before := readConfigFile(t, ws)
	require.NoError(t, ApplySettings(ws, s))
	assert.Equal(t, before, readConfigFile(t, ws))
}

func TestApplySettings_AStaleTeamFormIsAConflict(t *testing.T) {
	ws, _ := newWorkspace(t)
	cfg := ws.Config()
	cfg.Sandbox = SandboxReadOnly
	require.NoError(t, ws.SaveConfig(cfg))
	loaded, loadedAsk := "", false
	s := baseSettings(ws)
	s.LoadedTeamSandbox, s.LoadedTeamAsk = &loaded, &loadedAsk
	err := ApplySettings(ws, s)
	var sc *SettingsConflictError
	require.ErrorAs(t, err, &sc)
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorIs(t, err, filestore.ErrConflict)
	assert.Contains(t, sc.Msg, "team's agent limit")
	assert.Equal(t, SandboxReadOnly, ws.Config().Sandbox, "the team's change is not overwritten")

	// "full" in the file reads as the form's "" (no limit): not stale.
	cfg = ws.Config()
	cfg.Sandbox = SandboxFull
	require.NoError(t, ws.SaveConfig(cfg))
	require.NoError(t, ApplySettings(ws, s))

	// The gate moved under the form.
	cfg = ws.Config()
	cfg.AskBeforeWrites = Bool(true)
	require.NoError(t, ws.SaveConfig(cfg))
	require.ErrorAs(t, ApplySettings(ws, s), &sc)

	// A form that did not say what it loaded is never stale.
	s.LoadedTeamSandbox, s.LoadedTeamAsk = nil, nil
	s.TeamAskBeforeChanges = true
	require.NoError(t, ApplySettings(ws, s))
}

func TestApplySettings_VibeYamlChangedOnDiskIsAConflictAndNothingIsWritten(t *testing.T) {
	ws, _ := newWorkspace(t)
	path := filepath.Join(ws.Root(), ConfigFile)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(b, []byte("\n# edited elsewhere\n")...), 0o600))
	s := baseSettings(ws)
	s.Name = "Mine"
	err = ApplySettings(ws, s)
	var sc *SettingsConflictError
	require.ErrorAs(t, err, &sc)
	assert.Contains(t, sc.Msg, "vibe.yaml changed on disk")
	assert.Contains(t, readConfigFile(t, ws), "edited elsewhere")
	assert.NotContains(t, readConfigFile(t, ws), "Mine")
}

func TestApplySettings_RefusesWhenLocalSettingsAreUntrusted(t *testing.T) {
	ws, _ := newWorkspace(t)
	require.NoError(t, ws.SaveLocal(ws.Local()))
	require.NoError(t, os.Remove(filepath.Join(ws.Root(), ".vibe-local", "nonce")))
	require.NoError(t, ws.Reload())
	require.False(t, ws.LocalTrusted())
	before := readConfigFile(t, ws)
	s := baseSettings(ws)
	s.Name = "Mine"
	err := ApplySettings(ws, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different folder")
	assert.Equal(t, before, readConfigFile(t, ws))
}

func TestSetLocalAgent_TierAndGateAgainstTheCommittedLimit(t *testing.T) {
	ws, _ := newWorkspace(t)
	cfg := ws.Config()
	cfg.Sandbox, cfg.AskBeforeWrites = SandboxReadWrite, Bool(true)
	require.NoError(t, ws.SaveConfig(cfg))

	err := SetLocalAgent(ws, SandboxFull, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team limit in vibe.yaml is read-write")
	err = SetLocalAgent(ws, SandboxReadWrite, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires the agent to ask")
	require.Error(t, SetLocalAgent(ws, "root", true))

	require.NoError(t, SetLocalAgent(ws, SandboxReadOnly, true))
	assert.Equal(t, SandboxReadOnly, ws.Policy().Sandbox)
}

func TestParseSandbox(t *testing.T) {
	for _, ok := range []string{SandboxReadOnly, SandboxReadWrite, SandboxFull} {
		got, err := ParseSandbox(ok)
		require.NoError(t, err)
		assert.Equal(t, ok, got)
	}
	for _, bad := range []string{"", "root", "READ-ONLY"} {
		_, err := ParseSandbox(bad)
		require.Error(t, err, bad)
	}
}

func TestValidateSandbox_PinsMessage(t *testing.T) {
	for _, ok := range []string{"", "read-only", "read-write", "full"} {
		assert.NoError(t, ValidateSandbox(ok), ok)
	}
	for _, bad := range []string{"FULL", " full", "root"} {
		assert.EqualError(t, ValidateSandbox(bad), `unknown sandbox "`+bad+`" (want read-only, read-write or full)`)
	}
}
