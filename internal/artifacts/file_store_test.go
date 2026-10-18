package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

func fsPath(root, name, file string) string { return filepath.Join(root, Dir, name, file) }

func putFresh(t *testing.T, s *FileStore, name string) *Artifact {
	t.Helper()
	a := newArtifact(t, name)
	require.NoError(t, s.Put(context.Background(), a))
	return a
}

func TestFileStore_Layout(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	m, err := os.ReadFile(fsPath(root, "sales", ManifestFile))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(m), "version: 1\n"))
	v, err := os.ReadFile(fsPath(root, "sales", ViewFile))
	require.NoError(t, err)
	assert.Equal(t, goodView, string(v), "the view is stored byte for byte")
	// No leftover temp files.
	entries, err := os.ReadDir(filepath.Join(root, Dir, "sales"))
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestFileStore_SaveLoadSaveIsByteIdentical(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	first, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
	for i := 0; i < 3; i++ {
		a, err := s.Get(context.Background(), "sales")
		require.NoError(t, err)
		require.NoError(t, s.Put(context.Background(), a))
		after, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
		assert.Equal(t, string(first), string(after), "cycle %d", i)
	}
}

func TestFileStore_TwoFileRev_EitherFileChangedConflicts(t *testing.T) {
	for _, file := range []string{ManifestFile, ViewFile} {
		t.Run(file, func(t *testing.T) {
			root := t.TempDir()
			s := NewFileStore(root)
			a := putFresh(t, s, "sales")
			loaded := a.Rev

			// An outside writer changes just one of the two files.
			p := fsPath(root, "sales", file)
			cur, err := os.ReadFile(p)
			require.NoError(t, err)
			if file == ManifestFile {
				cur = []byte(strings.Replace(string(cur), "title: Regional sales", "title: Edited by hand", 1))
			} else {
				cur = []byte(`{"title":"hand edited","components":[{"id":"root","type":"container"}]}`)
			}
			require.NoError(t, os.WriteFile(p, cur, 0o644))
			beforeM, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
			beforeV, _ := os.ReadFile(fsPath(root, "sales", ViewFile))

			mine := newArtifact(t, "sales")
			mine.Manifest.Title = "Mine"
			mine.View = []byte(`{"title":"mine","components":[{"id":"root","type":"container"}]}`)
			err = s.Put(context.Background(), mine, WithRev(loaded))
			require.ErrorIs(t, err, ErrConflict)

			afterM, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
			afterV, _ := os.ReadFile(fsPath(root, "sales", ViewFile))
			assert.Equal(t, string(beforeM), string(afterM), "neither file was written")
			assert.Equal(t, string(beforeV), string(afterV), "neither file was written")
		})
	}
}

func TestFileStore_RevMovesWithBytesAcrossTheBoundary(t *testing.T) {
	a := RevOf([]byte("abc"), []byte("def"))
	b := RevOf([]byte("abcd"), []byte("ef"))
	c := RevOf([]byte("ab"), []byte("cdef"))
	assert.NotEqual(t, a, b)
	assert.NotEqual(t, a, c)
	assert.NotEqual(t, RevOf(nil, []byte("x")), RevOf([]byte("x"), nil))
	assert.NotEqual(t, RevOf([]byte{}, []byte("x")), revision(filePart{}, filePart{data: []byte("x"), present: true}), "absent is not empty")
	assert.Equal(t, "", revision(filePart{}, filePart{}))
}

