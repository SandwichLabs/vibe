package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "test-passphrase-12345"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newOpenApp constructs an App backed by a real .vibe container (SQLCipher +
// DuckDB) and returns it ready for binding-level tests. It wires the container
// directly (rather than via CreateFile) so the Wails runtime.EventsEmit
// lifecycle call — which requires the real Wails context — is not invoked.
func newOpenApp(t *testing.T) *App {
	t.Helper()

	logger := testLogger()
	app := NewApp(logger)
	app.ctx = context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.vibe")
	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(logger),
		container.WithTitle("App Test"),
	)
	require.NoError(t, err)

	app.container = c
	app.engine = engine.NewManager(c.Session(), engine.WithLogger(logger))
	app.registry = queries.NewRegistry(c.SQLiteDB(), logger)
	app.filePath = path
	app.beginFileSession()

	t.Cleanup(func() {
		_ = c.Close()
	})

	return app
}

// reopenApp saves and closes app's container, then reopens the same file
// path and rewires app.container/engine/registry to the fresh handles — used
// to assert that state survives a Save + Close + Open round-trip.
func reopenApp(t *testing.T, app *App) {
	t.Helper()

	path := app.filePath
	require.NoError(t, app.container.Save())
	require.NoError(t, app.container.Close())

	c, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(app.logger),
	)
	require.NoError(t, err)

	app.container = c
	app.engine = engine.NewManager(c.Session(), engine.WithLogger(app.logger))
	app.registry = queries.NewRegistry(c.SQLiteDB(), app.logger)
	app.beginFileSession()

	t.Cleanup(func() { _ = c.Close() })
}
