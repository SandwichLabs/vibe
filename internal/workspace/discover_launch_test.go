package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkWS(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFile), []byte("id: x\nname: x\n"), 0o644))
}

func TestDiscoverForLaunch(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	t.Run("workspace below home and its subfolder", func(t *testing.T) {
		home := filepath.Join(base, "h1")
		ws := filepath.Join(home, "proj")
		mkWS(t, ws)
		sub := filepath.Join(ws, "notebooks", "deep")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		got, err := DiscoverForLaunch(ws, home)
		require.NoError(t, err)
		assert.Equal(t, ws, got)
		got, err = DiscoverForLaunch(sub, home)
		require.NoError(t, err)
		assert.Equal(t, ws, got)
	})

	t.Run("stray vibe.yaml in home itself is ignored", func(t *testing.T) {
		home := filepath.Join(base, "h2")
		mkWS(t, home)
		sub := filepath.Join(home, "code")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		for _, start := range []string{home, sub} {
			_, err := DiscoverForLaunch(start, home)
			assert.ErrorIs(t, err, ErrNotWorkspace, start)
		}
	})

	t.Run("vibe.yaml above home is ignored", func(t *testing.T) {
		top := filepath.Join(base, "h3top")
		mkWS(t, top)
		home := filepath.Join(top, "users", "me")
		require.NoError(t, os.MkdirAll(filepath.Join(home, "x"), 0o755))
		_, err := DiscoverForLaunch(filepath.Join(home, "x"), home)
		assert.ErrorIs(t, err, ErrNotWorkspace)
	})

	t.Run("a closer workspace below home wins over one above", func(t *testing.T) {
		top := filepath.Join(base, "h4top")
		mkWS(t, top)
		home := filepath.Join(top, "me")
		ws := filepath.Join(home, "w")
		mkWS(t, ws)
		got, err := DiscoverForLaunch(ws, home)
		require.NoError(t, err)
		assert.Equal(t, ws, got)
	})

	t.Run("tree that does not contain home", func(t *testing.T) {
		home := filepath.Join(base, "h5")
		require.NoError(t, os.MkdirAll(home, 0o755))
		ws := filepath.Join(base, "srv5")
		mkWS(t, ws)
		got, err := DiscoverForLaunch(filepath.Join(ws), home)
		require.NoError(t, err)
		assert.Equal(t, ws, got)
	})

	t.Run("no workspace", func(t *testing.T) {
		home := filepath.Join(base, "h6")
		require.NoError(t, os.MkdirAll(home, 0o755))
		_, err := DiscoverForLaunch(home, home)
		assert.ErrorIs(t, err, ErrNotWorkspace)
	})

	t.Run("filesystem root is never chosen", func(t *testing.T) {
		_, err := DiscoverForLaunch(string(filepath.Separator), "")
		assert.ErrorIs(t, err, ErrNotWorkspace)
	})

	t.Run("home reached through a symlink: ancestor of the unresolved path is ignored", func(t *testing.T) {
		top := filepath.Join(base, "h7top")
		mkWS(t, top)
		realHome := filepath.Join(base, "elsewhere", "me") // not under top
		require.NoError(t, os.MkdirAll(filepath.Join(realHome, "x"), 0o755))
		link := filepath.Join(top, "homelink")
		require.NoError(t, os.Symlink(realHome, link))
		// start resolves to realHome/x (no workspace there or above); home is
		// given unresolved under top. Use a workspace-less start under top.
		start := filepath.Join(top, "plain")
		require.NoError(t, os.MkdirAll(start, 0o755))
		_, err := DiscoverForLaunch(start, link)
		assert.ErrorIs(t, err, ErrNotWorkspace)
	})
}
