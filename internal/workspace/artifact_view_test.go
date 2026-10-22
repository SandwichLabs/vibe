package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

const viewScriptManifest = "version: 1\ntitle: T\nview: script\nqueries:\n  - name: q\n    sql: SELECT 1\n"
const viewJS = "function draw(ctx) { return '<svg xmlns=\"http://www.w3.org/2000/svg\"/>'; }\n"

// scriptArtifact writes a view: script artifact through the store.
func scriptArtifact(t *testing.T, ws Workspace, name, manifest, js string) {
	t.Helper()
	a := &artifacts.Artifact{Name: name, ManifestText: []byte(manifest), View: []byte(js)}
	require.NoError(t, ws.Artifacts().Put(context.Background(), a, artifacts.Overwrite()))
}

// impl is the concrete workspace behind the interface, for the tests that look inside.
func impl(t *testing.T, ws Workspace) *workspace {
	t.Helper()
	w, ok := ws.(*workspace)
	require.True(t, ok)
	return w
}

func viewState(t *testing.T, ws Workspace, name string) ArtifactViewStatus {
	t.Helper()
	st, err := ws.ArtifactViewState(name)
	require.NoError(t, err)
	return st
}

func TestArtifactView_AllowThenAnyByteChangeAsksAgain(t *testing.T) {
	for _, file := range []string{artifacts.ManifestFile, artifacts.ScriptFile} {
		t.Run(file, func(t *testing.T) {
			ws, _, _ := memWorkspace(t, nil)
			scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
			st := viewState(t, ws, "chart")
			assert.Equal(t, "undecided", st.State)
			assert.Equal(t, "script", st.Kind)
			assert.Equal(t, viewJS, st.Source)
			require.NotEmpty(t, st.Hash)

			require.NoError(t, ws.AllowArtifactView("chart", st.Hash))
			assert.Equal(t, "allowed", viewState(t, ws, "chart").State)

			p := filepath.Join(ws.Root(), "artifacts", "chart", file)
			b, err := os.ReadFile(p)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(p, append(b, '\n'), 0o644)) // one byte
			after := viewState(t, ws, "chart")
			assert.Equal(t, "changed", after.State)
			assert.NotEqual(t, st.Hash, after.Hash)
		})
	}
}

func TestArtifactView_BytesMovedAcrossTheFilesAreChanged(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	tail := "# <script>steal()</script>\n"
	scriptArtifact(t, ws, "chart", viewScriptManifest+tail, "draw()\n")
	st := viewState(t, ws, "chart")
	require.NoError(t, ws.AllowArtifactView("chart", st.Hash))
	require.Equal(t, "allowed", viewState(t, ws, "chart").State)

	// The same bytes, the tail of artifact.yaml moved to the start of view.js.
	scriptArtifact(t, ws, "chart", viewScriptManifest, tail+"draw()\n")
	assert.Equal(t, "changed", viewState(t, ws, "chart").State)
}

func TestArtifactView_AllowRefusesAStaleHash(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	shown := viewState(t, ws, "chart").Hash

	// The agent (or git) rewrites the view between the prompt and the click.
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS+"fetch('x')\n")
	require.ErrorIs(t, ws.AllowArtifactView("chart", shown), ErrViewChanged)
	assert.Equal(t, "undecided", viewState(t, ws, "chart").State, "nothing was recorded")

	require.ErrorIs(t, ws.AllowArtifactView("chart", ""), ErrViewChanged, "an allow needs the hash that was shown")
	require.ErrorIs(t, ws.AllowArtifactView("chart", "not-a-hash"), ErrViewChanged)
	require.NoError(t, ws.AllowArtifactView("chart", viewState(t, ws, "chart").Hash))
}

func TestArtifactView_DenyHoldsAcrossEditsUntilRevoked(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	require.NoError(t, ws.DenyArtifactView("chart"))
	assert.Equal(t, "denied", viewState(t, ws, "chart").State)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS+"// edit\n")
	assert.Equal(t, "denied", viewState(t, ws, "chart").State)
	require.NoError(t, ws.RevokeArtifactView("chart"))
	assert.Equal(t, "undecided", viewState(t, ws, "chart").State)
}

