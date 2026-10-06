package filestore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_RejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "/etc/passwd", "..", "../x", "a/../../x", ".", "a\x00b"} {
		_, err := Resolve(root, bad)
		assert.ErrorIs(t, err, ErrOutsideRoot, "%q", bad)
	}
	got, err := Resolve(root, "a/b/c.txt")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(got, filepath.Join("a", "b", "c.txt")))
}

func TestResolve_SymlinkRules(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "f"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "outdir")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "f"), filepath.Join(root, "file-link")))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "real"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "inlink")))

	_, err := Resolve(root, "outdir/new.txt")
	assert.ErrorIs(t, err, ErrOutsideRoot, "directory link out of the root")
	_, err = Resolve(root, "outdir/f")
	assert.ErrorIs(t, err, ErrOutsideRoot)
	_, err = Resolve(root, "file-link")
	assert.ErrorIs(t, err, ErrOutsideRoot, "a link as the file itself")
	_, err = Resolve(root, "inlink/x.txt")
	assert.NoError(t, err, "a directory link that stays inside is fine")
}

func TestWriteAtomic_CreateAndConflictRules(t *testing.T) {
	root := t.TempDir()
	st, err := WriteAtomic(root, Write{Rel: "d/a.txt", Data: []byte("one")}, nil)
	require.NoError(t, err)
	assert.Equal(t, HashBytes([]byte("one")), st.Hash)

	// Creating over an existing file is a conflict.
	_, err = WriteAtomic(root, Write{Rel: "d/a.txt", Data: []byte("two")}, nil)
	var ce *ConflictError
	require.ErrorAs(t, err, &ce)
	assert.True(t, ce.Existing)
	assert.ErrorIs(t, err, ErrConflict)

	// Right revision: fine.
	st2, err := WriteAtomic(root, Write{Rel: "d/a.txt", Data: []byte("two"), Expect: st.Hash}, nil)
	require.NoError(t, err)

	// Stale revision.
	_, err = WriteAtomic(root, Write{Rel: "d/a.txt", Data: []byte("three"), Expect: st.Hash}, nil)
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, st2.Hash, ce.Got)
	data, _ := os.ReadFile(filepath.Join(root, "d", "a.txt"))
	assert.Equal(t, "two", string(data), "a refused write changes nothing")

	// Deleted since load.
	require.NoError(t, os.Remove(filepath.Join(root, "d", "a.txt")))
	_, err = WriteAtomic(root, Write{Rel: "d/a.txt", Data: []byte("x"), Expect: st2.Hash}, nil)
	require.ErrorAs(t, err, &ce)
	assert.True(t, ce.Missing)

	// Overwrite wins over everything.
	_, err = WriteAtomic(root, Write{Rel: "d/a.txt", Data: []byte("forced"), Expect: "stale", Overwrite: true}, nil)
	require.NoError(t, err)
}

func TestWriteAtomic_SameBytesDifferentMtimeIsNotAConflict(t *testing.T) {
	root := t.TempDir()
	st, err := WriteAtomic(root, Write{Rel: "a.txt", Data: []byte("same")}, nil)
	require.NoError(t, err)
	p := filepath.Join(root, "a.txt")
	require.NoError(t, os.Chtimes(p, st.ModTime.Add(3600e9), st.ModTime.Add(3600e9)))
	_, err = WriteAtomic(root, Write{Rel: "a.txt", Data: []byte("next"), Expect: st.Hash}, nil)
	assert.NoError(t, err, "the content hash is the arbiter, not mtime")
}

func TestWriteAtomic_LeavesNoTempFilesAndKeepsMode(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x.txt")
	require.NoError(t, os.WriteFile(p, []byte("a"), 0o600))
	hash := HashBytes([]byte("a"))
	_, err := WriteAtomic(root, Write{Rel: "x.txt", Data: []byte("b"), Expect: hash}, nil)
	require.NoError(t, err)
	fi, _ := os.Stat(p)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	// A refused write leaves nothing behind either.
	_, _ = WriteAtomic(root, Write{Rel: "x.txt", Data: []byte("c"), Expect: "stale"}, nil)
	entries, _ := os.ReadDir(root)
	require.Len(t, entries, 1)
	assert.Equal(t, "x.txt", entries[0].Name())
}

func TestWriteAtomic_RefusesToWriteThroughSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "victim.txt")
	require.NoError(t, os.WriteFile(target, []byte("precious"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link.txt")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "dirlink")))

	_, err := WriteAtomic(root, Write{Rel: "link.txt", Data: []byte("pwn"), Overwrite: true}, nil)
	assert.ErrorIs(t, err, ErrOutsideRoot)
	_, err = WriteAtomic(root, Write{Rel: "dirlink/new.txt", Data: []byte("pwn"), Overwrite: true}, nil)
	assert.ErrorIs(t, err, ErrOutsideRoot)
	data, _ := os.ReadFile(target)
	assert.Equal(t, "precious", string(data))
	assert.Error(t, Remove(root, "link.txt", "", true, nil))
	assert.FileExists(t, target)
}

func TestWriteAtomic_ConcurrentWritersExactlyOneWins(t *testing.T) {
	root := t.TempDir()
	base, err := WriteAtomic(root, Write{Rel: "n.txt", Data: []byte("0")}, nil)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := WriteAtomic(root, Write{Rel: "n.txt", Data: []byte(strings.Repeat("x", i+1)), Expect: base.Hash}, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrConflict):
				conflicts++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, 1, wins, "all writers loaded the same revision: only one may land")
	assert.Equal(t, 15, conflicts)
}

func TestObserverSeesWritesBeforeTheRename(t *testing.T) {
	root := t.TempDir()
	var gotPath, gotHash string
	obs := func(p, h string) { gotPath, gotHash = p, h }
	_, err := WriteAtomic(root, Write{Rel: "o.txt", Data: []byte("v")}, obs)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(gotPath, "o.txt"))
	assert.Equal(t, HashBytes([]byte("v")), gotHash)

	// A refused write is never announced.
	gotPath = ""
	_, err = WriteAtomic(root, Write{Rel: "o.txt", Data: []byte("w"), Expect: "stale"}, obs)
	require.Error(t, err)
	assert.Empty(t, gotPath)

	require.NoError(t, Remove(root, "o.txt", "", false, obs))
	assert.Equal(t, "", gotHash)
}

