package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/bundle"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func seedStudioBundle(t *testing.T, app *App) {
	t.Helper()
	_, err := app.ws.Notebooks().Create(app.ctxOrBackground(), "Quarterly review")
	require.NoError(t, err)
	require.NoError(t, app.ws.Queries().Create(app.ctxOrBackground(), &queries.Query{Title: "Top customers", SQL: "SELECT 1"}))
}

// openOther closes app's workspace and opens a fresh one (one hub hold per process).
func openOther(t *testing.T, app *App, name string) {
	t.Helper()
	require.NoError(t, app.CloseWorkspace())
	res, err := app.OpenWorkspace(newWorkspaceDir(t, name))
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
}

func TestBundle_ExportPreviewCommitRoundTrip(t *testing.T) {
	src := newOpenApp(t)
	seedStudioBundle(t, src)
	content, err := src.GetBundleContent()
	require.NoError(t, err)
	require.Len(t, content.Notebooks, 1)
	require.Len(t, content.Queries, 1)

	path := filepath.Join(t.TempDir(), "out.vibe")
	res, err := src.ExportBundle(BundleExportRequest{Path: path, Selection: bundle.Selection{All: true}, Passphrase: "pw one"})
	require.NoError(t, err)
	assert.True(t, res.Encrypted)
	assert.Equal(t, 1, res.Notebooks)

	// Into a second app with a workspace of its own, which already has a notebook of that name.
	dst := src
	openOther(t, dst, "Dest")
	_, err = dst.ws.Notebooks().Create(dst.ctxOrBackground(), "Quarterly review")
	require.NoError(t, err)

	pv, err := dst.PreviewBundleImport(path, "")
	require.NoError(t, err)
	assert.True(t, pv.NeedsPassphrase)
	assert.Empty(t, pv.Handle)
	pv, err = dst.PreviewBundleImport(path, "bad")
	require.NoError(t, err)
	assert.True(t, pv.NeedsPassphrase)
	assert.True(t, pv.WrongPassphrase)

	pv, err = dst.PreviewBundleImport(path, "pw one")
	require.NoError(t, err)
	require.NotEmpty(t, pv.Handle)
	require.Len(t, pv.Items, 2)
	conflicts := 0
	for _, it := range pv.Items {
		if it.Conflict {
			conflicts++
		}
	}
	assert.Equal(t, 1, conflicts)

	ev := &eventLog{}
	dst.emit = ev.sink
	out, err := dst.CommitBundleImport(pv.Handle, BundleDecisions{OnConflict: "rename"})
	require.NoError(t, err)
	assert.Zero(t, out.Failed())
	assert.FileExists(t, filepath.Join(dst.ws.Root(), "notebooks", "quarterly-review-2.vnb"))
	assert.Positive(t, ev.count("notebooks:changed"))

	// A handle is single use.
	_, err = dst.CommitBundleImport(pv.Handle, BundleDecisions{})
	require.Error(t, err)
}

func TestBundle_ImportIntoNewWorkspaceFromTheLauncher(t *testing.T) {
	src := newOpenApp(t)
	seedStudioBundle(t, src)
	path := filepath.Join(t.TempDir(), "out.vibe")
	_, err := src.ExportBundle(BundleExportRequest{Path: path, Selection: bundle.Selection{All: true}})
	require.NoError(t, err)

	require.NoError(t, src.CloseWorkspace())
	app := newBareApp(t)
	pv, err := app.PreviewBundleImport(path, "")
	require.NoError(t, err, "a preview needs no open workspace")
	require.Len(t, pv.Items, 2)
	for _, it := range pv.Items {
		assert.False(t, it.Conflict)
	}
	res, err := app.ImportBundleIntoNewWorkspace(pv.Handle, CreateWorkspaceRequest{Name: "From bundle", ParentDir: t.TempDir()}, BundleDecisions{})
	require.NoError(t, err)
	require.NotNil(t, res.Opened)
	assert.Zero(t, res.Result.Failed())
	assert.FileExists(t, filepath.Join(app.ws.Root(), "notebooks", "quarterly-review.vnb"))
}

func TestBundle_RefusesOlderAndForeignFiles(t *testing.T) {
	app := newOpenApp(t)
	old := filepath.Join(t.TempDir(), "old.vibe")
	require.NoError(t, os.WriteFile(old, append([]byte("SQLite format 3\x00"), make([]byte, 64)...), 0o600))
	_, err := app.PreviewBundleImport(old, "")
	require.Error(t, err)
	assert.Equal(t, "This is an older .vibe file that holds data. Vibe no longer opens these.", err.Error())

	txt := filepath.Join(t.TempDir(), "x.vibe")
	require.NoError(t, os.WriteFile(txt, []byte("hello"), 0o600))
	_, err = app.PreviewBundleImport(txt, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "isn't a Vibe bundle")
}