func TestArtifactView_OnlyAScriptViewHasADecision(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	a := &artifacts.Artifact{Name: "plain", ManifestText: []byte(artifactManifest), View: []byte(artifactView)}
	require.NoError(t, ws.Artifacts().Put(context.Background(), a, artifacts.Overwrite()))
	st := viewState(t, ws, "plain")
	assert.Equal(t, "none", st.State)
	assert.Equal(t, "a2ui", st.Kind)
	assert.Empty(t, st.Hash)
	assert.Error(t, ws.DenyArtifactView("plain"))
	assert.Error(t, ws.AllowArtifactView("plain", "x"))

	_, err := ws.ArtifactViewState("../x")
	assert.Error(t, err)
	st, err = ws.ArtifactViewState("missing")
	require.NoError(t, err)
	assert.Equal(t, "none", st.State)
	assert.NotEmpty(t, st.Error)
}

func TestArtifactView_DecisionSurvivesReopen(t *testing.T) {
	ws, _, cfg := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	require.NoError(t, ws.AllowArtifactView("chart", viewState(t, ws, "chart").Hash))
	again := reopen(t, ws, cfg)
	assert.Equal(t, "allowed", viewState(t, again, "chart").State)
}

func TestArtifactView_SymlinkedWorkspacePathSharesTheDecision(t *testing.T) {
	ws, _, cfg := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	require.NoError(t, ws.AllowArtifactView("chart", viewState(t, ws, "chart").Hash))

	link := filepath.Join(t.TempDir(), "via-link")
	require.NoError(t, os.Symlink(ws.Root(), link))
	viaLink, err := Open(link, WithConfigDir(cfg), WithUserConfig(UserConfig{Version: ConfigVersion}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = viaLink.Close() })
	assert.Equal(t, ws.Root(), viaLink.Root(), "the root is the resolved path")
	assert.Equal(t, "allowed", viewState(t, viaLink, "chart").State)
}

func TestArtifactView_ADifferentFolderNeverSharesADecision(t *testing.T) {
	a, _, _ := memWorkspace(t, nil)
	b, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, a, "chart", viewScriptManifest, viewJS)
	scriptArtifact(t, b, "chart", viewScriptManifest, viewJS) // identical bytes
	assert.Equal(t, viewState(t, a, "chart").Hash, viewState(t, b, "chart").Hash)
	assert.NotEqual(t, impl(t, a).viewKey("chart"), impl(t, b).viewKey("chart"))

	// Even one decision store behind both (the worst case) keeps them apart.
	impl(t, b).vdec = impl(t, a).vdec
	require.NoError(t, a.AllowArtifactView("chart", viewState(t, a, "chart").Hash))
	assert.Equal(t, "allowed", viewState(t, a, "chart").State)
	assert.Equal(t, "undecided", viewState(t, b, "chart").State, "a clone at another path asks")

	// A copy of an allowed folder inside the same workspace under another name.
	scriptArtifact(t, a, "chart2", viewScriptManifest, viewJS)
	assert.Equal(t, "undecided", viewState(t, a, "chart2").State)
}

func TestArtifactView_DecisionsAreNeverWrittenInsideTheWorkspace(t *testing.T) {
	ws, _, cfg := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	require.NoError(t, ws.AllowArtifactView("chart", viewState(t, ws, "chart").Hash))
	require.NoError(t, ws.DenyArtifactView("chart"))

	_ = filepath.WalkDir(ws.Root(), func(p string, d os.DirEntry, _ error) error {
		assert.NotEqual(t, ViewDecisionFile, d.Name(), "views.json inside the workspace: %s", p)
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			assert.NotContains(t, string(b), `"decision"`, p)
		}
		return nil
	})
	var found string
	_ = filepath.WalkDir(cfg, func(p string, d os.DirEntry, _ error) error {
		if d.Name() == ViewDecisionFile {
			found = p
		}
		return nil
	})
	require.NotEmpty(t, found, "the decision lives in the user config directory")
	assert.Equal(t, filepath.Dir(found), filepath.Dir(impl(t, ws).localPath(impl(t, ws).cfg.ID)), "beside dotenv.json and local.yaml")
}

