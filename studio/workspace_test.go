package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenWorkspace_OpensAndRecords(t *testing.T) {
	app := newBareApp(t)
	var ev eventLog
	app.emit = ev.sink
	root := newWorkspaceDir(t, "Acme Analytics")

	res, err := app.OpenWorkspace(root)
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
	require.NotNil(t, res.Info)
	assert.Equal(t, "Acme Analytics", res.Info.Name)
	assert.False(t, res.Info.ReadOnly)
	assert.NotEmpty(t, res.Info.SessionID)
	require.Len(t, res.Info.Connections, 1)
	assert.Equal(t, "local", res.Info.Connections[0].Name)
	assert.Equal(t, "ok", res.Info.Connections[0].Status)
	assert.True(t, res.Info.Connections[0].Default)
	assert.Equal(t, 1, ev.count("workspace:opened"))

	recents, err := app.ListRecentWorkspaces()
	require.NoError(t, err)
	require.Len(t, recents, 1)
	assert.Equal(t, "Acme Analytics", recents[0].Name)
	assert.False(t, recents[0].Missing)
	assert.Equal(t, res.Info.Root, recents[0].Path)

	// The hub is held while open, and released on close.
	_, err = hub.ReadHolder(res.Info.Root)
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())
	_, err = hub.ReadHolder(res.Info.Root)
	assert.ErrorIs(t, err, hub.ErrNotHeld)
	assert.Equal(t, 1, ev.count("workspace:closed"))
	assert.False(t, app.IsWorkspaceOpen())
	_, err = app.GetWorkspaceInfo()
	assert.ErrorIs(t, err, errNoWorkspace)
}

