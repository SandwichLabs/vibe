package notebooks

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/a2ui"
)

var update = flag.Bool("update", false, "rewrite golden files")

func goldenNotebook(t *testing.T) Notebook {
	t.Helper()
	nb := Notebook{SchemaVersion: SchemaVersion, ID: "weekly-revenue", Title: "Weekly revenue"}
	sqlCell := Cell{ID: "cell_1", Type: CellSQL, Source: "SELECT region, sum(amt) AS total\nFROM orders\nWHERE amt > 10 AND note <> '<b>&</b>'\nGROUP BY 1", View: "bar", ViewPinned: true, Label: "By region", Connection: "local"}
	md := Cell{ID: "cell_2", Type: CellMarkdown, Source: "# Notes\n\nRevenue < forecast & rising.\n"}
	page := a2ui.Page{Title: "Dash", Components: []a2ui.Component{{ID: "t", Type: "text", Fields: map[string]any{"text": "a < b"}}}}
	a := Cell{ID: "cell_3", Type: CellA2UI, Collapsed: true, Page: &page}
	nb.Cells = []Cell{sqlCell, md, a}
	return nb
}

func newFileStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	root := t.TempDir()
	return NewFileStore(root), root
}

func TestEncodeFile_Golden(t *testing.T) {
	got, err := EncodeFile(goldenNotebook(t))
	require.NoError(t, err)
	path := filepath.Join("testdata", "golden", "weekly-revenue.vnb")
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
	assert.True(t, strings.HasSuffix(string(got), "}\n"), "trailing newline")
	assert.NotContains(t, string(got), "\\u003c", "no HTML escaping")
	assert.NotContains(t, string(got), "updatedAt")
	assert.NotContains(t, string(got), `"id": "weekly-revenue"`)
}

func TestFileStore_SaveLoadSaveIsByteIdentical(t *testing.T) {
	s, root := newFileStore(t)
	ctx := context.Background()
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "weekly-revenue.vnb"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "weekly-revenue.vnb"), golden, 0o644))

	for i := 0; i < 3; i++ {
		nb, warnings, err := s.Get(ctx, "weekly-revenue")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, "weekly-revenue", nb.ID)
		_, _, err = s.Save(ctx, nb)
		require.NoError(t, err)
		after, err := os.ReadFile(filepath.Join(root, "notebooks", "weekly-revenue.vnb"))
		require.NoError(t, err)
		assert.Equal(t, string(golden), string(after), "cycle %d", i)
	}
}

func TestFileStore_CreateNamesFileAfterTitleAndDedupes(t *testing.T) {
	s, root := newFileStore(t)
	ctx := context.Background()
	a, err := s.Create(ctx, "Weekly Revenue!")
	require.NoError(t, err)
	assert.Equal(t, "weekly-revenue", a.ID)
	b, err := s.Create(ctx, "Weekly Revenue!")
	require.NoError(t, err)
	assert.Equal(t, "weekly-revenue-2", b.ID)
	assert.FileExists(t, filepath.Join(root, "notebooks", "weekly-revenue.vnb"))
	assert.FileExists(t, filepath.Join(root, "notebooks", "weekly-revenue-2.vnb"))

	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestFileStore_RenameKeepsID(t *testing.T) {
	s, root := newFileStore(t)
	ctx := context.Background()
	a, err := s.Create(ctx, "Draft")
	require.NoError(t, err)
	require.NoError(t, s.Rename(ctx, a.ID, "Final report"))
	got, _, err := s.Get(ctx, "draft")
	require.NoError(t, err)
	assert.Equal(t, "Final report", got.Title)
	assert.FileExists(t, filepath.Join(root, "notebooks", "draft.vnb"))
}

func TestFileStore_StaleSaveIsAConflict(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	created, err := s.Create(ctx, "Shared")
	require.NoError(t, err)

	// Studio loads it.
	studio, _, err := s.Get(ctx, created.ID)
	require.NoError(t, err)

	// Something else (git pull, an editor, another process) rewrites the file.
	path := filepath.Join(root, "notebooks", "shared.vnb")
	disk, err := os.ReadFile(path)
	require.NoError(t, err)
	changed := strings.Replace(string(disk), `"title": "Shared"`, `"title": "Edited elsewhere"`, 1)
	require.NotEqual(t, string(disk), changed)
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o644))

	studio.Title = "My edit"
	_, _, err = s.Save(ctx, studio)
	require.ErrorIs(t, err, ErrConflict)
	after, _ := os.ReadFile(path)
	assert.Equal(t, changed, string(after), "a refused write changes nothing")

	// Reload is the safe resolution.
	fresh, _, err := s.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Edited elsewhere", fresh.Title)
	fresh.Title = "Merged"
	_, _, err = s.Save(ctx, fresh)
	require.NoError(t, err)

	// Overwrite is the explicit way to clobber.
	studio.Title = "Overwritten"
	_, _, err = s.Save(ctx, studio)
	require.ErrorIs(t, err, ErrConflict)
	saved, _, err := s.Save(ctx, studio, Overwrite())
	require.NoError(t, err)
	assert.Equal(t, "Overwritten", saved.Title)
}

