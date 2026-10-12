package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SandwichLabs/vibe/internal/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These pin the conflict resolver's behaviour at the binding, so the logic can
// move out of studio without changing what "Keep mine", "Reload" and "Save as
// copy" do (#54 slice 8, P1).

func TestResolveConflict_RejectsUnknownActionAndKind(t *testing.T) {
	app := newOpenApp(t)
	_, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: "x", Action: "merge"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown action "merge"`)
	_, err = app.ResolveConflict(ConflictRequest{Kind: "dashboard", ID: "x", Action: "reload"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown kind "dashboard"`)
}

func TestResolveConflict_RequiresAnOpenWorkspace(t *testing.T) {
	app := newBareApp(t)
	_, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: "x", Action: "reload"})
	assert.ErrorIs(t, err, errNoWorkspace)
}

func TestResolveConflict_ReloadOfAMissingItemFails(t *testing.T) {
	app := newOpenApp(t)
	for _, kind := range []string{"notebook", "query", "skill"} {
		_, err := app.ResolveConflict(ConflictRequest{Kind: kind, ID: "missing-one", Action: "reload"})
		require.Error(t, err, kind)
		assert.False(t, strings.HasPrefix(err.Error(), conflictPrefix), "%s: a missing item is not a conflict: %v", kind, err)
	}
}

func TestResolveConflict_SkillReloadReportsTheDiskRevision(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SaveSkill("rules", "desc", "original", "", ""))
	res, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "reload"})
	require.NoError(t, err)
	assert.Equal(t, "rules", res.ID)
	assert.Equal(t, "rules", res.Title)
	assert.NotEmpty(t, res.Rev)
}

func TestResolveConflict_SkillOverwriteWithoutARevisionReplacesWhatIsOnDisk(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	require.NoError(t, app.SaveSkill("rules", "desc", "original", "", ""))
	spath := filepath.Join(info.Root, "skills", "rules", "SKILL.md")
	b, err := os.ReadFile(spath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spath, append(b, []byte("\nsomeone else\n")...), 0o600))

	res, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "overwrite",
		Data: map[string]any{"description": "desc", "instructions": "mine"}})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Rev)
	b, err = os.ReadFile(spath)
	require.NoError(t, err)
	assert.Contains(t, string(b), "mine")
	assert.NotContains(t, string(b), "someone else")
}

func TestResolveConflict_SkillSaveCopyNaming(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SaveSkill("rules", "d", "original", "", ""))
	data := map[string]any{"description": "d", "instructions": "mine"}
	first, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "saveCopy", Data: data})
	require.NoError(t, err)
	second, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "saveCopy", Data: data})
	require.NoError(t, err)
	assert.Equal(t, "rules-copy", first.ID)
	assert.Equal(t, "rules-copy-2", second.ID)
	assert.Equal(t, second.ID, second.Title, "a skill's title is its name")
}

func TestResolveConflict_QueryOverwriteAfterDeleteWritesItBack(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	q, err := app.SaveQueryOn(SaveQueryRequest{Title: "Orders", SQL: "SELECT 1"})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(info.Root, "queries", q.ID+".sql")))

	// With the banner's revision, as the editor sends it: the file is gone, so it
	// is written back (under a fresh id).
	res, err := app.ResolveConflict(ConflictRequest{Kind: "query", ID: q.ID, Action: "overwrite", Rev: q.Rev,
		Data: map[string]any{"title": "Orders", "sql": "SELECT 2"}})
	require.NoError(t, err)
	got, err := app.GetQuery(res.ID)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", got.SQL)
	assert.Equal(t, "Orders", res.Title)
}

func TestResolveConflict_QueryKeepsWhatTheEditorDoesNotEdit(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	require.NoError(t, os.WriteFile(filepath.Join(info.Root, "queries", "daily.sql"),
		[]byte("-- vibe: {\"title\":\"Daily\",\"description\":\"by the agent\",\"connection\":\"local\"}\nSELECT 1\n"), 0o600))
	// A description from the editor wins; an empty one keeps the disk's.
	_, err := app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "overwrite",
		Data: map[string]any{"title": "Daily", "sql": "SELECT 2", "description": "mine"}})
	require.NoError(t, err)
	got, err := app.GetQuery("daily")
	require.NoError(t, err)
	assert.Equal(t, "mine", got.Description)
	assert.Equal(t, "local", got.Connection)
	_, err = app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "overwrite",
		Data: map[string]any{"title": "Daily", "sql": "SELECT 3"}})
	require.NoError(t, err)
	got, _ = app.GetQuery("daily")
	assert.Equal(t, "mine", got.Description)
}