func TestOpenWorkspace_VibeFileIsImportedNotOpened(t *testing.T) {
	app := newBareApp(t)
	path := filepath.Join(t.TempDir(), "old.vibe")
	require.NoError(t, os.WriteFile(path, []byte("SQLite format 3"), 0o600))
	_, err := app.OpenWorkspace(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Import .vibe")
	assert.False(t, app.IsWorkspaceOpen())

	// A .vibe on the command line is offered to the import flow, not opened.
	app.setInitialArg(path)
	init := app.GetInitialWorkspace()
	assert.Empty(t, init.Path)
	assert.Equal(t, path, init.BundlePath)
}

func TestSetInitialArg(t *testing.T) {
	app := newBareApp(t)
	dir := t.TempDir()
	app.setInitialArg(dir)
	assert.Equal(t, dir, app.GetInitialWorkspace().Path)

	app = newBareApp(t)
	app.setInitialArg("-flag")
	assert.Equal(t, InitialWorkspace{}, app.GetInitialWorkspace())

	app.setInitialArg(filepath.Join(dir, "nope"))
	assert.Contains(t, app.GetInitialWorkspace().Message, "isn't there")
}

// #33: the launch target survives being read (a reload mid-open still finds
// it) and is spent only by MarkLaunchHandled (a reload after close shows the
// Launcher).
func TestMarkLaunchHandled_ConsumesLaunchTarget(t *testing.T) {
	app := newBareApp(t)
	dir := t.TempDir()
	app.setInitialArg(dir)

	assert.Equal(t, dir, app.GetInitialWorkspace().Path)
	assert.Equal(t, dir, app.GetInitialWorkspace().Path, "a reload before the open was handled still sees it")

	app.MarkLaunchHandled()
	assert.Equal(t, InitialWorkspace{}, app.GetInitialWorkspace(), "a reload after it was handled opens nothing")
}

func TestOpenWorkspace_PlainFolderThenCreateInPlace(t *testing.T) {
	app := newBareApp(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("mine"), 0o600))

	res, err := app.OpenWorkspace(dir)
	require.NoError(t, err)
	assert.Equal(t, "not-workspace", res.Status)
	assert.False(t, app.IsWorkspaceOpen(), "nothing is opened or written until the person confirms")
	_, statErr := os.Stat(filepath.Join(dir, workspace.ConfigFile))
	assert.True(t, os.IsNotExist(statErr))

	// Without InPlace a folder that has files is refused.
	_, err = app.CreateWorkspace(CreateWorkspaceRequest{Name: "x", Dir: dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already a folder")

	res, err = app.CreateWorkspace(CreateWorkspaceRequest{Dir: dir, InPlace: true})
	require.NoError(t, err)
	assert.Equal(t, "opened", res.Status)
	assert.Equal(t, filepath.Base(dir), res.Info.Name)
	b, err := os.ReadFile(filepath.Join(dir, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "mine", string(b), "other files are left alone")
}

func TestCreateWorkspace_TemplateAndDirCheck(t *testing.T) {
	app := newBareApp(t)
	parent := t.TempDir()

	chk, err := app.CheckWorkspaceDir(parent, "Acme Analytics")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(parent, "acme-analytics"), chk.Dir)
	assert.False(t, chk.Exists)

	res, err := app.CreateWorkspace(CreateWorkspaceRequest{Name: "Acme Analytics", ParentDir: parent, Template: "contacts"})
	require.NoError(t, err)
	assert.Empty(t, res.Warnings)
	assert.Equal(t, filepath.Join(parent, "acme-analytics"), res.Path)
	assert.Equal(t, 1, res.Info.Counts.Notebooks)
	assert.Greater(t, res.Info.Counts.Tables, 0)
	_, err = os.Stat(filepath.Join(res.Path, "data", "local.duckdb"))
	require.NoError(t, err)

	chk, err = app.CheckWorkspaceDir(parent, "Acme Analytics")
	require.NoError(t, err)
	assert.True(t, chk.Exists)
	assert.True(t, chk.NotEmpty)

	_, err = app.CreateWorkspace(CreateWorkspaceRequest{Name: "Acme Analytics", ParentDir: parent})
	require.Error(t, err, "an existing, non-empty folder is refused")

	_, err = app.CreateWorkspace(CreateWorkspaceRequest{Name: "Bad", ParentDir: t.TempDir(), Template: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown template")
}

func TestSwitchWorkspace_NoLostEditsAndReleasesHub(t *testing.T) {
	app := newBareApp(t)
	a := newWorkspaceDir(t, "Alpha")
	b := newWorkspaceDir(t, "Beta")

	resA, err := app.OpenWorkspace(a)
	require.NoError(t, err)
	_, err = app.ExecuteQuery("CREATE TABLE only_in_a (id INTEGER)")
	require.NoError(t, err)
	nb, err := app.CreateNotebook("Alpha notebook")
	require.NoError(t, err)

	resB, err := app.OpenWorkspace(b)
	require.NoError(t, err)
	assert.NotEqual(t, resA.Info.SessionID, resB.Info.SessionID)
	_, err = hub.ReadHolder(resA.Info.Root)
	assert.ErrorIs(t, err, hub.ErrNotHeld, "the old workspace's hub is released before the new one opens")
	names, err := app.ListTables()
	require.NoError(t, err)
	assert.NotContains(t, names, "only_in_a")
	_, err = app.GetNotebook(nb.ID)
	assert.Error(t, err, "the other workspace's notebook is not here")

	// A save stamped with the old session is refused, never lands in Beta.
	_, err = app.SaveNotebook(map[string]any{"id": nb.ID, "title": "late", "cells": []any{}}, resA.Info.SessionID)
	require.Error(t, err)

	_, err = app.OpenWorkspace(a)
	require.NoError(t, err)
	names, err = app.ListTables()
	require.NoError(t, err)
	assert.Contains(t, names, "only_in_a", "data written straight to the source survives a switch")
	got, err := app.GetNotebook(nb.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alpha notebook", got.Title)
}

func TestOpenWorkspace_HeldElsewhereIsReadOnlyAndNotNotebookBlocking(t *testing.T) {
	app := newBareApp(t)
	var ev eventLog
	app.emit = ev.sink
	root := newWorkspaceDir(t, "Shared")

	ws, err := workspace.Open(root)
	require.NoError(t, err)
	other, err := hub.AcquireHub(ws, "vibe mcp serve")
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Release(); _ = ws.Close() })

	res, err := app.OpenWorkspace(root)
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
	assert.True(t, res.Info.ReadOnly)
	require.NotNil(t, res.Info.Holder)
	assert.Equal(t, "vibe mcp serve", res.Info.Holder.App)
	assert.Equal(t, os.Getpid(), res.Info.Holder.PID)
	assert.Equal(t, 1, ev.count("workspace:locked"))
	assert.Equal(t, "read-only", res.Info.Policy.Sandbox, "a window that does not own the database cannot change it")

	// The local DuckDB file is locked by the owner, and is reported as such.
	assert.NotEqual(t, "ok", res.Info.Connections[0].Status)

	// Notebooks are plain files and stay editable.
	nb, err := app.CreateNotebook("Still editable")
	require.NoError(t, err)
	_, err = app.SaveNotebook(map[string]any{"id": nb.ID, "title": "Edited", "cells": []any{}}, res.Info.SessionID)
	require.NoError(t, err)

	// MCP cannot start without the hub.
	_, err = app.StartMCP()
	require.Error(t, err)
	assert.False(t, app.GetMCPStatus().CanStart)

	// Take over: this process is the holder, which cannot be asked to release
	// itself, so the error is plain and the window stays read-only.
	_, err = app.TakeOverWorkspace()
	require.Error(t, err)
	info, err := app.GetWorkspaceInfo()
	require.NoError(t, err)
	assert.True(t, info.ReadOnly, "a failed take-over leaves the workspace open read-only")
}

func TestOpenWorkspaceReadOnly_NeverTakesHub(t *testing.T) {
	app := newBareApp(t)
	root := newWorkspaceDir(t, "RO")
	res, err := app.OpenWorkspaceReadOnly(root)
	require.NoError(t, err)
	assert.True(t, res.Info.ReadOnly)
	_, err = hub.ReadHolder(root)
	assert.ErrorIs(t, err, hub.ErrNotHeld)
}

func TestReleaseRequest_ClosesWorkspaceAndReleasesHub(t *testing.T) {
	app := newBareApp(t)
	var ev eventLog
	app.emit = ev.sink
	root := newWorkspaceDir(t, "Released")
	res, err := app.OpenWorkspace(root)
	require.NoError(t, err)

	app.onReleaseRequested()
	assert.Equal(t, 1, ev.count("workspace:releasing"))
	assert.Equal(t, 1, ev.count("workspace:closed"))
	assert.False(t, app.IsWorkspaceOpen())
	_, err = hub.ReadHolder(res.Info.Root)
	assert.ErrorIs(t, err, hub.ErrNotHeld)

	// It can be opened again afterwards (the one releasable hold was freed).
	_, err = app.OpenWorkspace(root)
	require.NoError(t, err)
}

func TestConflict_NotebookResolveActions(t *testing.T) {
	app := newOpenApp(t)
	nb, err := app.CreateNotebook("Weekly revenue")
	require.NoError(t, err)
	info, _ := app.GetWorkspaceInfo()
	loaded, err := app.GetNotebook(nb.ID)
	require.NoError(t, err)

	// Something else saves the file behind Studio's back.
	path := filepath.Join(info.Root, "notebooks", nb.ID+".vnb")
	disk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(disk), "Weekly revenue", "Changed by git pull", 1)), 0o600))

	local := map[string]any{"id": nb.ID, "rev": loaded.Rev, "title": "My edit", "cells": []any{}}
	_, err = app.SaveNotebook(local, info.SessionID)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix), "conflicts are marked for the UI: %v", err)

	// Reload keeps the disk version and reports its revision.
	res, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: nb.ID, Action: "reload"})
	require.NoError(t, err)
	assert.Equal(t, "Changed by git pull", res.Title)
	assert.NotEqual(t, loaded.Rev, res.Rev)

	// Save as copy writes the local edit as a new notebook and leaves the disk one.
	cp, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: nb.ID, Action: "saveCopy", Data: local})
	require.NoError(t, err)
	assert.Equal(t, "My edit (copy)", cp.Title)
	assert.NotEqual(t, nb.ID, cp.ID)
	still, err := app.GetNotebook(nb.ID)
	require.NoError(t, err)
	assert.Equal(t, "Changed by git pull", still.Title)

	// Overwrite replaces the disk version with the local edit.
	ow, err := app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: nb.ID, Action: "overwrite", Data: local})
	require.NoError(t, err)
	assert.Equal(t, "My edit", ow.Title)
	got, err := app.GetNotebook(nb.ID)
	require.NoError(t, err)
	assert.Equal(t, "My edit", got.Title)
}