func TestFileStore_RevRoundTripsThroughJSON(t *testing.T) {
	// The frontend sends the document back as JSON; "rev" must survive Coerce.
	ctx := context.Background()
	a, _ := newFileStore(t)
	nb, err := a.Create(ctx, "J")
	require.NoError(t, err)
	got, _, err := a.Get(ctx, nb.ID)
	require.NoError(t, err)
	raw := notebookToRaw(got)

	// A second writer in the same process (the agent) saves first.
	agent, _, err := a.Get(ctx, nb.ID)
	require.NoError(t, err)
	agent.Title = "Agent edit"
	_, _, err = a.Save(ctx, agent)
	require.NoError(t, err)

	// The editor's stale copy still carries the old rev.
	_, _, err = a.Save(ctx, raw)
	assert.ErrorIs(t, err, ErrConflict)
}

func TestFileStore_SecondCreateOfExistingFileConflicts(t *testing.T) {
	// A notebook the store never loaded and that exists on disk: no silent clobber.
	ctx := context.Background()
	s, root := newFileStore(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "hand.vnb"), []byte(`{"schemaVersion":2,"title":"Hand","cells":[]}`), 0o644))
	nb := Empty("Mine")
	nb.ID = "hand"
	_, _, err := s.Save(ctx, notebookToRaw(nb))
	assert.ErrorIs(t, err, ErrConflict)
}

