package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

func newPageArtifact(t *testing.T, name string) *Artifact {
	t.Helper()
	return &Artifact{Name: name, Manifest: *pageManifest(t, ""), Files: toFiles(goodPageFiles())}
}

func pagePath(root, name, p string) string {
	return filepath.Join(root, Dir, name, HTMLDir, filepath.FromSlash(p))
}

func TestStore_HTMLPageLifecycle(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := newPageArtifact(t, "sales-page")
		require.NoError(t, s.Put(ctx, a))
		require.NotEmpty(t, a.Rev)

		got, err := s.Get(ctx, "sales-page")
		require.NoError(t, err)
		assert.Equal(t, a.Rev, got.Rev, "the revision Put returned is the one a read computes")
		assert.Equal(t, ViewHTML, got.Manifest.View)
		assert.Empty(t, got.View)
		require.NotNil(t, got.Page)
		assert.Equal(t, PageStatic, got.Page.Kind)
		require.NotNil(t, a.Page)
		assert.NotEmpty(t, a.Page.Hash)
		assert.Equal(t, a.Page.Hash, got.Page.Hash, "the hash a Put reports is the hash a read computes")
		assert.Equal(t, "html (static)", ViewLabel(got))
		assert.Equal(t, goodPageFiles()["assets/style.css"], fileText(got.Files, "assets/style.css"))
		assert.Len(t, got.Files, len(goodPageFiles()))

		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Empty(t, list[0].Error)
		assert.Equal(t, "html (static)", ViewLabel(&list[0]))

		src, err := s.Source(ctx, "sales-page")
		require.NoError(t, err)
		assert.Equal(t, a.Rev, src.Rev)
		assert.Equal(t, HTMLDir+"/", src.ViewFile)
		assert.Len(t, src.Files, len(goodPageFiles()))

		// Edit one file: a new revision; the old one conflicts.
		oldRev := got.Rev
		oldHash := got.Page.Hash
		got.Files = setFile(got.Files, "assets/style.css", "body{}")
		require.NoError(t, s.Put(ctx, got))
		assert.NotEqual(t, oldRev, got.Rev)
		assert.NotEqual(t, oldHash, got.Page.Hash)
		got.Manifest.Title = "Renamed"
		hashBeforeTitle := got.Page.Hash
		require.NoError(t, s.Put(ctx, got))
		assert.NotEqual(t, hashBeforeTitle, got.Page.Hash, "the manifest is part of what was allowed")
		stale := newPageArtifact(t, "sales-page")
		err = s.Put(ctx, stale, WithRev(oldRev))
		assert.ErrorIs(t, err, ErrConflict)

		// Dropping a file removes it.
		again, err := s.Get(ctx, "sales-page")
		require.NoError(t, err)
		files := setFile(again.Files, "assets/style.css", "")
		files = dropFile(files, "assets/style.css")
		files = dropFile(files, "assets/logo.svg")
		again.Files = files
		again.Files = setFile(again.Files, "index.html.tmpl", `<p>{{ param "top" }}</p>`)
		again.Files = dropFile(again.Files, "assets/bg.png")
		again.Files = dropFile(again.Files, "foot.tmpl")
		require.NoError(t, s.Put(ctx, again))
		final, err := s.Get(ctx, "sales-page")
		require.NoError(t, err)
		assert.Len(t, final.Files, 1)

		require.NoError(t, s.Delete(ctx, "sales-page"))
		_, err = s.Get(ctx, "sales-page")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func fileText(files []PageFile, p string) string {
	for _, f := range files {
		if f.Path == p {
			return string(f.Data)
		}
	}
	return ""
}

func setFile(files []PageFile, p, text string) []PageFile {
	out := dropFile(files, p)
	return append(out, PageFile{Path: p, Data: []byte(text)})
}

func dropFile(files []PageFile, p string) []PageFile {
	var out []PageFile
	for _, f := range files {
		if f.Path != p {
			out = append(out, f)
		}
	}
	return out
}

func TestStore_HTMLPutRefusals(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := newPageArtifact(t, "p")
		a.Files = nil
		assert.ErrorIs(t, s.Put(ctx, a), ErrInvalid, "no files: never 'keep what is there'")

		a = newPageArtifact(t, "p")
		a.View = []byte(goodView)
		assert.ErrorIs(t, s.Put(ctx, a), ErrInvalid, "a view file beside html/")

		a = newPageArtifact(t, "p")
		a.Files = setFile(a.Files, "index.html.tmpl", `<script src="https://cdn.test/x.js"></script>`)
		assert.ErrorIs(t, s.Put(ctx, a), ErrInvalid)

		b := newArtifact(t, "plain")
		b.Files = toFiles(goodPageFiles())
		assert.ErrorIs(t, s.Put(ctx, b), ErrInvalid, "page files with an a2ui manifest")
		_, err := s.Get(ctx, "p")
		assert.ErrorIs(t, err, ErrNotFound, "nothing was written")
	})
}