func TestResolveConflict_NotebookOverwriteAfterDelete(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	nb, err := app.CreateNotebook("Doomed")
	require.NoError(t, err)
	path := filepath.Join(info.Root, "notebooks", nb.ID+".vnb")
	require.NoError(t, os.Remove(path))
	var ev eventLog
	app.emit = ev.sink

	// Without a revision, "Keep mine" writes over whatever is there (here: nothing).
	res, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: nb.ID, Action: "overwrite",
		Data: map[string]any{"title": "Back again", "cells": []any{}}})
	require.NoError(t, err)
	assert.Equal(t, nb.ID, res.ID)
	assert.Equal(t, "Back again", res.Title)
	_, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, 1, ev.count("data:changed"), "the lists refresh once")
	assert.GreaterOrEqual(t, ev.count("notebooks:changed"), 1, "an open notebook hears about the new revision")
}

func TestResolveConflict_NotebookSaveCopyDropsIdAndRev(t *testing.T) {
	app := newOpenApp(t)
	nb, err := app.CreateNotebook("Source")
	require.NoError(t, err)
	local := map[string]any{"id": nb.ID, "rev": "stale", "title": "  ", "cells": []any{}}
	cp, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: nb.ID, Action: "saveCopy", Data: local})
	require.NoError(t, err)
	assert.NotEqual(t, nb.ID, cp.ID)
	assert.Equal(t, "Untitled (copy)", cp.Title, "a blank title becomes Untitled")
	assert.Equal(t, nb.ID, local["id"], "the caller's map is not modified")
	assert.Equal(t, "stale", local["rev"])
}

// ---- workspace settings (SetWorkspaceSettings) ----

func settingsFor(info *WorkspaceInfo) WorkspaceSettings {
	return WorkspaceSettings{Name: info.Name, DefaultConnection: "local", Sandbox: "read-write", AskBeforeChanges: true}
}

func TestSetWorkspaceSettings_RejectsAnUnknownTier(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	s := settingsFor(info)
	s.TeamSandbox = "root"
	_, err := app.SetWorkspaceSettings(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sandbox")
	s = settingsFor(info)
	s.Sandbox = "root"
	_, err = app.SetWorkspaceSettings(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sandbox mode")
}

func TestSetWorkspaceSettings_FullMeansNoTeamLimitAndTrimsTheName(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	s := settingsFor(info)
	s.Name = "  Spaced  "
	s.TeamSandbox = "full"
	got, err := app.SetWorkspaceSettings(s)
	require.NoError(t, err)
	assert.Equal(t, "Spaced", got.Name)
	assert.False(t, got.TeamSandboxSet, "full is written as no limit")
	yml, err := os.ReadFile(filepath.Join(info.Root, workspace.ConfigFile))
	require.NoError(t, err)
	assert.NotContains(t, string(yml), "sandbox:")
}

func TestSetWorkspaceSettings_ChangedOnDiskIsAConflictAndNothingIsWritten(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	// An editor rewrites vibe.yaml behind the app's back (the app has not reloaded).
	path := filepath.Join(info.Root, workspace.ConfigFile)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(b, []byte("\n# edited elsewhere\n")...), 0o600))
	s := settingsFor(info)
	s.Name = "Mine"
	_, err = app.SetWorkspaceSettings(s)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix), "%v", err)
	assert.Contains(t, err.Error(), "vibe.yaml changed on disk")
	b, _ = os.ReadFile(path)
	assert.Contains(t, string(b), "edited elsewhere")
	assert.NotContains(t, string(b), "Mine")
}

func TestSetWorkspaceSettings_StaleTeamGateIsAConflict(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	cfg := app.ws.Config()
	cfg.AskBeforeWrites = workspace.Bool(true)
	require.NoError(t, app.ws.SaveConfig(cfg))
	loadedAsk := false
	s := settingsFor(info)
	s.LoadedTeamAsk = &loadedAsk
	_, err := app.SetWorkspaceSettings(s)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	assert.Contains(t, err.Error(), "team's agent limit")
}

func TestSetWorkspaceSettings_EventsAndRecents(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	var ev eventLog
	app.emit = ev.sink
	s := settingsFor(info)
	s.Name = "Renamed for recents"
	s.Sandbox = "read-only"
	_, err := app.SetWorkspaceSettings(s)
	require.NoError(t, err)
	assert.Equal(t, 1, ev.count("agent:config"))
	assert.Equal(t, 1, ev.count("workspace:changed"))
	list, err := app.ListRecentWorkspaces()
	require.NoError(t, err)
	require.NotEmpty(t, list)
	assert.Equal(t, "Renamed for recents", list[0].Name)
}

func TestSetWorkspaceSettings_RefusesWhenLocalSettingsAreUntrusted(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	// The folder was deleted and cloned again: its nonce no longer matches.
	require.NoError(t, app.ws.SaveLocal(app.ws.Local()))
	require.NoError(t, os.RemoveAll(filepath.Join(info.Root, ".vibe-local", "nonce")))
	require.NoError(t, app.ws.Reload())
	require.False(t, app.ws.LocalTrusted())
	_, err := app.SetWorkspaceSettings(settingsFor(info))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different folder")
}
