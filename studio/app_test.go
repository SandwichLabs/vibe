package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// eventLog collects the events an App emits, so lifecycle tests can assert on
// them without a live Wails runtime.
type eventLog struct {
	mu     sync.Mutex
	events []string
	data   map[string][]any
}

func (e *eventLog) sink(event string, data ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	if e.data == nil {
		e.data = map[string][]any{}
	}
	e.data[event] = append(e.data[event], data...)
}

func (e *eventLog) count(event string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, x := range e.events {
		if x == event {
			n++
		}
	}
	return n
}

// newBareApp is an App with no workspace open and an isolated per-user config
// directory (recent list, per-person workspace settings).
func newBareApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	app := NewApp(testLogger())
	app.ctx = context.Background()
	t.Cleanup(func() { _ = app.CloseWorkspace() })
	return app
}

// newWorkspaceDir makes an empty workspace folder (vibe.yaml, notebooks/, ...)
// and returns its path.
func newWorkspaceDir(t *testing.T, name string) string {
	t.Helper()
	res, err := workspace.Init(filepath.Join(t.TempDir(), "ws"), workspace.WithName(name))
	require.NoError(t, err)
	root := res.Workspace.Root()
	require.NoError(t, res.Workspace.Close())
	return root
}

// newOpenApp constructs an App with a real workspace open (its `local` DuckDB
// file, file-backed notebook/query/skill stores) and returns it ready for
// binding-level tests.
func newOpenApp(t *testing.T) *App {
	t.Helper()
	app := newBareApp(t)
	res, err := app.OpenWorkspace(newWorkspaceDir(t, "App Test"))
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
	return app
}

// reopenApp closes and reopens app's workspace, to assert that state survives
// a close/open round trip.
func reopenApp(t *testing.T, app *App) {
	t.Helper()
	root := app.ws.Root()
	require.NoError(t, app.CloseWorkspace())
	res, err := app.OpenWorkspace(root)
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
}