func TestStore_HTMLKindSwitches(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()

	// a2ui -> html: the old view file goes.
	a := newArtifact(t, "x")
	require.NoError(t, s.Put(ctx, a))
	p := newPageArtifact(t, "x")
	p.Rev = a.Rev
	require.NoError(t, s.Put(ctx, p))
	_, err := os.Stat(fsPath(root, "x", ViewFile))
	assert.True(t, os.IsNotExist(err), "view.a2ui.json removed")
	require.FileExists(t, pagePath(root, "x", "index.html.tmpl"))

	// html -> script: the page folder goes entirely.
	sc := &Artifact{Name: "x", Manifest: *mustDecode(t, strings.Replace(goodManifest, "view: a2ui", "view: script", 1)), View: []byte("draw()"), Rev: p.Rev}
	require.NoError(t, s.Put(ctx, sc))
	_, err = os.Stat(filepath.Join(root, Dir, "x", HTMLDir))
	assert.True(t, os.IsNotExist(err), "html/ removed, folders too")
	require.FileExists(t, fsPath(root, "x", ScriptFile))
	got, err := s.Get(ctx, "x")
	require.NoError(t, err)
	assert.Nil(t, got.Files)
	assert.Nil(t, got.Page)
}

func TestFileStore_HTMLLayoutAndNesting(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := newPageArtifact(t, "p")
	a.Files = setFile(a.Files, "assets/img/deep.json", "{}")
	require.NoError(t, s.Put(ctx, a))
	require.FileExists(t, pagePath(root, "p", "assets/img/deep.json"))
	got, err := s.Get(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, a.Rev, got.Rev)
	// Removing the nested file removes its emptied folder.
	got.Files = dropFile(got.Files, "assets/img/deep.json")
	require.NoError(t, s.Put(ctx, got))
	_, err = os.Stat(pagePath(root, "p", "assets/img"))
	assert.True(t, os.IsNotExist(err))
	// No temp files are left behind.
	require.NoError(t, filepath.WalkDir(filepath.Join(root, Dir), func(p string, d os.DirEntry, _ error) error {
		assert.False(t, strings.HasPrefix(d.Name(), filestore.TempPrefix), p)
		return nil
	}))
}

func TestFileStore_HTMLRevisionCoversEveryFile(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := newPageArtifact(t, "p")
	require.NoError(t, s.Put(ctx, a))
	for _, f := range []string{"assets/style.css", "foot.tmpl", "index.html.tmpl"} {
		t.Run(f, func(t *testing.T) {
			got, err := s.Get(ctx, "p")
			require.NoError(t, err)
			old, err := os.ReadFile(pagePath(root, "p", f))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(pagePath(root, "p", f), append(old, ' '), 0o644))
			t.Cleanup(func() { _ = os.WriteFile(pagePath(root, "p", f), old, 0o644) })
			err = s.Put(ctx, got)
			assert.ErrorIs(t, err, ErrConflict, "a change to %s outside the store is a conflict", f)
			after, err := s.Get(ctx, "p")
			require.NoError(t, err)
			assert.NotEqual(t, got.Rev, after.Rev)
		})
	}
}