func TestConflict_QueryAndSkill(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()

	q, err := app.SaveQueryOn(SaveQueryRequest{Title: "Orders", SQL: "SELECT 1"})
	require.NoError(t, err)
	qpath := filepath.Join(info.Root, "queries", q.ID+".sql")
	body, err := os.ReadFile(qpath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(qpath, append(body, []byte("\n-- edited elsewhere\n")...), 0o600))
	_, err = app.SaveQueryOn(SaveQueryRequest{ID: q.ID, Title: "Orders", SQL: "SELECT 2", Rev: q.Rev})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	cp, err := app.ResolveConflict(ConflictRequest{Kind: "query", ID: q.ID, Action: "saveCopy",
		Data: map[string]any{"title": "Orders", "sql": "SELECT 2"}})
	require.NoError(t, err)
	assert.Equal(t, "Orders (copy)", cp.Title)
	_, err = app.ResolveConflict(ConflictRequest{Kind: "query", ID: q.ID, Action: "overwrite",
		Data: map[string]any{"title": "Orders", "sql": "SELECT 3"}})
	require.NoError(t, err)
	got, err := app.GetQuery(q.ID)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 3", got.SQL)

	require.NoError(t, app.SaveSkill("my-skill", "desc", "do things", "", ""))
	skills, err := app.ListSkills()
	require.NoError(t, err)
	var rev string
	for _, s := range skills {
		if s.Name == "my-skill" {
			rev = s.Rev
		}
	}
	require.NotEmpty(t, rev)
	spath := filepath.Join(info.Root, "skills", "my-skill", "SKILL.md")
	sb, err := os.ReadFile(spath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spath, append(sb, []byte("\nextra line\n")...), 0o600))
	err = app.SaveSkill("my-skill", "desc", "mine", "", rev)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	_, err = app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "my-skill", Action: "overwrite",
		Data: map[string]any{"description": "desc", "instructions": "mine"}})
	require.NoError(t, err)
	_, err = app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "my-skill", Action: "saveCopy",
		Data: map[string]any{"description": "desc", "instructions": "mine"}})
	require.NoError(t, err)
}

func TestConflict_SkillKeepMineWithBannerRev(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	require.NoError(t, app.SaveSkill("rules", "desc", "do things", "", ""))
	diskRev := func() string {
		all, err := app.ListSkills()
		require.NoError(t, err)
		for _, s := range all {
			if s.Name == "rules" {
				return s.Rev
			}
		}
		t.Fatal("skill missing")
		return ""
	}
	spath := filepath.Join(info.Root, "skills", "rules", "SKILL.md")
	edit := func(line string) {
		b, err := os.ReadFile(spath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(spath, append(b, []byte(line)...), 0o600))
	}
	mine := map[string]any{"description": "desc", "instructions": "mine"}

	edit("\nfirst outside edit\n")
	bannerRev := diskRev() // what the banner was raised against
	edit("\nsecond outside edit\n")
	_, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "overwrite", Rev: bannerRev, Data: mine})
	require.Error(t, err, "a second outside edit must not be overwritten")
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	b, err := os.ReadFile(spath)
	require.NoError(t, err)
	assert.Contains(t, string(b), "second outside edit")

	// The banner's revision still matches: Keep mine goes through.
	_, err = app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "overwrite", Rev: diskRev(), Data: mine})
	require.NoError(t, err)
	b, err = os.ReadFile(spath)
	require.NoError(t, err)
	assert.Contains(t, string(b), "mine")
}

