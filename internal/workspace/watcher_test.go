package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/skills"
)

func startWatcher(t *testing.T, ws Workspace) *Watcher {
	t.Helper()
	w, err := ws.Watch(WithDebounce(40 * time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func next(t *testing.T, w *Watcher) Change {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := w.WaitForChange(ctx)
	require.NoError(t, err, "expected a change event")
	return c
}

func expectQuiet(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case c := <-w.Events():
		t.Fatalf("unexpected change event: %+v", c)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestWatcher_ExternalEditsAreReported(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	nb, err := ws.Notebooks().Create(ctx, "Report")
	require.NoError(t, err)
	w := startWatcher(t, ws)

	path := filepath.Join(root, "notebooks", nb.ID+".vnb")
	data, _ := os.ReadFile(path)

	// An editor saves the notebook.
	require.NoError(t, os.WriteFile(path, append(data[:len(data)-2:len(data)-2], []byte(" \n}\n")...), 0o644))
	c := next(t, w)
	assert.Equal(t, Change{Kind: KindNotebook, Op: OpChanged, ID: "report", Path: "notebooks/report.vnb"}, c)

	// A new file from a git pull.
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "pulled.vnb"), []byte(`{"schemaVersion":2,"title":"P","cells":[]}`), 0o644))
	assert.Equal(t, Change{Kind: KindNotebook, Op: OpCreated, ID: "pulled", Path: "notebooks/pulled.vnb"}, next(t, w))

	// Deleted.
	require.NoError(t, os.Remove(filepath.Join(root, "notebooks", "pulled.vnb")))
	assert.Equal(t, Change{Kind: KindNotebook, Op: OpDeleted, ID: "pulled", Path: "notebooks/pulled.vnb"}, next(t, w))

	// Queries and config.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "queries"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "queries", "q1.sql"), []byte("select 1\n"), 0o644))
	assert.Equal(t, Change{Kind: KindQuery, Op: OpCreated, ID: "q1", Path: "queries/q1.sql"}, next(t, w))

	cfg, _ := os.ReadFile(filepath.Join(root, "vibe.yaml"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "vibe.yaml"), append(cfg, []byte("# edited\n")...), 0o644))
	assert.Equal(t, Change{Kind: KindConfig, Op: OpChanged, ID: "vibe.yaml", Path: "vibe.yaml"}, next(t, w))
	expectQuiet(t, w)
}

func TestWatcher_IgnoresOwnWritesByHash(t *testing.T) {
	ws, _ := newWorkspace(t)
	ctx := context.Background()
	w := startWatcher(t, ws)

	nb, err := ws.Notebooks().Create(ctx, "Mine")
	require.NoError(t, err)
	nb.Title = "Mine, edited"
	_, _, err = ws.Notebooks().Save(ctx, nb)
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{Title: "Q", SQL: "select 1"}))
	require.NoError(t, ws.Skills().Put(ctx, &skills.Stored{Name: "s", Description: "d", Instructions: "i"}))
	require.NoError(t, ws.Notebooks().Delete(ctx, nb.ID))
	require.NoError(t, ws.Skills().Delete(ctx, "s"))
	cfg := ws.Config()
	cfg.Name = "Renamed by me"
	require.NoError(t, ws.SaveConfig(cfg))
	expectQuiet(t, w)

	// ...but an external edit right after is still seen.
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "notebooks", "x.vnb"), []byte(`{"schemaVersion":2,"title":"X","cells":[]}`), 0o644))
	assert.Equal(t, KindNotebook, next(t, w).Kind)
}

func TestWatcher_UntouchedFilesProduceNoEvents(t *testing.T) {
	ws, root := newWorkspace(t)
	_, err := ws.Notebooks().Create(context.Background(), "Still")
	require.NoError(t, err)
	w := startWatcher(t, ws)
	path := filepath.Join(root, "notebooks", "still.vnb")
	data, _ := os.ReadFile(path)
	// Rewriting identical bytes (touch, a checkout of the same content).
	require.NoError(t, os.WriteFile(path, data, 0o644))
	require.NoError(t, os.Chtimes(path, time.Now(), time.Now().Add(time.Second)))
	expectQuiet(t, w)
}

func TestWatcher_DirectoriesCreatedLaterAreWatched(t *testing.T) {
	// Fresh folder with a hand-written vibe.yaml: no notebooks/, queries/ or skills/ yet.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)
	defer ws.Close()
	w := startWatcher(t, ws)

	// git pull creates the directory and files in one go.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notebooks", "late.vnb"), []byte(`{"schemaVersion":2,"title":"L","cells":[]}`), 0o644))
	assert.Equal(t, Change{Kind: KindNotebook, Op: OpCreated, ID: "late", Path: "notebooks/late.vnb"}, next(t, w))

	// skills/<name>/ is its own directory on Linux: both levels appear later.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "revenue"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skills", "revenue", "SKILL.md"), []byte("---\nname: revenue\ndescription: d\n---\n\nbody\n"), 0o644))
	assert.Equal(t, Change{Kind: KindSkill, Op: OpCreated, ID: "revenue", Path: "skills/revenue/SKILL.md"}, next(t, w))

	// Edits inside the new skill directory are watched too.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skills", "revenue", "SKILL.md"), []byte("---\nname: revenue\ndescription: d2\n---\n\nbody\n"), 0o644))
	assert.Equal(t, Change{Kind: KindSkill, Op: OpChanged, ID: "revenue", Path: "skills/revenue/SKILL.md"}, next(t, w))
}

