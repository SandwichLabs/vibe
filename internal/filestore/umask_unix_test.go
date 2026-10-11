//go:build unix

package filestore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic_NewFileHonoursUmask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")
	require.NoError(t, WriteFileAtomic(path, []byte("x"), 0o666))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o666)&^processUmask(), fi.Mode().Perm(), "same as os.WriteFile would give")
	ref := filepath.Join(filepath.Dir(path), "ref.json")
	require.NoError(t, os.WriteFile(ref, []byte("x"), 0o666))
	rfi, err := os.Stat(ref)
	require.NoError(t, err)
	assert.Equal(t, rfi.Mode().Perm(), fi.Mode().Perm())
}

func TestWriteFileAtomicExact_SetsExactMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k")
	require.NoError(t, WriteFileAtomic(path, []byte("x"), 0o644))
	require.NoError(t, WriteFileAtomicExact(path, []byte("y"), 0o600))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}
