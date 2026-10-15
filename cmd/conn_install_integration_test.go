//go:build integration

package cmd

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `--install` downloads everything a lake needs, not just its type's base list:
// a SQLite catalog needs the sqlite extension too. Downloads (network), into an
// empty cache directory.
func TestConnInstallCoversASQLiteCatalogLake(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("os.UserCacheDir ignores XDG_CACHE_HOME on macOS: this would download into the real cache")
	}
	root := newWS(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, stderr, err := runVibe("conn", "add", "lake", "--type", "ducklake", "--path", "data/lake.db",
		"--option", "catalog=sqlite", "--option", "data_path=data/files", "--read-only=false", "--workspace", root)
	require.NoError(t, err, stderr)

	// Test --install on a lake that does not exist yet still installs first.
	out, stderr, err := runVibe("conn", "test", "lake", "--install", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "new lake")

	// A fresh cache: connect --install loops through ducklake, then sqlite.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// (The CLI opens read-only and the catalog is not there yet, so the connect
	// itself is refused for that; what matters is that both were installed.)
	_, stderr, err = runVibe("conn", "connect", "lake", "--install", "--workspace", filepath.Clean(root))
	if err != nil {
		assert.Contains(t, err.Error(), "not readable")
		assert.NotContains(t, err.Error(), "extension")
	}
	assert.Contains(t, stderr, `"ducklake"`)
	assert.Contains(t, stderr, `"sqlite"`)
}