func TestConflict_SkillWriteBackAfterDeleteOnDisk(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	require.NoError(t, app.SaveSkill("gone", "desc", "original", "", ""))
	spath := filepath.Join(info.Root, "skills", "gone", "SKILL.md")
	require.NoError(t, os.Remove(spath))
	_, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "gone", Action: "overwrite",
		Data: map[string]any{"description": "desc", "instructions": "mine"}})
	require.NoError(t, err, "Write it back must recreate the deleted skill")
	b, err := os.ReadFile(spath)
	require.NoError(t, err)
	assert.Contains(t, string(b), "mine")
}

func TestWatcher_ExternalEditEmitsChangedOnDisk(t *testing.T) {
	app := newBareApp(t)
	var ev eventLog
	app.emit = ev.sink
	res, err := app.OpenWorkspace(newWorkspaceDir(t, "Watched"))
	require.NoError(t, err)
	nb, err := app.CreateNotebook("Watched notebook")
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond) // let the watcher settle
	path := filepath.Join(res.Info.Root, "notebooks", nb.ID+".vnb")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(b), "Watched notebook", "Edited outside", 1)), 0o600))

	require.Eventually(t, func() bool { return ev.count("workspace:changed-on-disk") > 0 }, 5*time.Second, 25*time.Millisecond)
	ev.mu.Lock()
	payload, _ := ev.data["workspace:changed-on-disk"][0].(map[string]any)
	ev.mu.Unlock()
	assert.Equal(t, "notebook", payload["kind"])
	assert.Equal(t, nb.ID, payload["id"])
}

func TestMCP_StartStopStatusAndClaudeConfigHasNoToken(t *testing.T) {
	app := newOpenApp(t)
	app.locateVibe = func() (string, error) { return "/usr/local/bin/vibe", nil }
	root := app.ws.Root()

	st := app.GetMCPStatus()
	assert.False(t, st.Running)
	assert.True(t, st.CanStart)

	st, err := app.StartMCP()
	require.NoError(t, err)
	assert.True(t, st.Running)
	assert.True(t, strings.HasPrefix(st.URL, "http://127.0.0.1:"))
	assert.True(t, strings.HasSuffix(st.URL, "/sse"))

	holder, err := hub.ReadHolder(root)
	require.NoError(t, err)
	require.NotNil(t, holder.MCP)
	token := holder.MCP.Token
	require.NotEmpty(t, token)

	// The status binding never carries the token.
	raw, err := json.Marshal(app.GetMCPStatus())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), token)

	// Neither does the Claude config.
	cfg, err := app.GetClaudeConfig()
	require.NoError(t, err)
	assert.NotContains(t, cfg.Desktop, token)
	assert.NotContains(t, cfg.Code, token)
	assert.Contains(t, cfg.Desktop, "--workspace")
	assert.Contains(t, cfg.Desktop, root)
	assert.Contains(t, cfg.Code, "claude mcp add")
	info, err := app.GetWorkspaceInfo()
	require.NoError(t, err)
	rawInfo, _ := json.Marshal(info)
	assert.NotContains(t, string(rawInfo), token)

	require.NoError(t, app.StopMCP())
	assert.False(t, app.GetMCPStatus().Running)
	holder, err = hub.ReadHolder(root)
	require.NoError(t, err)
	assert.Nil(t, holder.MCP, "MCP off clears the endpoint from hub.lock")
}

func TestClose_StopsMCPBeforeReleasingHub(t *testing.T) {
	app := newOpenApp(t)
	root := app.ws.Root()
	_, err := app.StartMCP()
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())
	_, err = hub.ReadHolder(root)
	assert.ErrorIs(t, err, hub.ErrNotHeld)
	assert.False(t, app.GetMCPStatus().Running)
}