func TestFileStore_WriteOrderAndHalfWrittenPairs(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()

	// A crash after the view and before the manifest of a NEW artifact leaves a
	// folder with only the view: it lists with an error and does not Get.
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "half"), 0o755))
	require.NoError(t, os.WriteFile(fsPath(root, "half", ViewFile), []byte(goodView), 0o644))
	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "half", list[0].Name)
	assert.Contains(t, list[0].Error, ManifestFile)
	_, err = s.Get(ctx, "half")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)

	// A manifest without a view is also an error, never a blank view.
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "noview"), 0o755))
	m, err := Encode(&newArtifact(t, "x").Manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fsPath(root, "noview", ManifestFile), m, 0o644))
	_, err = s.Get(ctx, "noview")
	require.Error(t, err)
	assert.Contains(t, err.Error(), ViewFile)

	// Saving over a broken folder needs Overwrite (there is no Rev to hand back).
	fix := newArtifact(t, "half")
	assert.ErrorIs(t, s.Put(ctx, fix), ErrConflict)
	require.NoError(t, s.Put(ctx, fix, Overwrite()))
	_, err = s.Get(ctx, "half")
	require.NoError(t, err)
}

func TestFileStore_ListReportsBrokenItems(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	putFresh(t, s, "good")
	bad := filepath.Join(root, Dir, "broken")
	require.NoError(t, os.MkdirAll(bad, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bad, ManifestFile), []byte("version: 1\ntitle: T\nsurprise: x\nqueries: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bad, ViewFile), []byte(goodView), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "Upper"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "con"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, ".hidden"), 0o755))

	list, err := s.List(ctx)
	require.NoError(t, err)
	byName := map[string]Artifact{}
	for _, a := range list {
		byName[a.Name] = a
	}
	assert.Empty(t, byName["good"].Error)
	assert.Contains(t, byName["broken"].Error, "surprise")
	assert.NotEmpty(t, byName["Upper"].Error)
	assert.NotEmpty(t, byName["con"].Error)
	assert.NotContains(t, byName, ".hidden")
}

func TestFileStore_OversizedFilesAreRefused(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	putFresh(t, s, "big")

	require.NoError(t, os.WriteFile(fsPath(root, "big", ManifestFile), []byte(strings.Repeat("a: b\n", MaxManifestBytes/5+10)), 0o644))
	_, err := s.Get(ctx, "big")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte limit")

	putFresh2 := func() {
		require.NoError(t, os.RemoveAll(filepath.Join(root, Dir, "big")))
		s = NewFileStore(root)
		putFresh(t, s, "big")
	}
	putFresh2()
	pad := strings.Repeat("x", MaxViewBytes)
	require.NoError(t, os.WriteFile(fsPath(root, "big", ViewFile), []byte(`{"pad":"`+pad+`"}`), 0o644))
	_, err = s.Get(ctx, "big")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte limit")
	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Contains(t, list[0].Error, "byte limit")

	a := newArtifact(t, "huge")
	a.View = []byte(`{"pad":"` + pad + `"}`)
	assert.ErrorIs(t, s.Put(ctx, a), ErrInvalid)
}

func TestFileStore_SymlinkedFolderIsRefusedAndSkipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	// A real artifact placed outside the workspace and linked in.
	other := NewFileStore(outside)
	require.NoError(t, other.Put(ctx, newArtifact(t, "evil")))
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(outside, Dir, "evil"), filepath.Join(root, Dir, "evil")))

	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list, "a symlinked folder is skipped by List")
	_, err = s.Get(ctx, "evil")
	require.Error(t, err, "and refused by Get")
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "symbolic link")
	assert.Error(t, s.Put(ctx, newArtifact(t, "evil"), Overwrite()), "Put refuses to write through it")
	assert.Error(t, s.Delete(ctx, "evil"))
	// Nothing outside was touched.
	still, err := other.Get(ctx, "evil")
	require.NoError(t, err)
	assert.Equal(t, "Regional sales", still.Manifest.Title)

	// A symlink to a folder INSIDE the workspace is refused as well.
	require.NoError(t, other.Put(ctx, newArtifact(t, "real")))
	inner := NewFileStore(root)
	putFresh(t, inner, "real")
	require.NoError(t, os.Symlink(filepath.Join(root, Dir, "real"), filepath.Join(root, Dir, "alias")))
	_, err = s.Get(ctx, "alias")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
}

func TestFileStore_SymlinkedArtifactsFolderIsRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ctx := context.Background()
	require.NoError(t, NewFileStore(outside).Put(ctx, newArtifact(t, "evil")))
	require.NoError(t, os.Symlink(filepath.Join(outside, Dir), filepath.Join(root, Dir)))
	s := NewFileStore(root)
	_, err := s.List(ctx)
	assert.Error(t, err)
	_, err = s.Get(ctx, "evil")
	assert.Error(t, err)
	assert.Error(t, s.Put(ctx, newArtifact(t, "other")))
}

func TestFileStore_SymlinkedFilesAreRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	for _, file := range []string{ManifestFile, ViewFile} {
		putFresh(t, s, "sales")
		secret := filepath.Join(outside, "secret")
		require.NoError(t, os.WriteFile(secret, []byte("version: 1\n"), 0o644))
		p := fsPath(root, "sales", file)
		require.NoError(t, os.Remove(p))
		require.NoError(t, os.Symlink(secret, p))

		_, err := s.Get(ctx, "sales")
		require.Error(t, err, file)
		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.NotEmpty(t, list[0].Error, "a symlinked %s lists with an error", file)
		assert.Error(t, s.Put(ctx, newArtifact(t, "sales"), Overwrite()), "Put does not write through a link")
		got, _ := os.ReadFile(secret)
		assert.Equal(t, "version: 1\n", string(got))
		require.NoError(t, os.RemoveAll(filepath.Join(root, Dir)))
		s = NewFileStore(root)
	}
}

func TestFileStore_NamesCannotEscape(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	for _, name := range []string{"../escape", "..", "a/../../escape", "/abs", "x\\y", "ünï"} {
		assert.Error(t, s.Put(ctx, newArtifact(t, name)), name)
		_, err := s.Get(ctx, name)
		assert.Error(t, err, name)
		assert.NoError(t, s.Delete(ctx, name), "Delete of a bad name is a no-op")
	}
	entries, err := os.ReadDir(filepath.Dir(root))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, "escape", e.Name())
	}
}

func TestFileStore_DeleteKeepsForeignFiles(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	require.NoError(t, os.WriteFile(fsPath(root, "sales", "notes.md"), []byte("keep"), 0o644))
	require.NoError(t, s.Delete(context.Background(), "sales"))
	_, err := os.Stat(fsPath(root, "sales", ManifestFile))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(fsPath(root, "sales", ViewFile))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(fsPath(root, "sales", "notes.md"))
	assert.NoError(t, err)

	putFresh(t, s, "other")
	require.NoError(t, s.Delete(context.Background(), "other"))
	_, err = os.Stat(filepath.Join(root, Dir, "other"))
	assert.True(t, os.IsNotExist(err), "an emptied folder is removed")
}

func TestFileStore_ObserverSeesBothWrites(t *testing.T) {
	root := t.TempDir()
	var paths []string
	s := NewFileStore(root, WithObserver(func(p, h string) { paths = append(paths, filepath.Base(p)+":"+h[:0]) }))
	putFresh(t, s, "sales")
	assert.Equal(t, []string{ViewFile + ":", ManifestFile + ":"}, paths, "view first, then manifest")
}

func TestFileStore_ConflictErrorShape(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	err := NewFileStore(root).Put(context.Background(), newArtifact(t, "sales"))
	var ce *filestore.ConflictError
	require.ErrorAs(t, err, &ce)
	assert.True(t, ce.Existing)
	assert.Equal(t, Dir+"/sales", ce.Path)
}

func TestFileStore_WriteComparesEachFileAgainstWhatWasRead(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	read, err := s.read("sales")
	require.NoError(t, err)

	// A writer gets in after the pair check but before the writes.
	require.NoError(t, os.WriteFile(fsPath(root, "sales", ViewFile), []byte(`{"slipped":"in"}`), 0o644))
	a := newArtifact(t, "sales")
	prep, err := prepare(a)
	require.NoError(t, err)
	err = s.write(a, prep, read)
	require.ErrorIs(t, err, ErrConflict, "the per-file compare-and-write catches it")
	m, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
	assert.Contains(t, string(m), "title: Regional sales", "the manifest was not written after the view write was refused")
}

