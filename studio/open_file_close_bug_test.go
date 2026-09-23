package main

// TestOpenFile_FailedReopenDoesNotCrashOnNextOpen is a regression guard for a
// reviewer-caught SIGSEGV: OpenFile/CreateFile/CreateFromTemplate used to
// Save+Close the CURRENT container inline and then attempt the new
// open/create, but on a failed open (e.g. a wrong passphrase) the early
// `return err` left a.container still pointing at the now-CLOSED container.
// The next OpenFile call then saw a.container != nil, called Save()/Close()
// on an already-closed container, and the process died.
//
// Repro sequence from the report: open A, OpenFile(B, wrong passphrase),
// OpenFile(B, any passphrase) — the fix (closeCurrentContainerLocked, called
// unconditionally right after closing, before the new open is attempted)
// means a failed open always leaves a.container nil, so the next call finds
// nothing stale to Save/Close.

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenFile_FailedReopenDoesNotCrashOnNextOpen(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.vibe")
	pathB := filepath.Join(dir, "b.vibe")

	logger := testLogger()
	cA, err := container.Create(pathA, container.WithPassphrase(testPassphrase), container.WithLogger(logger))
	require.NoError(t, err)
	require.NoError(t, cA.Save())
	require.NoError(t, cA.Close())

	cB, err := container.Create(pathB, container.WithPassphrase("b-passphrase"), container.WithLogger(logger))
	require.NoError(t, err)
	require.NoError(t, cB.Save())
	require.NoError(t, cB.Close())

	app := NewApp(logger)
	app.emit = func(string, ...any) {} // no Wails runtime in this test

	// 1. Open A successfully.
	require.NoError(t, app.OpenFile(pathA, testPassphrase))
	require.NotNil(t, app.container)

	// 2. Attempt to open B with the WRONG passphrase — this must fail, and
	// (the fix) must leave a.container nil rather than pointing at A's
	// now-closed container.
	err = app.OpenFile(pathB, "wrong-passphrase")
	require.Error(t, err)
	assert.Nil(t, app.container, "a failed open must leave the app with no stale/closed container")

	// 3. Open B for real. Before the fix, this step's internal
	// Save()/Close() of the (already-closed) container from step 1 would
	// SIGSEGV. Any panic here fails the test.
	require.NotPanics(t, func() {
		err = app.OpenFile(pathB, "b-passphrase")
	})
	require.NoError(t, err)
	assert.NotNil(t, app.container)
	assert.Equal(t, pathB, app.filePath)
}

// TestCloseCurrentContainerLocked_EmitsFileClosed asserts the UI is told to
// fall back to Unlock/the file picker the moment the current container is
// torn down for the next open — not just on an explicit CloseFile call.
func TestCloseCurrentContainerLocked_EmitsFileClosed(t *testing.T) {
	app := newOpenApp(t)
	var events []string
	app.emit = func(name string, _ ...any) {
		events = append(events, name)
	}

	app.mu.Lock()
	app.closeCurrentContainerLocked()
	app.mu.Unlock()

	assert.Contains(t, events, "file:closed")
	assert.Nil(t, app.container)
	assert.Nil(t, app.engine)
	assert.Nil(t, app.registry)
	assert.Empty(t, app.filePath)
	assert.Empty(t, app.sessionID)
}