func TestSetWorkspaceSettings_TightenOnlyAndLocalLayer(t *testing.T) {
	app := newOpenApp(t)
	info, err := app.GetWorkspaceInfo()
	require.NoError(t, err)
	assert.Equal(t, "read-write", info.Policy.Sandbox)
	assert.True(t, info.Policy.AskBeforeWrites)

	// A team limit of read-only caps this person's own choice.
	_, err = app.SetWorkspaceSettings(WorkspaceSettings{
		Name: "Renamed", DefaultConnection: "local",
		TeamSandbox: "read-only", TeamAskBeforeChanges: true,
		Sandbox: "full", AskBeforeChanges: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't go above")

	// The gate cannot be loosened when the team requires it.
	_, err = app.SetWorkspaceSettings(WorkspaceSettings{
		Name: "Renamed", DefaultConnection: "local",
		TeamSandbox: "read-write", TeamAskBeforeChanges: true,
		Sandbox: "read-write", AskBeforeChanges: false,
	})
	require.Error(t, err)

	got, err := app.SetWorkspaceSettings(WorkspaceSettings{
		Name: "Renamed", DefaultConnection: "local",
		TeamSandbox: "read-write", TeamAskBeforeChanges: false,
		Sandbox: "read-only", AskBeforeChanges: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	assert.Equal(t, "read-only", got.Policy.Sandbox)
	assert.False(t, got.Policy.AskBeforeWrites, "this person turned the gate off, which the team does not forbid")
	assert.Equal(t, "read-write", got.Team.Sandbox)

	// The committed file carries the team limit; the loosening is not in the folder.
	yml, err := os.ReadFile(filepath.Join(got.Root, workspace.ConfigFile))
	require.NoError(t, err)
	assert.Contains(t, string(yml), "sandbox: read-write")
	assert.NotContains(t, string(yml), "ask_before_writes: false")

	_, err = app.SetWorkspaceSettings(WorkspaceSettings{Name: "x", DefaultConnection: "nope", Sandbox: "read-write", AskBeforeChanges: true})
	require.Error(t, err)

	// Effective policy reaches the agent handler.
	app.mu.RLock()
	h := app.newAgentToolHandler("full")
	pol := app.agentPolicy()
	app.mu.RUnlock()
	assert.NotNil(t, h)
	assert.Equal(t, "read-only", string(pol.Mode))
}

func TestDuplicateID_DetectedAndRegenerated(t *testing.T) {
	app := newBareApp(t)
	orig := newWorkspaceDir(t, "Original")
	_, err := app.OpenWorkspace(orig)
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())

	// cp -r: the copy carries the same id.
	cp := filepath.Join(t.TempDir(), "copy")
	require.NoError(t, os.MkdirAll(cp, 0o755))
	b, err := os.ReadFile(filepath.Join(orig, workspace.ConfigFile))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cp, workspace.ConfigFile), b, 0o600))

	res, err := app.OpenWorkspace(cp)
	require.NoError(t, err)
	require.Len(t, res.Info.DuplicateIDs, 1)
	oldID := res.Info.ID

	fixed, err := app.RegenerateWorkspaceID()
	require.NoError(t, err)
	assert.Empty(t, fixed.DuplicateIDs)
	assert.NotEqual(t, oldID, fixed.ID)
}

func TestRecents_RemoveRestoreRelocateAndMissing(t *testing.T) {
	app := newBareApp(t)
	a := newWorkspaceDir(t, "One")
	_, err := app.OpenWorkspace(a)
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())

	removed, err := app.RemoveRecentWorkspace(a)
	require.NoError(t, err)
	require.NotNil(t, removed)
	list, _ := app.ListRecentWorkspaces()
	assert.Empty(t, list)
	require.NoError(t, app.RestoreRecentWorkspace(*removed))
	list, _ = app.ListRecentWorkspaces()
	require.Len(t, list, 1)

	// Move the folder: the entry shows as missing, and Locate re-points it.
	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(a, moved))
	list, _ = app.ListRecentWorkspaces()
	require.Len(t, list, 1)
	assert.True(t, list[0].Missing)
	require.NoError(t, app.RelocateRecentWorkspace(a, moved))
	list, _ = app.ListRecentWorkspaces()
	assert.False(t, list[0].Missing)

	require.Error(t, app.RelocateRecentWorkspace(moved, t.TempDir()), "a folder without vibe.yaml cannot be located to")
}

func TestSetPreferences_KeepsRecentList(t *testing.T) {
	app := newBareApp(t)
	_, err := app.OpenWorkspace(newWorkspaceDir(t, "Keep me"))
	require.NoError(t, err)
	p, err := app.GetPreferences()
	require.NoError(t, err)
	// The frontend round-trips only the fields it knows.
	require.NoError(t, app.SetPreferences(prefsWithoutExtra(p)))
	list, err := app.ListRecentWorkspaces()
	require.NoError(t, err)
	assert.Len(t, list, 1, "saving the theme must not drop the recent list")
}

func TestListCatalogGroups_AndDashboardLocalTables(t *testing.T) {
	app := newOpenApp(t)
	_, err := app.ExecuteQuery("CREATE TABLE people (id INTEGER PRIMARY KEY, name VARCHAR)")
	require.NoError(t, err)

	groups, err := app.ListCatalogGroups()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "local", groups[0].Connection)
	require.Len(t, groups[0].Tables, 1)
	assert.Equal(t, "local.main.people", groups[0].Tables[0].Qualified)

	d, err := app.GetDashboard()
	require.NoError(t, err)
	assert.Equal(t, "App Test", d.Title)
	assert.Equal(t, []string{"local.main.people"}, d.LocalTables)
	assert.Equal(t, 1, d.TableCount)

	// The qualified name flows straight to the datasheet.
	data, err := app.GetTableData("local.main.people", 1, 10, "", "")
	require.NoError(t, err)
	assert.Equal(t, 0, len(data.Rows))
}

func TestNoWorkspace_BindingsRefuse(t *testing.T) {
	app := newBareApp(t)
	_, err := app.ListNotebooks()
	assert.ErrorIs(t, err, errNoWorkspace)
	_, err = app.GetClaudeConfig()
	assert.ErrorIs(t, err, errNoWorkspace)
	_, err = app.StartMCP()
	assert.ErrorIs(t, err, errNoWorkspace)
	assert.NoError(t, app.CloseWorkspace())
	_ = context.Background()
}

func prefsWithoutExtra(p prefs.Preferences) prefs.Preferences {
	p.Extra = nil
	return p
}