func TestFileStore_NamesReachingOutsideArtifactsAreRefusedEvenWhenFilesExist(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	m, err := Encode(&newArtifact(t, "x").Manifest)
	require.NoError(t, err)
	for _, dir := range []string{"escape", filepath.Join(Dir, "Sales")} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, ManifestFile), m, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, ViewFile), []byte(goodView), 0o644))
	}
	for _, name := range []string{"../escape", "Sales", "con"} {
		a, err := s.Get(ctx, name)
		assert.Error(t, err, name)
		assert.Nil(t, a, name)
		assert.Error(t, s.Put(ctx, newArtifact(t, name), Overwrite()), name)
		assert.NoError(t, s.Delete(ctx, name), name)
	}
	for _, p := range []string{
		filepath.Join(root, "escape", ManifestFile), filepath.Join(root, "escape", ViewFile),
		filepath.Join(root, Dir, "Sales", ManifestFile), filepath.Join(root, Dir, "Sales", ViewFile),
	} {
		got, err := os.ReadFile(p)
		require.NoError(t, err, p)
		assert.NotEmpty(t, got)
	}
	b, _ := os.ReadFile(filepath.Join(root, "escape", ManifestFile))
	assert.Equal(t, string(m), string(b), "untouched")
}

func TestRevOf_LengthPrefixCannotBeForged(t *testing.T) {
	assert.NotEqual(t, RevOf([]byte("x\x01y"), []byte("z")), RevOf([]byte("x"), []byte("y\x01z")))
	assert.NotEqual(t, RevOf([]byte("x\x00y"), []byte("z")), RevOf([]byte("x"), []byte("y\x00z")))
	assert.NotEqual(t, RevOf([]byte("x\x01\x00\x00\x00\x00\x00\x00\x00\x01y"), []byte("z")), RevOf([]byte("x"), []byte("y")))
}

func TestFileStore_ConflictingPutLeavesThePairAsItWas(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	var inject func()
	viewWrites := 0
	s := NewFileStore(root, WithObserver(func(p, _ string) {
		if filepath.Base(p) == ViewFile {
			viewWrites++
		}
		if inject != nil && filepath.Base(p) == ViewFile {
			f := inject
			inject = nil
			f()
		}
	}))
	putFresh(t, s, "sales")
	oldM, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
	oldV, _ := os.ReadFile(fsPath(root, "sales", ViewFile))

	// A foreign edit to artifact.yaml lands while the view is being written.
	foreign := []byte(strings.Replace(string(oldM), "title: Regional sales", "title: Foreign", 1))
	inject = func() { require.NoError(t, os.WriteFile(fsPath(root, "sales", ManifestFile), foreign, 0o644)) }
	mine := newArtifact(t, "sales")
	mine.View = []byte(`{"title":"mine","components":[{"id":"root","type":"container"}]}`)
	got, err := s.Get(ctx, "sales")
	require.NoError(t, err)
	mine.Rev = got.Rev
	require.ErrorIs(t, s.Put(ctx, mine), ErrConflict)
	curM, _ := os.ReadFile(fsPath(root, "sales", ManifestFile))
	curV, _ := os.ReadFile(fsPath(root, "sales", ViewFile))
	assert.Equal(t, string(foreign), string(curM), "the foreign manifest is kept")
	assert.Equal(t, string(oldV), string(curV), "the view is rolled back, not left beside the foreign manifest")

	// An edit that lands before the view write is caught before anything changes.
	require.NoError(t, os.WriteFile(fsPath(root, "sales", ManifestFile), oldM, 0o644))
	read, err := s.read("sales")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fsPath(root, "sales", ManifestFile), foreign, 0o644))
	prep, err := prepare(mine)
	require.NoError(t, err)
	viewWrites = 0
	require.ErrorIs(t, s.write(mine, prep, read), ErrConflict)
	assert.Zero(t, viewWrites, "the pre-check refuses before any view write is even announced")
	curV, _ = os.ReadFile(fsPath(root, "sales", ViewFile))
	assert.Equal(t, string(oldV), string(curV))
}

