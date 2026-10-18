package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	require.NoError(t, (&Catalog{Version: catalogVersion}).Save(real))
	link := filepath.Join(dir, "catalog.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	_, err := Load(link)
	require.Error(t, err)
	assert.Nil(t, NewSource(link).Catalog())
}

func TestLoad_RefusesOversizedWithoutLeaking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, os.WriteFile(path, append([]byte("SECRET"), make([]byte, maxSnapshot)...), 0o600))
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
	assert.NotContains(t, err.Error(), "SECRET")
}

func TestLoad_MissingFileIsNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}