func TestFileStore_HTMLBrokenFolderListsWithItsError(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"symlink file": func(t *testing.T, root string) {
			require.NoError(t, os.Symlink("/etc/passwd", pagePath(root, "p", "assets/link.json")))
		},
		"symlink folder": func(t *testing.T, root string) {
			require.NoError(t, os.Symlink(t.TempDir(), pagePath(root, "p", "assets/dir")))
		},
		"dotfile": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(pagePath(root, "p", ".secret"), []byte("x"), 0o644))
		},
		"unknown extension": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(pagePath(root, "p", "notes.txt"), []byte("x"), 0o644))
		},
		"too deep": func(t *testing.T, root string) {
			require.NoError(t, os.MkdirAll(pagePath(root, "p", "assets/a/b"), 0o755))
			require.NoError(t, os.WriteFile(pagePath(root, "p", "assets/a/b/c.json"), []byte("{}"), 0o644))
		},
		"too big": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(pagePath(root, "p", "assets/big.json"), []byte(`"`+strings.Repeat("a", MaxPageFileBytes)+`"`), 0o644))
		},
		"edited into a script": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(pagePath(root, "p", "index.html.tmpl"), []byte(`<a href="https://x.test">x</a>`), 0o644))
		},
		"stray view file": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(fsPath(root, "p", ViewFile), []byte(goodView), 0o644))
		},
		"html folder is a symlink": func(t *testing.T, root string) {
			other := t.TempDir()
			require.NoError(t, os.RemoveAll(filepath.Join(root, Dir, "p", HTMLDir)))
			require.NoError(t, os.Symlink(other, filepath.Join(root, Dir, "p", HTMLDir)))
		},
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("symlinks")
			}
			root := t.TempDir()
			s := NewFileStore(root)
			ctx := context.Background()
			require.NoError(t, s.Put(ctx, newPageArtifact(t, "p")))
			break_(t, root)
			list, err := s.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.NotEmpty(t, list[0].Error, "listed with its error")
			_, err = s.Get(ctx, "p")
			assert.Error(t, err)
		})
	}
}

func TestFileStore_HTMLTempFilesAreIgnored(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, newPageArtifact(t, "p")))
	// A leftover temp file of an interrupted atomic write is not a page file.
	require.NoError(t, os.WriteFile(pagePath(root, "p", filestore.TempPrefix+"x.json.123"), []byte("x"), 0o644))
	got, err := s.Get(ctx, "p")
	require.NoError(t, err)
	assert.Len(t, got.Files, len(goodPageFiles()))
}

func TestFileStore_NonHTMLArtifactIgnoresAStrayHTMLFolder(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := newArtifact(t, "plain")
	require.NoError(t, s.Put(ctx, a))
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "plain", HTMLDir), 0o755))
	require.NoError(t, os.WriteFile(pagePath(root, "plain", ".weird"), []byte("x"), 0o644))
	got, err := s.Get(ctx, "plain")
	require.NoError(t, err)
	assert.Equal(t, a.Rev, got.Rev, "revisions of the other kinds are unchanged by an html/ folder")
	assert.Nil(t, got.Files)
}

func TestStore_RevisionsOfTheOtherKindsAreStable(t *testing.T) {
	// revision() is unchanged: pin one so a change is a conscious decision.
	assert.Equal(t, RevOf([]byte("m"), []byte("v")), revisionWithFiles(filePart{[]byte("m"), true}, filePart{[]byte("v"), true}, nil))
	assert.NotEqual(t, RevOf([]byte("m"), nil), revisionWithFiles(filePart{[]byte("m"), true}, filePart{}, []PageFile{}))
}

func TestStore_HTMLRevisionBoundaries(t *testing.T) {
	m := filePart{[]byte("m"), true}
	rev := func(files ...PageFile) string { return revisionWithFiles(m, filePart{}, files) }
	assert.NotEqual(t,
		rev(PageFile{"a.tmpl", []byte("xy")}, PageFile{"b.tmpl", []byte("z")}),
		rev(PageFile{"a.tmpl", []byte("x")}, PageFile{"b.tmpl", []byte("yz")}), "a byte across a file boundary")
	assert.NotEqual(t, rev(PageFile{"a.tmpl", []byte("x")}), rev(PageFile{"b.tmpl", []byte("x")}), "a rename")
	assert.NotEqual(t, rev(PageFile{"a.tmpl", []byte("x")}), rev(PageFile{"a.tmpl", []byte("x")}, PageFile{"b.json", nil}), "an added empty file")
	assert.Equal(t, rev(PageFile{"a", nil}, PageFile{"b", nil}), rev(PageFile{"b", nil}, PageFile{"a", nil}))
}