// A repository's own config (core.fsmonitor, filters) names programs git would
// run; opening a folder must never run git at all.
func TestOpen_NeverRunsGit(t *testing.T) {
	app := newBareApp(t)
	root := newWorkspaceDir(t, "Hostile")
	marker := filepath.Join(t.TempDir(), "ran")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\n\trepositoryformatversion = 0\n\tfsmonitor = touch "+marker+"\n"), 0o600))

	res, err := app.OpenWorkspace(root)
	require.NoError(t, err)
	assert.Equal(t, "main", res.Info.GitBranch)
	_, err = app.GetWorkspaceInfo()
	require.NoError(t, err)
	_, err = app.ListRecentWorkspaces()
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "git must not run on open")
}

func TestRelease_DoesNotCloseAWorkspaceSwitchedToDuringTheGrace(t *testing.T) {
	app := newBareApp(t)
	_, err := app.OpenWorkspace(newWorkspaceDir(t, "First"))
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { app.onReleaseRequested(); close(done) }()
	time.Sleep(50 * time.Millisecond) // inside the grace window
	second := newWorkspaceDir(t, "Second")
	_, err = app.OpenWorkspace(second)
	require.NoError(t, err)
	<-done
	info, err := app.GetWorkspaceInfo()
	require.NoError(t, err, "the new workspace must stay open")
	assert.Equal(t, "Second", info.Name)
}

func TestSwitch_ToABrokenFolderKeepsTheCurrentWorkspace(t *testing.T) {
	app := newBareApp(t)
	_, err := app.OpenWorkspace(newWorkspaceDir(t, "Good"))
	require.NoError(t, err)
	bad := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bad, workspace.ConfigFile), []byte("version: [not, valid"), 0o600))
	_, err = app.OpenWorkspace(bad)
	require.Error(t, err)
	info, err := app.GetWorkspaceInfo()
	require.NoError(t, err)
	assert.Equal(t, "Good", info.Name)
	_, err = hub.ReadHolder(info.Root)
	assert.NoError(t, err, "the current workspace still holds its hub")
}

func TestSaveQueryOn_KeepsParametersAndDescription_ConnectionFollowsEditor(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	// A query written by the CLI or the agent, with a connection and a description.
	require.NoError(t, os.WriteFile(filepath.Join(info.Root, "queries", "daily.sql"),
		[]byte("-- vibe: {\"title\":\"Daily\",\"description\":\"by the agent\",\"connection\":\"local\"}\nSELECT 1\n"), 0o600))
	got, err := app.GetQuery("daily")
	require.NoError(t, err)
	require.Equal(t, "local", got.Connection)

	_, err = app.SaveQueryOn(SaveQueryRequest{ID: "daily", Title: "Daily", SQL: "SELECT 2", Rev: got.Rev, Connection: got.Connection})
	require.NoError(t, err)
	after, err := app.GetQuery("daily")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", after.SQL)
	assert.Equal(t, "local", after.Connection, "the editor sent the connection it loaded")
	assert.Equal(t, "by the agent", after.Description)

	// The editor's "Run on" choice is authoritative: a change, and a clear, are saved.
	_, err = app.SaveQueryOn(SaveQueryRequest{ID: "daily", Title: "Daily", SQL: "SELECT 2", Rev: after.Rev, Connection: "other"})
	require.NoError(t, err)
	after, _ = app.GetQuery("daily")
	assert.Equal(t, "other", after.Connection)
	assert.Equal(t, "by the agent", after.Description, "the description is kept")
	_, err = app.SaveQueryOn(SaveQueryRequest{ID: "daily", Title: "Daily", SQL: "SELECT 2", Rev: after.Rev})
	require.NoError(t, err)
	after, _ = app.GetQuery("daily")
	assert.Empty(t, after.Connection)
	require.NoError(t, os.WriteFile(filepath.Join(info.Root, "queries", "daily.sql"),
		[]byte("-- vibe: {\"title\":\"Daily\",\"description\":\"by the agent\",\"connection\":\"local\"}\nSELECT 1\n"), 0o600))

	// Keep mine / save as copy keep them too.
	_, err = app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "overwrite", Data: map[string]any{"title": "Daily", "sql": "SELECT 3"}})
	require.NoError(t, err)
	cp, err := app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "saveCopy", Data: map[string]any{"title": "Daily", "sql": "SELECT 4"}})
	require.NoError(t, err)
	c, err := app.GetQuery(cp.ID)
	require.NoError(t, err)
	assert.Equal(t, "local", c.Connection)
	assert.Equal(t, "by the agent", c.Description)

	// Conflict resolution uses the editor's "Run on" choice when it sends one.
	other := "other"
	_, err = app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "overwrite", Connection: &other,
		Data: map[string]any{"title": "Daily", "sql": "SELECT 5"}})
	require.NoError(t, err)
	kept, err := app.GetQuery("daily")
	require.NoError(t, err)
	assert.Equal(t, "other", kept.Connection, "Keep mine writes the editor's connection, not the one on disk")
	assert.Equal(t, "SELECT 5", kept.SQL)
	empty := ""
	cp2, err := app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "saveCopy", Connection: &empty,
		Data: map[string]any{"title": "Daily", "sql": "SELECT 6"}})
	require.NoError(t, err)
	c2, _ := app.GetQuery(cp2.ID)
	assert.Empty(t, c2.Connection, "an empty choice is the workspace default")
	bad := `x"; DROP`
	_, err = app.ResolveConflict(ConflictRequest{Kind: "query", ID: "daily", Action: "overwrite", Connection: &bad, Data: map[string]any{"title": "Daily", "sql": "SELECT 7"}})
	require.Error(t, err)
}

