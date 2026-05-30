package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStudioStartup_TriggersOrphanSweep is the P6.8 regression guard for the
// "startup sweep" acceptance criterion: it proves the wired Studio startup hook
// actually invokes the orphan-temp recovery, not just that the underlying
// SweepOrphanedTemp function works in isolation. A fabricated dead-PID orphan
// must be reclaimed by startup, while a live (this-process) dir is kept.
func TestStudioStartup_TriggersOrphanSweep(t *testing.T) {
	root := os.TempDir()

	// Dead-PID orphan: this PID is not a running process, so startup must sweep it.
	deadPID := 999999999
	orphanDir := filepath.Join(root, "vibe-duckdb-"+strconv.Itoa(deadPID)+"-startup-stale")
	require.NoError(t, os.MkdirAll(orphanDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(orphanDir, "analytics.duckdb"), []byte("stale"), 0600))
	t.Cleanup(func() { _ = os.RemoveAll(orphanDir) })

	// Live dir owned by this very (running) process; startup must NOT sweep it.
	liveDir := filepath.Join(root, "vibe-duckdb-"+strconv.Itoa(os.Getpid())+"-startup-live")
	require.NoError(t, os.MkdirAll(liveDir, 0700))
	t.Cleanup(func() { _ = os.RemoveAll(liveDir) })

	app := NewApp(testLogger())
	// startup wires container.StartupSweep; a background context keeps the Wails
	// runtime emitter inert (emit is installed but never invoked here).
	app.startup(context.Background())

	assert.NoDirExists(t, orphanDir, "studio startup must sweep the dead-PID orphan temp dir")
	assert.DirExists(t, liveDir, "studio startup must keep a live process's temp dir")
}