func TestRemove(t *testing.T) {
	root := t.TempDir()
	st, _ := WriteAtomic(root, Write{Rel: "r.txt", Data: []byte("1")}, nil)
	require.NoError(t, os.WriteFile(filepath.Join(root, "r.txt"), []byte("2"), 0o644))
	assert.ErrorIs(t, Remove(root, "r.txt", st.Hash, false, nil), ErrConflict)
	assert.FileExists(t, filepath.Join(root, "r.txt"))
	require.NoError(t, Remove(root, "r.txt", st.Hash, true, nil))
	assert.NoFileExists(t, filepath.Join(root, "r.txt"))
	require.NoError(t, Remove(root, "r.txt", "", false, nil), "missing is a no-op")
}

func TestListDirAndSubdirs(t *testing.T) {
	root := t.TempDir()
	assert.Empty(t, mustList(t, root, "nope", ".x"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "d", "sub"), 0o755))
	for _, n := range []string{"b.x", "a.x", ".hidden.x", "c.y", "edit.x~", ".vibe-tmp-z.x.123"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "d", n), []byte("1"), 0o644))
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "d", "a.x"), filepath.Join(root, "d", "link.x")))
	assert.Equal(t, []string{"a.x", "b.x"}, mustList(t, root, "d", ".x"))
	subs, err := ListSubdirs(root, "d")
	require.NoError(t, err)
	assert.Equal(t, []string{"sub"}, subs)
}

func mustList(t *testing.T, root, rel, ext string) []string {
	t.Helper()
	out, err := ListDir(root, rel, ext)
	require.NoError(t, err)
	return out
}

func TestSlugAndUniqueSlug(t *testing.T) {
	assert.Equal(t, "weekly-revenue", Slug("  Weekly Revenue!! "))
	assert.Equal(t, "untitled", Slug("日本語"))
	assert.Equal(t, "untitled", Slug(""))
	assert.Len(t, Slug(strings.Repeat("ab ", 60)), 64-0, "capped")
	taken := map[string]bool{"a": true, "a-2": true}
	assert.Equal(t, "a-3", UniqueSlug("a", func(s string) bool { return taken[s] }))
	long := strings.Repeat("z", 64)
	got := UniqueSlug(long, func(s string) bool { return s == long })
	assert.LessOrEqual(t, len(got), 64)
	assert.True(t, strings.HasSuffix(got, "-2"))
}

func TestValidID(t *testing.T) {
	for _, ok := range []string{"a", "weekly-revenue", "a_b", "0lead", strings.Repeat("x", 64), "com", "com10", "console"} {
		assert.True(t, ValidID(ok), ok)
	}
	for _, bad := range []string{"", ".", "..", ".hidden", "a/b", `a\b`, "a\x00", strings.Repeat("x", 65),
		"My Notes", "Upper", "a.b", "a:b", "-lead", "_lead", "con", "nul", "prn", "aux", "com1", "lpt9", "é"} {
		assert.False(t, ValidID(bad), bad)
	}
}

func TestRead_SizeCapAndPlainErrors(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big.txt")
	f, err := os.Create(big)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(MaxFileSize+1)) // sparse
	require.NoError(t, f.Close())
	_, _, err = Read(root, "big.txt")
	assert.ErrorIs(t, err, ErrTooLarge)

	require.NoError(t, os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hi"), 0o644))
	data, st, err := Read(root, "ok.txt")
	require.NoError(t, err)
	assert.Equal(t, "hi", string(data))
	assert.Equal(t, HashBytes([]byte("hi")), st.Hash)

	// A directory is not a file, and a read error is an error, not a short file.
	require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), 0o755))
	_, _, err = Read(root, "dir")
	assert.Error(t, err)
	_, _, err = Read(root, "missing.txt")
	assert.ErrorIs(t, err, ErrNotFound)

	// Overwriting a file over the cap still works (the hash streams).
	_, err = WriteAtomic(root, Write{Rel: "big.txt", Data: []byte("small"), Overwrite: true}, nil)
	assert.NoError(t, err)
}

func TestSweepStaleTemps(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notebooks"), 0o755))
	old := filepath.Join(root, "notebooks", TempPrefix+"a.vnb.123")
	fresh := filepath.Join(root, "notebooks", TempPrefix+"b.vnb.456")
	keep := filepath.Join(root, "notebooks", "real.vnb")
	for _, p := range []string{old, fresh, keep} {
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	}
	require.NoError(t, os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))
	SweepStaleTemps(root, []string{"notebooks", "missing"}, time.Minute)
	assert.NoFileExists(t, old)
	assert.FileExists(t, fresh, "a recent temp file may belong to a write in flight")
	assert.FileExists(t, keep)
}
