package filestore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithin(t *testing.T) {
	cases := []struct {
		name string
		root string
		p    string
		want bool
	}{
		{"same path", "/a/b", "/a/b", true},
		{"child", "/a/b", "/a/b/c", true},
		{"deep child", "/a/b", "/a/b/c/d/e", true},
		{"trailing slash on root", "/a/b/", "/a/b/c", true},
		{"unclean child resolving inside", "/a/b", "/a/b/c/../d", true},
		{"parent", "/a/b", "/a", false},
		{"sibling", "/a/b", "/a/c", false},
		{"prefix trick /a/bc vs /a/b", "/a/b", "/a/bc", false},
		{"prefix trick child of /a/bc", "/a/b", "/a/bc/d", false},
		{"dotdot escape", "/a/b", "/a/b/../c", false},
		{"dotdot to parent", "/a/b", "/a/b/..", false},
		{"dot-dot-prefixed name is inside", "/a/b", "/a/b/..hidden", true},
		{"dot-dot-prefixed dir is inside", "/a/b", "/a/b/..x/y", true},
		{"relative vs absolute cannot be related", "/a/b", "c", false},
		{"case differs is a different path", "/a/b", "/a/B/c", false},
		{"root is filesystem root", "/", "/x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Within(tc.root, tc.p))
		})
	}
}

// Within is lexical; callers resolve symlinks first. This pins that: a link
// inside the root that points outside is "within" until it is resolved, and
// not within once it is.
func TestWithin_SymlinkNeedsResolving(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.True(t, Within(realRoot, filepath.Join(realRoot, "link")), "lexically inside")
	resolved, err := filepath.EvalSymlinks(link)
	require.NoError(t, err)
	assert.False(t, Within(realRoot, resolved), "resolved target is outside")
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	require.NoError(t, WriteFileAtomic(path, []byte("one"), 0o644))
	require.NoError(t, os.Chmod(path, 0o640))
	require.NoError(t, WriteFileAtomic(path, []byte("two"), 0o600))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "two", string(got))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), fi.Mode().Perm(), "an existing file keeps its mode, not the one passed")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file is left behind")

	assert.Error(t, WriteFileAtomic(filepath.Join(dir, "missing", "x"), []byte("x"), 0o644))
}
