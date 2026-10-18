//go:build unix

package lsp

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RefusesAFIFOWithoutHanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	done := make(chan error, 1)
	go func() { _, err := Load(path); done <- err }()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
	assert.Nil(t, NewSource(path).Catalog())
}
