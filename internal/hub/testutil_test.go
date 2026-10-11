package hub

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/require"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newWorkspace makes a fresh workspace with an isolated per-user config dir
// (so the host's settings never leak in) and returns it, open.
func newWorkspace(t *testing.T) workspace.Workspace {
	t.Helper()
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	res, err := workspace.Init(filepath.Join(t.TempDir(), "acme"), workspace.WithInitLogger(quietLogger()))
	require.NoError(t, err)
	require.NoError(t, res.Workspace.Close())
	ws, err := workspace.Open(res.Workspace.Root(), workspace.WithLogger(quietLogger()), workspace.WithConfigDir(os.Getenv("VIBE_CONFIG_DIR")))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