func TestFileStore_DeleteAndMissing(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	nb, err := s.Create(ctx, "Gone")
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, nb.ID))
	assert.NoFileExists(t, filepath.Join(root, "notebooks", "gone.vnb"))
	require.NoError(t, s.Delete(ctx, nb.ID), "deleting a missing notebook is a no-op")
	_, _, err = s.Get(ctx, nb.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFileStore_HandEditedFilesAreCoerced(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	raw := `{"schemaVersion":1,"title":"Old v1","cells":[{"id":"c1","type":"sql","source":"select 1","view":"sparkly"},{"id":"c2","type":"nope"}],"junk":1}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "old.vnb"), []byte(raw), 0o644))
	nb, warnings, err := s.Get(ctx, "old")
	require.NoError(t, err)
	assert.NotEmpty(t, warnings)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, "table", nb.Cells[0].View)

	// A corrupt file lists (so it can be found) and carries the error.
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "bad.vnb"), []byte("{not json"), 0o644))
	list, err := s.List(ctx)
	require.NoError(t, err)
	var bad *Summary
	for i := range list {
		if list[i].ID == "bad" {
			bad = &list[i]
		}
	}
	require.NotNil(t, bad)
	assert.NotEmpty(t, bad.Error)
	_, _, err = s.Get(ctx, "bad")
	assert.Error(t, err)
}

func TestFileStore_NewerSchemaRejected(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "future.vnb"), []byte(`{"schemaVersion":99,"title":"F","cells":[]}`), 0o644))
	_, _, err := s.Get(ctx, "future")
	assert.ErrorContains(t, err, "newer version")
}

func TestFileStore_RefusesPathTricksAndSymlinks(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	for _, id := range []string{"../x", "a/b", "", ".hidden", `a\b`} {
		_, _, err := s.Get(ctx, id)
		assert.Error(t, err, id)
		assert.Error(t, s.Delete(ctx, id), id)
	}

	// A notebooks/ directory that is a symlink out of the workspace.
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "notebooks")))
	_, err := s.Create(ctx, "Escape")
	require.Error(t, err)
	entries, _ := os.ReadDir(outside)
	assert.Empty(t, entries, "nothing may be written outside the workspace")
}

func TestFileStore_SymlinkedFileIsRefused(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	secret := filepath.Join(t.TempDir(), "secret.vnb")
	require.NoError(t, os.WriteFile(secret, []byte(`{"schemaVersion":2,"title":"S","cells":[]}`), 0o644))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "notebooks", "link.vnb")))
	_, _, err := s.Get(ctx, "link")
	assert.Error(t, err)
	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestFileStore_ObserverSeesOwnWrites(t *testing.T) {
	var gotPath, gotHash string
	root := t.TempDir()
	s := NewFileStore(root, WithObserver(func(p, h string) { gotPath, gotHash = p, h }))
	nb, err := s.Create(context.Background(), "Obs")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(gotPath, "obs.vnb"))
	assert.Equal(t, nb.Rev, gotHash)
	require.NoError(t, s.Delete(context.Background(), nb.ID))
	assert.Equal(t, "", gotHash, "a delete reports an empty hash")
}

func TestUnescapeHTML(t *testing.T) {
	assert.Equal(t, `"a < b > c & d"`, string(unescapeHTML([]byte("\"a \\u003c b \\u003e c \\u0026 d\""))))
	// An escaped backslash followed by literal text must survive.
	assert.Equal(t, `"\\u003c"`, string(unescapeHTML([]byte(`"\\u003c"`))))
	assert.Equal(t, `"line\nbreak \"q\""`, string(unescapeHTML([]byte(`"line\nbreak \"q\""`))))
}

func TestFileStore_InvalidNamesAndOversizeFilesListWithError(t *testing.T) {
	ctx := context.Background()
	s, root := newFileStore(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "My Notes.vnb"), []byte(`{"schemaVersion":2,"title":"M","cells":[]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notebooks", "con.vnb"), []byte(`{"schemaVersion":2,"title":"C","cells":[]}`), 0o644))
	f, err := os.Create(filepath.Join(root, "notebooks", "huge.vnb"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(11<<20))
	require.NoError(t, f.Close())
	ok, err := s.Create(ctx, "Fine")
	require.NoError(t, err)

	list, err := s.List(ctx)
	require.NoError(t, err)
	errs := map[string]string{}
	for _, sum := range list {
		errs[sum.ID] = sum.Error
	}
	assert.Contains(t, errs["My Notes"], "not a valid notebook id")
	assert.Contains(t, errs["con"], "not a valid notebook id", "Windows device names are not ids")
	assert.Contains(t, errs["huge"], "too large")
	assert.Empty(t, errs[ok.ID])
	for _, id := range []string{"My Notes", "con", "huge"} {
		_, _, err := s.Get(ctx, id)
		assert.Error(t, err, id)
	}
}

func TestFileStore_ListRefreshesTheLastSeenRevision(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a := NewFileStore(root)
	nb, err := a.Create(ctx, "Shared")
	require.NoError(t, err)
	b := NewFileStore(root) // another process
	got, _, err := b.Get(ctx, nb.ID)
	require.NoError(t, err)
	got.Title = "Changed by b"
	_, _, err = b.Save(ctx, got)
	require.NoError(t, err)
	// a lists, so its fallback revision is current: a revision-less internal
	// save then works; without the list it would be a stale write.
	_, err = a.List(ctx)
	require.NoError(t, err)
	got.Rev, got.Title = "", "Changed by a"
	_, _, err = a.Save(ctx, got)
	assert.NoError(t, err)
}
