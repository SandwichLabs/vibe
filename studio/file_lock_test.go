package main

// TestOpenFile_InUse_FriendlyMessage is the Studio side of #80: when the
// file is already held (e.g. by `vibe mcp serve`), OpenFile must surface a
// readable message naming the holder rather than the raw wrapped error, and
// must leave the app with no container attached.

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenFile_InUse_FriendlyMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held.vibe")

	logger := testLogger()
	seed, err := container.Create(path, container.WithPassphrase(testPassphrase), container.WithLogger(logger))
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	holder, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(logger),
		container.WithLockApp("vibe mcp serve"),
	)
	require.NoError(t, err)
	defer holder.Close() //nolint:errcheck

	app := NewApp(logger)
	app.emit = func(string, ...any) {}

	err = app.OpenFile(path, testPassphrase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "This file is already open in vibe mcp serve (pid ")
	assert.Contains(t, err.Error(), "). Close it there, then try again.")
	assert.Nil(t, app.container)
}