func TestArtifactView_DecisionsList(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "b-chart", viewScriptManifest, viewJS)
	scriptArtifact(t, ws, "a-chart", viewScriptManifest, viewJS)
	require.NoError(t, ws.AllowArtifactView("b-chart", viewState(t, ws, "b-chart").Hash))
	require.NoError(t, ws.DenyArtifactView("a-chart"))
	scriptArtifact(t, ws, "b-chart", viewScriptManifest, viewJS+"//x\n")

	got := ws.ArtifactViewDecisions()
	require.Len(t, got, 2)
	assert.Equal(t, "a-chart", got[0].Name)
	assert.Equal(t, "denied", got[0].State)
	assert.Equal(t, "b-chart", got[1].Name)
	assert.Equal(t, "changed", got[1].State)

	require.NoError(t, ws.Artifacts().Delete(context.Background(), "b-chart", artifacts.WithRev("")))
	got = ws.ArtifactViewDecisions()
	assert.Equal(t, "gone", got[1].State)
	require.NoError(t, ws.RevokeArtifactView("b-chart"), "a gone artifact's decision can still be revoked")
	assert.Len(t, ws.ArtifactViewDecisions(), 1)
}

func TestOnDiskCase(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Work", "Artifacts"), 0o755))
	assert.Equal(t, filepath.Join(dir, "Work", "Artifacts"), onDiskCase(filepath.Join(dir, "work", "artifacts")))
	assert.Equal(t, filepath.Join(dir, "Work", "Artifacts"), onDiskCase(filepath.Join(dir, "Work", "Artifacts")))
	assert.Equal(t, filepath.Join(dir, "Work", "new"), onDiskCase(filepath.Join(dir, "WORK", "new")), "a missing element keeps its spelling")
	// An exact match wins over a case-folded one (case-sensitive volumes).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "x"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "X"), 0o755))
	assert.True(t, strings.HasSuffix(onDiskCase(filepath.Join(dir, "X")), "X"))
	assert.True(t, strings.HasSuffix(onDiskCase(filepath.Join(dir, "x")), "x"))
}

func TestArtifactView_SymlinkedArtifactsFolderIsRefused(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	real := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.Rename(filepath.Join(ws.Root(), "artifacts"), real))
	require.NoError(t, os.Symlink(real, filepath.Join(ws.Root(), "artifacts")))

	st := viewState(t, ws, "chart")
	assert.Equal(t, "none", st.State)
	assert.Empty(t, st.Hash)
	assert.NotEmpty(t, st.Error)
	assert.NotContains(t, st.Error, real, "no path crosses the bridge")
	assert.ErrorIs(t, ws.AllowArtifactView("chart", "anything"), ErrNoScriptView)
	assert.ErrorIs(t, ws.DenyArtifactView("chart"), ErrNoScriptView)
}

func TestArtifactView_AllowIsRefusedWhenTheKindChangedAfterTheHashWasShown(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", viewScriptManifest, viewJS)
	shown := viewState(t, ws, "chart").Hash
	a := &artifacts.Artifact{Name: "chart", ManifestText: []byte(artifactManifest), View: []byte(artifactView)}
	require.NoError(t, ws.Artifacts().Put(context.Background(), a, artifacts.Overwrite()))
	require.Error(t, ws.AllowArtifactView("chart", shown))
	assert.Empty(t, ws.ArtifactViewDecisions(), "nothing was recorded")
}