func TestFileStore_HTMLOSClutterIsNotAPageFile(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, newPageArtifact(t, "p")))
	before, err := s.Get(ctx, "p")
	require.NoError(t, err)
	for _, p := range []string{".DS_Store", "assets/.DS_Store", "assets/Thumbs.db", "Thumbs.db"} {
		require.NoError(t, os.WriteFile(pagePath(root, "p", p), []byte("junk"), 0o644))
	}
	after, err := s.Get(ctx, "p")
	require.NoError(t, err, "not an error")
	assert.Equal(t, before.Rev, after.Rev, "not hashed")
	assert.Equal(t, before.Page.Hash, after.Page.Hash)
	assert.Len(t, after.Files, len(before.Files), "not served")
	// Another dotfile is still refused.
	require.NoError(t, os.WriteFile(pagePath(root, "p", ".env"), []byte("x"), 0o644))
	_, err = s.Get(ctx, "p")
	assert.Error(t, err)
}

// An html/ folder left beside a manifest that is not view: html is not part of
// that artifact, but turning the artifact into a page must not inherit it.
func TestFileStore_StaleHTMLFolderCannotJoinAPage(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := newArtifact(t, "x")
	require.NoError(t, s.Put(ctx, a))
	require.NoError(t, os.MkdirAll(pagePath(root, "x", "assets"), 0o755))
	require.NoError(t, os.WriteFile(pagePath(root, "x", "assets/stale.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(pagePath(root, "x", "evil.js"), []byte("steal()"), 0o644))

	p := newPageArtifact(t, "x")
	p.Rev = a.Rev
	require.NoError(t, s.Put(ctx, p))
	got, err := s.Get(ctx, "x")
	require.NoError(t, err)
	assert.Len(t, got.Files, len(goodPageFiles()), "only the files that were sent")
	assert.Equal(t, PageStatic, got.Page.Kind, "the stale script did not make it scripted, or run")
	assert.Equal(t, p.Rev, got.Rev, "the revision a Put returns is what the disk holds")
	_, err = os.Stat(pagePath(root, "x", "evil.js"))
	assert.True(t, os.IsNotExist(err))
}

func TestReadPageFile_RefusesALink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, Dir, "p", HTMLDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	outside := filepath.Join(t.TempDir(), "secret.json")
	require.NoError(t, os.WriteFile(outside, []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.json"), []byte("{}"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link.json")))

	realInfo, err := os.Lstat(filepath.Join(dir, "real.json"))
	require.NoError(t, err)
	// A link swapped in for the file the listing saw is not the file it saw.
	_, _, err = readPageFile(root, "artifacts/p/html/link.json", realInfo, 100)
	assert.Error(t, err)
	// And the listing refuses a link outright.
	_, _, err = readPageFiles(root, "p")
	assert.ErrorIs(t, err, ErrInvalid)
	data, _, err := readPageFile(root, "artifacts/p/html/real.json", realInfo, 100)
	require.NoError(t, err)
	assert.Equal(t, "{}", string(data))
	_, _, err = readPageFile(root, "artifacts/p/html/real.json", realInfo, 1)
	assert.Error(t, err, "the cap applies to what is read")
}

// Leftover html/ files beside a missing manifest are seen by a Put and a Delete.
func TestFileStore_HTMLFilesWithoutAManifest(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	require.NoError(t, os.MkdirAll(pagePath(root, "x", "assets"), 0o755))
	require.NoError(t, os.WriteFile(pagePath(root, "x", "leftover.js"), []byte("steal()"), 0o644))

	// A Put makes a page from exactly the files sent; the leftover is gone.
	p := newPageArtifact(t, "x")
	require.NoError(t, s.Put(ctx, p, WithRev("")), "nothing but leftovers: a create")
	got, err := s.Get(ctx, "x")
	require.NoError(t, err)
	assert.Equal(t, PageStatic, got.Page.Kind)
	_, err = os.Stat(pagePath(root, "x", "leftover.js"))
	assert.True(t, os.IsNotExist(err))

	// A Delete with the manifest gone removes the folder's files.
	require.NoError(t, os.Remove(fsPath(root, "x", ManifestFile)))
	require.NoError(t, s.Delete(ctx, "x", Overwrite()))
	_, err = os.Stat(filepath.Join(root, Dir, "x", HTMLDir))
	assert.True(t, os.IsNotExist(err), "html/ is not left behind")
}