func TestFileStore_RestoreViewOnlyTouchesTheViewThisPutWrote(t *testing.T) {
	for _, tc := range []struct {
		name string
		was  filePart
	}{
		{"old view present", filePart{data: []byte(`{"old":1}`), present: true}},
		{"old view absent", filePart{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s := NewFileStore(root)
			putFresh(t, s, "sales")
			foreign := []byte(`{"foreign":"edit"}`)
			require.NoError(t, os.WriteFile(fsPath(root, "sales", ViewFile), foreign, 0o644))

			// The view this Put wrote was something else: the foreign edit stays.
			s.restoreView("sales", ViewFile, tc.was, true, filestore.HashBytes([]byte(goodView)))
			got, err := os.ReadFile(fsPath(root, "sales", ViewFile))
			require.NoError(t, err, "not deleted")
			assert.Equal(t, string(foreign), string(got), "not overwritten")

			// When it is still our write, the rollback does happen.
			s.restoreView("sales", ViewFile, tc.was, true, filestore.HashBytes(foreign))
			got, err = os.ReadFile(fsPath(root, "sales", ViewFile))
			if tc.was.present {
				require.NoError(t, err)
				assert.Equal(t, string(tc.was.data), string(got))
			} else {
				assert.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestFileStore_DeleteChecksTheLastSeenRevision(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	require.NoError(t, os.WriteFile(fsPath(root, "sales", ViewFile), []byte(`{"title":"edited","components":[{"id":"root","type":"container"}]}`), 0o644))
	require.ErrorIs(t, s.Delete(ctx, "sales"), ErrConflict)
	_, err := os.Stat(fsPath(root, "sales", ManifestFile))
	assert.NoError(t, err, "nothing was deleted")
	_, err = s.Get(ctx, "sales") // reading it refreshes the revision
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, "sales"))
}

func TestFileStore_SourceReadsABrokenArtifactAndPutFixesItWithItsRev(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s := NewFileStore(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(fsPath(root, "sales", ManifestFile)), 0o755))
	require.NoError(t, os.WriteFile(fsPath(root, "sales", ManifestFile), []byte("title: [oops\n"), 0o644))

	_, err := s.Get(ctx, "sales")
	require.ErrorIs(t, err, ErrInvalid)
	src, err := s.Source(ctx, "sales")
	require.NoError(t, err)
	assert.Equal(t, "title: [oops\n", src.Manifest)
	assert.True(t, src.ViewMissing)
	assert.False(t, src.ManifestMissing)

	fix := newArtifact(t, "sales")
	require.NoError(t, s.Put(ctx, fix, WithRev(src.Rev)), "the source's revision is enough: no Overwrite needed")
	_, err = s.Get(ctx, "sales")
	require.NoError(t, err)
}

func TestFileStore_DeleteRemovesEachFileOnlyIfUnchanged(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	var s *FileStore
	armed := false
	s = NewFileStore(root, WithObserver(func(abs, _ string) {
		// Announced just before the view is removed: an outside writer touches the manifest now.
		if armed && filepath.Base(abs) == ViewFile {
			armed = false
			_ = os.WriteFile(fsPath(root, "sales", ManifestFile), []byte("# outside\n"+goodManifest), 0o644)
		}
	}))
	a := putFresh(t, s, "sales")
	armed = true
	require.ErrorIs(t, s.Delete(ctx, "sales", WithRev(a.Rev)), ErrConflict)
	_, err := os.Stat(fsPath(root, "sales", ManifestFile))
	assert.NoError(t, err, "the manifest that changed after the check was not deleted")
}