func TestArtifactView_ErrorsCarryNoPaths(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	dir := filepath.Join(ws.Root(), "artifacts", "broken")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(viewScriptManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.js"), []byte("draw()\u202e\n"), 0o644))
	for _, name := range []string{"broken", "missing"} {
		st := viewState(t, ws, name)
		assert.NotEmpty(t, st.Error)
		assert.NotContains(t, st.Error, ws.Root())
	}
	assert.Equal(t, viewErrUnusable, viewState(t, ws, "broken").Error)
}

const viewPageManifest = "version: 1\ntitle: P\nview: html\nqueries:\n  - name: q\n    sql: SELECT 1\n"

// pageArtifact writes a view: html artifact through the store.
func pageArtifact(t *testing.T, ws Workspace, name string, files ...artifacts.PageFile) {
	t.Helper()
	a := &artifacts.Artifact{Name: name, ManifestText: []byte(viewPageManifest), Files: files}
	require.NoError(t, ws.Artifacts().Put(context.Background(), a, artifacts.Overwrite()))
}

func TestArtifactView_HTMLPageHasAFileListAndTheSameDecisionFlow(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	idx := artifacts.PageFile{Path: "index.html.tmpl", Data: []byte(`<p>{{ query "q" }}</p>`)}
	logo := artifacts.PageFile{Path: "assets/logo.png", Data: []byte("\x89PNG\r\n\x1a\n....")}
	pageArtifact(t, ws, "pg", idx, logo)

	st := viewState(t, ws, "pg")
	assert.Equal(t, "html", st.Kind)
	assert.Equal(t, "undecided", st.State)
	assert.Equal(t, "static", st.PageKind)
	assert.Equal(t, []string{"q"}, st.Queries)
	assert.Empty(t, st.Source, "a page has files, not one source")
	require.Len(t, st.Files, 2)
	assert.Equal(t, ArtifactViewFile{Path: "assets/logo.png", Role: "image", Size: len(logo.Data)}, st.Files[0], "a binary file has no text")
	assert.Equal(t, ArtifactViewFile{Path: "index.html.tmpl", Role: "entry", Size: len(idx.Data), Text: string(idx.Data)}, st.Files[1])
	require.Len(t, st.PageFiles, 2)

	// The hash is PageHash over exactly these bytes.
	a, err := artifacts.PageFromText("pg", []byte(st.Manifest), st.PageFiles)
	require.NoError(t, err)
	assert.Equal(t, a.Page.Hash, st.Hash)

	require.NoError(t, ws.AllowArtifactView("pg", st.Hash))
	assert.Equal(t, "allowed", viewState(t, ws, "pg").State)

	// Any file's one byte, or a file added, asks again.
	pageArtifact(t, ws, "pg", artifacts.PageFile{Path: idx.Path, Data: append(idx.Data, ' ')}, logo)
	assert.Equal(t, "changed", viewState(t, ws, "pg").State)
	require.NoError(t, ws.DenyArtifactView("pg"))
	assert.Equal(t, "denied", viewState(t, ws, "pg").State)
	d := ws.ArtifactViewDecisions()
	require.Len(t, d, 1)
	assert.Equal(t, "pg", d[0].Name)
	assert.Equal(t, "denied", d[0].State)
	require.NoError(t, ws.RevokeArtifactView("pg"))
	assert.Empty(t, ws.ArtifactViewDecisions())
}

func TestArtifactView_APageThatFailsTheVetIsUnusableNotAllowable(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	dir := filepath.Join(ws.Root(), "artifacts", "bad")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "html"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(viewPageManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(`<a href="http://x.test/">x</a>`), 0o644))
	st := viewState(t, ws, "bad")
	assert.NotEmpty(t, st.Error)
	assert.Empty(t, st.Hash)
	assert.NotContains(t, st.Error, ws.Root(), "a fixed text, not the path")
	assert.Error(t, ws.AllowArtifactView("bad", "anything"))
}

const askPageManifest = viewPageManifest + "html:\n  capabilities:\n    ask: {}\n    state: {scope: local}\n"

func askPage(t *testing.T, ws Workspace, name, js string) {
	t.Helper()
	a := &artifacts.Artifact{Name: name, ManifestText: []byte(askPageManifest), Files: []artifacts.PageFile{
		{Path: "index.html.tmpl", Data: []byte(`<p>{{ query "q" }}</p><script src="app.js"></script>`)},
		{Path: "app.js", Data: []byte(js)},
	}}
	require.NoError(t, ws.Artifacts().Put(context.Background(), a, artifacts.Overwrite()))
}