func TestSetPreferences_AlwaysKeepsRecentsFromDisk(t *testing.T) {
	app := newBareApp(t)
	_, err := app.OpenWorkspace(newWorkspaceDir(t, "One"))
	require.NoError(t, err)
	stale, err := app.GetPreferences() // a copy the frontend holds
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())
	_, err = app.OpenWorkspace(newWorkspaceDir(t, "Two"))
	require.NoError(t, err)

	require.NoError(t, app.SetPreferences(stale)) // a theme change with the stale copy, recents key included
	list, err := app.ListRecentWorkspaces()
	require.NoError(t, err)
	assert.Len(t, list, 2, "a theme change must not roll the list back")
}

func TestKeepMine_RefusesWhenTheFileChangedAgain(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	nb, err := app.CreateNotebook("N")
	require.NoError(t, err)
	disk, err := app.GetNotebook(nb.ID)
	require.NoError(t, err)
	raised := disk.Rev
	path := filepath.Join(info.Root, "notebooks", nb.ID+".vnb")
	b, _ := os.ReadFile(path)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(b), `"N"`, `"Changed again"`, 1)), 0o600))

	_, err = app.ResolveConflict(ConflictRequest{Kind: "notebook", ID: nb.ID, Action: "overwrite", Rev: raised,
		Data: map[string]any{"id": nb.ID, "title": "Mine", "cells": []any{}}})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	still, _ := app.GetNotebook(nb.ID)
	assert.Equal(t, "Changed again", still.Title)
}

func TestSkillSaveCopy_NeverOverwritesAnEarlierCopy(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SaveSkill("rules", "d", "original", "", ""))
	data := map[string]any{"description": "d", "instructions": "mine one"}
	first, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "saveCopy", Data: data})
	require.NoError(t, err)
	data2 := map[string]any{"description": "d", "instructions": "mine two"}
	second, err := app.ResolveConflict(ConflictRequest{Kind: "skill", ID: "rules", Action: "saveCopy", Data: data2})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)
	s1, err := app.ws.Skills().Get(context.Background(), first.ID)
	require.NoError(t, err)
	assert.Contains(t, s1.Instructions, "mine one")
}

func TestSetWorkspaceSettings_RefusesAStaleTeamForm(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	// vibe.yaml changed since the form loaded (a git pull set a team limit).
	cfg := app.ws.Config()
	cfg.Sandbox = "read-only"
	require.NoError(t, app.ws.SaveConfig(cfg))
	loaded := ""
	loadedAsk := false
	_, err := app.SetWorkspaceSettings(WorkspaceSettings{
		Name: info.Name, DefaultConnection: "local", TeamSandbox: "", Sandbox: "read-write", AskBeforeChanges: true,
		LoadedTeamSandbox: &loaded, LoadedTeamAsk: &loadedAsk,
	})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	assert.Equal(t, "read-only", app.ws.Config().Sandbox, "the team's change is not overwritten")
}

func TestQueryKeepMine_RefusesWhenTheFileChangedAgain(t *testing.T) {
	app := newOpenApp(t)
	info, _ := app.GetWorkspaceInfo()
	q, err := app.SaveQueryOn(SaveQueryRequest{Title: "Orders", SQL: "SELECT 1"})
	require.NoError(t, err)
	raised, err := app.GetQuery(q.ID)
	require.NoError(t, err)
	path := filepath.Join(info.Root, "queries", q.ID+".sql")
	b, _ := os.ReadFile(path)
	require.NoError(t, os.WriteFile(path, append(b, []byte("-- changed again\n")...), 0o600))
	_, err = app.ResolveConflict(ConflictRequest{Kind: "query", ID: q.ID, Action: "overwrite", Rev: raised.Rev,
		Data: map[string]any{"title": "Orders", "sql": "SELECT 2"}})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	got, _ := app.GetQuery(q.ID)
	assert.Contains(t, got.SQL, "SELECT 1", "nothing was overwritten")
}

func TestLaunchArg_FileURIsAndKinds(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "my work")
	require.NoError(t, os.Mkdir(spaced, 0o755))
	bundle := filepath.Join(dir, "x.VIBE")
	require.NoError(t, os.WriteFile(bundle, []byte("x"), 0o600))
	plain := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(plain, []byte("x"), 0o600))

	// %U from a .desktop entry: file:// URIs, percent-encoded.
	assert.Equal(t, spaced, classifyLaunchArg("file://"+(&url.URL{Path: spaced}).EscapedPath()).Path)
	assert.Equal(t, bundle, classifyLaunchArg("file://"+(&url.URL{Path: bundle}).EscapedPath()).BundlePath)
	// A folder named like a bundle is still a workspace.
	odd := filepath.Join(dir, "x.vibe")
	require.NoError(t, os.Mkdir(odd, 0o755))
	assert.Equal(t, odd, classifyLaunchArg(odd).Path)
	assert.Empty(t, classifyLaunchArg(odd).BundlePath)
	// A directory is a workspace, a .vibe is a bundle, anything else is a notice.
	assert.Equal(t, spaced, classifyLaunchArg(spaced).Path)
	assert.Equal(t, bundle, classifyLaunchArg(bundle).BundlePath)
	r := classifyLaunchArg(plain)
	assert.Empty(t, r.Path)
	assert.Empty(t, r.BundlePath)
	assert.Contains(t, r.Message, "is a file, not a folder")
	assert.Contains(t, classifyLaunchArg(filepath.Join(dir, "gone")).Message, "isn't there")
	assert.Equal(t, InitialWorkspace{}, classifyLaunchArg("--gui"))
	assert.Equal(t, InitialWorkspace{}, classifyLaunchArg(""))
}