func TestBundle_ConnectionsImportToThisComputerOnly(t *testing.T) {
	src := newOpenApp(t)
	seedStudioBundle(t, src)
	require.NoError(t, src.ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "wh", Type: "postgres", Host: "db.example", Database: "d"}))
	require.NoError(t, src.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "mine", Type: "sqlite", Path: "/home/me/x.sqlite"}))
	content, err := src.GetBundleContent()
	require.NoError(t, err)
	portable := map[string]bool{}
	for _, c := range content.Connections {
		portable[c.Name] = c.Portable
	}
	assert.True(t, portable["wh"])
	assert.False(t, portable["mine"], "an absolute path cannot leave this computer")

	path := filepath.Join(t.TempDir(), "c.vibe")
	_, err = src.ExportBundle(BundleExportRequest{Path: path, Selection: bundle.Selection{All: true, SharedConnections: true}})
	require.NoError(t, err)

	dst := src
	openOther(t, dst, "Dest")
	pv, err := dst.PreviewBundleImport(path, "")
	require.NoError(t, err)
	require.Len(t, pv.Connections, 1)
	_, err = dst.CommitBundleImport(pv.Handle, BundleDecisions{Connections: []string{"wh"}})
	require.NoError(t, err)
	local := make([]string, 0, len(dst.ws.Local().Connections))
	for _, c := range dst.ws.Local().Connections {
		local = append(local, c.Name)
	}
	assert.Equal(t, []string{"wh"}, local)
	for _, c := range dst.ws.Config().Connections {
		assert.NotEqual(t, "wh", c.Name, "never the committed vibe.yaml")
	}
}

func TestBundle_HandlesAreBounded(t *testing.T) {
	var s bundleStore
	var first string
	for i := 0; i < maxOpenBundles+2; i++ {
		h := s.put(&bundle.Package{})
		if i == 0 {
			first = h
		}
	}
	ok := s.has(first)
	assert.False(t, ok, "the oldest preview was dropped")
	assert.Len(t, s.held, maxOpenBundles)
}

func TestBundle_HandleLifecycle(t *testing.T) {
	app := newOpenApp(t)
	seedStudioBundle(t, app)
	path := filepath.Join(t.TempDir(), "out.vibe")
	_, err := app.ExportBundle(BundleExportRequest{Path: path, Selection: bundle.Selection{All: true}})
	require.NoError(t, err)

	// Bad options spend nothing: the person can choose again with the same handle.
	pv, err := app.PreviewBundleImport(path, "")
	require.NoError(t, err)
	_, err = app.CommitBundleImport(pv.Handle, BundleDecisions{OnConflict: "merge"})
	require.Error(t, err)
	_, err = app.CommitBundleImport(pv.Handle, BundleDecisions{OnConflict: "skip"})
	require.NoError(t, err)

	// Two commits racing for one handle: exactly one gets it.
	pv, err = app.PreviewBundleImport(path, "")
	require.NoError(t, err)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := app.CommitBundleImport(pv.Handle, BundleDecisions{OnConflict: "rename"})
			errs <- err
		}()
	}
	e1, e2 := <-errs, <-errs
	assert.True(t, (e1 == nil) != (e2 == nil), "one commit wins: %v / %v", e1, e2)

	// Closing the workspace drops every held preview.
	pv, err = app.PreviewBundleImport(path, "")
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())
	_, err = app.CommitBundleImport(pv.Handle, BundleDecisions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

// Many commits racing for one handle: exactly one takes it (the lookup, expiry
// check, removal and order trim are one critical section).
func TestBundleStore_TakeIsAtomic(t *testing.T) {
	for round := 0; round < 200; round++ {
		var s bundleStore
		h := s.put(&bundle.Package{})
		other := s.put(&bundle.Package{})
		const n = 16
		var won int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, ok := s.take(h); ok {
					atomic.AddInt32(&won, 1)
				}
			}()
		}
		close(start)
		wg.Wait()
		require.EqualValues(t, 1, won, "round %d", round)
		ok := s.has(other)
		require.True(t, ok, "taking one handle leaves the others")
		require.Len(t, s.order, 1)
	}
}