func TestPageAsk_OnAtAllowRevocableSeparatelyAndOffAfterAnEdit(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	askPage(t, ws, "pg", "vibe.use('ask')")
	st := viewState(t, ws, "pg")
	assert.False(t, ws.PageAskEnabled("pg", st.Hash), "not before the page is allowed")
	require.NoError(t, ws.AllowArtifactView("pg", st.Hash))
	assert.True(t, ws.PageAskEnabled("pg", st.Hash), "on by default at allow time")
	d := ws.ArtifactViewDecisions()
	require.Len(t, d, 1)
	assert.True(t, d[0].DeclaresAsk)
	assert.True(t, d[0].AskOn)

	require.NoError(t, ws.SetPageAsk("pg", false))
	assert.False(t, ws.PageAskEnabled("pg", st.Hash))
	assert.Equal(t, "allowed", viewState(t, ws, "pg").State, "the page itself stays allowed")

	// An edit and a new allow do not turn a person's "off" back on.
	askPage(t, ws, "pg", "vibe.use('ask'); 1")
	st2 := viewState(t, ws, "pg")
	require.NoError(t, ws.AllowArtifactView("pg", st2.Hash))
	assert.False(t, ws.PageAskEnabled("pg", st2.Hash))
	require.NoError(t, ws.SetPageAsk("pg", true))
	assert.True(t, ws.PageAskEnabled("pg", st2.Hash))
	// An edit alone ends it, since the hash moved.
	askPage(t, ws, "pg", "vibe.use('ask'); 2")
	assert.False(t, ws.PageAskEnabled("pg", viewState(t, ws, "pg").Hash))
}

func TestPageAsk_OnlyForAScriptedPageThatDeclaresIt(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	pageArtifact(t, ws, "static", artifacts.PageFile{Path: "index.html.tmpl", Data: []byte(`<p>{{ query "q" }}</p>`)})
	assert.ErrorIs(t, ws.SetPageAsk("static", true), ErrNoScriptView)
	st := viewState(t, ws, "static")
	require.NoError(t, ws.AllowArtifactView("static", st.Hash))
	assert.False(t, ws.PageAskEnabled("static", st.Hash))
	assert.False(t, ws.PageAskEnabled("../x", st.Hash))
}

func TestArtifactViewStatus_PageQueriesAndDecisionsCarryTheKind(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	askPage(t, ws, "pg", "vibe.use('ask')")
	st := viewState(t, ws, "pg")
	require.Len(t, st.QueryUses, 1)
	assert.Equal(t, ArtifactViewQuery{Name: "q", Used: true}, st.QueryUses[0], "the default database has an empty connection")
	require.NoError(t, ws.AllowArtifactView("pg", st.Hash))
	d := ws.ArtifactViewDecisions()
	require.Len(t, d, 1)
	assert.Equal(t, "html", d[0].Kind)
	assert.Equal(t, "scripted", d[0].PageKind)
	assert.Equal(t, []string{"ask", "state"}, d[0].Capabilities)
}

// A scripted page can run every manifest query with vibe.run, so the prompt
// lists them all; a static page lists only what its templates name.
func TestArtifactViewStatus_ScriptedPageListsEveryQueryItCanRun(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	m := "version: 1\ntitle: P\nview: html\nqueries:\n  - name: harmless\n    sql: SELECT 1\n  - name: secrets\n    connection: prod_pg\n    sql: SELECT 2\n"
	put := func(name, tmpl string, extra ...artifacts.PageFile) {
		files := append([]artifacts.PageFile{{Path: "index.html.tmpl", Data: []byte(tmpl)}}, extra...)
		require.NoError(t, ws.Artifacts().Put(context.Background(), &artifacts.Artifact{Name: name, ManifestText: []byte(m), Files: files}, artifacts.Overwrite()))
	}
	put("scripted", `<p>{{ query "harmless" }}</p><script src="app.js"></script>`, artifacts.PageFile{Path: "app.js", Data: []byte("vibe.run('secrets')")})
	put("static", `<p>{{ query "harmless" }}</p>`)

	got := viewState(t, ws, "scripted").QueryUses
	assert.Equal(t, []ArtifactViewQuery{{Name: "harmless", Used: true}, {Name: "secrets", Connection: "prod_pg", Used: false}}, got)
	assert.Equal(t, []ArtifactViewQuery{{Name: "harmless", Used: true}}, viewState(t, ws, "static").QueryUses)
}