func TestHandleOpenPath_QueuesBeforeReadyThenEmits(t *testing.T) {
	app := newBareApp(t)
	var events []string
	var payloads []InitialWorkspace
	app.emit = func(event string, data ...any) {
		events = append(events, event)
		p, ok := data[0].(InitialWorkspace)
		require.True(t, ok)
		payloads = append(payloads, p)
	}
	dir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "b.vibe")
	require.NoError(t, os.WriteFile(bundle, []byte("x"), 0o600))

	// Before the frontend asks: queued, no event.
	app.handleOpenPath(dir)
	assert.Empty(t, events)
	assert.Equal(t, dir, app.GetInitialWorkspace().Path)

	// After: an event, and the queue is untouched.
	app.handleOpenPath(bundle)
	require.Equal(t, []string{"app:open-path"}, events)
	assert.Equal(t, bundle, payloads[0].BundlePath)
	assert.Equal(t, dir, app.GetInitialWorkspace().Path)

	// A notice is delivered too, and an empty path is ignored.
	app.handleOpenPath(filepath.Join(dir, "gone"))
	app.handleOpenPath("")
	require.Len(t, events, 2)
	assert.Contains(t, payloads[1].Message, "isn't there")
}

func TestResolveLaunch(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	home := filepath.Join(base, "home")
	ws := newWorkspaceDirAt(t, filepath.Join(home, "proj"))
	sub := filepath.Join(ws, "notebooks")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	plain := filepath.Join(home, "plain")
	require.NoError(t, os.MkdirAll(plain, 0o755))

	t.Run("cwd is home: launcher", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{}, resolveLaunch(nil, home, home))
	})
	t.Run("cwd is /: launcher", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{}, resolveLaunch(nil, string(filepath.Separator), home))
	})
	t.Run("cwd empty: launcher", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{}, resolveLaunch(nil, "", home))
	})
	t.Run("stray vibe.yaml in home: launcher", func(t *testing.T) {
		h2 := filepath.Join(base, "home2")
		newWorkspaceDirAt(t, h2)
		assert.Equal(t, InitialWorkspace{}, resolveLaunch(nil, h2, h2))
		assert.Equal(t, InitialWorkspace{}, resolveLaunch(nil, filepath.Join(h2, "x"), h2))
	})
	t.Run("cwd inside a workspace", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{Path: ws}, resolveLaunch(nil, ws, home))
		assert.Equal(t, InitialWorkspace{Path: ws}, resolveLaunch(nil, sub, home))
	})
	t.Run("cwd in a non-workspace folder: launcher", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{}, resolveLaunch(nil, plain, home))
	})
	t.Run("explicit workspace path beats cwd", func(t *testing.T) {
		other := newWorkspaceDirAt(t, filepath.Join(home, "other"))
		assert.Equal(t, InitialWorkspace{Path: other}, resolveLaunch([]string{other}, ws, home))
	})
	t.Run("explicit path inside a workspace resolves to its root", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{Path: ws}, resolveLaunch([]string{sub}, home, home))
	})
	t.Run("explicit non-workspace folder passes through", func(t *testing.T) {
		assert.Equal(t, InitialWorkspace{Path: plain}, resolveLaunch([]string{plain}, ws, home))
	})
	t.Run("explicit bundle, missing path, flag", func(t *testing.T) {
		b := filepath.Join(home, "x.vibe")
		require.NoError(t, os.WriteFile(b, []byte("z"), 0o644))
		assert.Equal(t, InitialWorkspace{BundlePath: b}, resolveLaunch([]string{b}, ws, home))
		r := resolveLaunch([]string{filepath.Join(home, "gone")}, ws, home)
		assert.Contains(t, r.Message, "isn't there")
		assert.Equal(t, InitialWorkspace{Path: ws}, resolveLaunch([]string{"--flag"}, ws, home))
		assert.Equal(t, InitialWorkspace{Path: ws}, resolveLaunch([]string{"--flag", plain}, ws, home), "only the first argument counts")
	})
}

// newWorkspaceDirAt makes dir look like a workspace (a vibe.yaml is all
// discovery looks for).
func newWorkspaceDirAt(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, workspace.ConfigFile), []byte("id: x\nname: x\n"), 0o644))
	return dir
}

func TestConflictErrBridgeShape(t *testing.T) {
	wrapped := fmt.Errorf("failed to save: %w", &filestore.ConflictError{Path: "notebooks/a.vnb"})
	err := conflictErr(wrapped)
	require.ErrorIs(t, err, filestore.ErrConflict, "the bridge error is still a conflict to Go callers")
	assert.True(t, strings.HasPrefix(err.Error(), conflictPrefix))
	assert.Contains(t, err.Error(), "changed on disk")

	other := errors.New("disk full")
	assert.Equal(t, other, conflictErr(other), "non-conflicts pass through")
	assert.NoError(t, conflictErr(nil))
}