func TestWatcher_AtomicRenameOverIsOneChange(t *testing.T) {
	// What git and most editors do: write a temp file, rename over the target.
	ws, root := newWorkspace(t)
	_, err := ws.Notebooks().Create(context.Background(), "Atomic")
	require.NoError(t, err)
	w := startWatcher(t, ws)
	tmp := filepath.Join(root, "notebooks", ".atomic.vnb.swp")
	require.NoError(t, os.WriteFile(tmp, []byte(`{"schemaVersion":2,"title":"New","cells":[]}`), 0o644))
	require.NoError(t, os.Rename(tmp, filepath.Join(root, "notebooks", "atomic.vnb")))
	c := next(t, w)
	assert.Equal(t, OpChanged, c.Op)
	assert.Equal(t, "atomic", c.ID)
	expectQuiet(t, w)
}

func TestWatcher_BurstIsDebounced(t *testing.T) {
	ws, root := newWorkspace(t)
	// A wider window than startWatcher's: a loaded CI runner can stall longer
	// than 40ms between two writes and split the burst into two changes.
	w, err := ws.Watch(WithDebounce(300 * time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	p := filepath.Join(root, "notebooks", "burst.vnb")
	for i := 0; i < 10; i++ {
		require.NoError(t, os.WriteFile(p, []byte(`{"schemaVersion":2,"title":"B`+string(rune('a'+i))+`","cells":[]}`), 0o644))
		time.Sleep(5 * time.Millisecond)
	}
	c := next(t, w)
	assert.Equal(t, OpCreated, c.Op)
	expectQuiet(t, w)
}

func TestWatcher_CloseIsIdempotentAndClosesEvents(t *testing.T) {
	ws, _ := newWorkspace(t)
	w, err := ws.Watch()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
	_, ok := <-w.Events()
	assert.False(t, ok)
}

func TestWorkspaceCloseStopsWatchers(t *testing.T) {
	ws, _ := newWorkspace(t)
	w, err := ws.Watch()
	require.NoError(t, err)
	require.NoError(t, ws.Close())
	_, ok := <-w.Events()
	assert.False(t, ok)
}

// Review #109 item 4: a watched directory that is removed and recreated (a git
// checkout of another branch and back) must be watched again.
func TestWatcher_RecreatedSkillDirIsWatchedAgain(t *testing.T) {
	ws, root := newWorkspace(t)
	sk := filepath.Join(root, "skills", "foo")
	require.NoError(t, os.MkdirAll(sk, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sk, "SKILL.md"), []byte("---\nname: foo\ndescription: d\n---\n\nbody\n"), 0o644))
	w := startWatcher(t, ws)

	require.NoError(t, os.RemoveAll(sk))
	c := next(t, w)
	assert.Equal(t, Change{Kind: KindSkill, Op: OpDeleted, ID: "foo", Path: "skills/foo/SKILL.md"}, c)

	require.NoError(t, os.MkdirAll(sk, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sk, "SKILL.md"), []byte("---\nname: foo\ndescription: d2\n---\n\nbody2\n"), 0o644))
	assert.Equal(t, OpCreated, next(t, w).Op)
	expectQuiet(t, w)

	// An edit inside the recreated directory is seen.
	require.NoError(t, os.WriteFile(filepath.Join(sk, "SKILL.md"), []byte("---\nname: foo\ndescription: d3\n---\n\nbody3\n"), 0o644))
	assert.Equal(t, Change{Kind: KindSkill, Op: OpChanged, ID: "foo", Path: "skills/foo/SKILL.md"}, next(t, w))
}

func TestWatcher_RecreatedNotebooksDirIsWatchedAgain(t *testing.T) {
	ws, root := newWorkspace(t)
	nb, err := ws.Notebooks().Create(context.Background(), "Keep")
	require.NoError(t, err)
	w := startWatcher(t, ws)
	dir := filepath.Join(root, "notebooks")

	require.NoError(t, os.RemoveAll(dir))
	assert.Equal(t, Change{Kind: KindNotebook, Op: OpDeleted, ID: nb.ID, Path: "notebooks/" + nb.ID + ".vnb"}, next(t, w))

	require.NoError(t, os.MkdirAll(dir, 0o755))
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.vnb"), []byte(`{"schemaVersion":2,"title":"a","cells":[]}`+"\n"), 0o644))
	assert.Equal(t, Change{Kind: KindNotebook, Op: OpCreated, ID: "a", Path: "notebooks/a.vnb"}, next(t, w))

	// And a rename of the whole directory away and back.
	require.NoError(t, os.Rename(dir, dir+".bak"))
	assert.Equal(t, OpDeleted, next(t, w).Op)
	require.NoError(t, os.Rename(dir+".bak", dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.vnb"), []byte(`{"schemaVersion":2,"title":"b","cells":[]}`+"\n"), 0o644))
	got := map[string]ChangeOp{}
	for i := 0; i < 2; i++ {
		c := next(t, w)
		got[c.ID] = c.Op
	}
	assert.Equal(t, OpCreated, got["b"])
}
