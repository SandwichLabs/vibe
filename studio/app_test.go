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
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
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
	app.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(logger))
	app.registry = queries.NewRegistry(c.SQLiteDB(), logger)
	app.filePath = path

	t.Cleanup(func() {
		_ = c.Close()
	})

	return app
}

// TestAppFieldMetaBindings exercises the Wails-bound GetFieldMeta/SetFieldMeta
// methods (plan §1.3) against a real opened container, mirroring the existing
// meta binding behavior: no-file-open guards, upsert, and round-trip reads.
func TestAppFieldMetaBindings(t *testing.T) {
	app := newOpenApp(t)

	// Empty table returns no rows, no error.
	got, err := app.GetFieldMeta("customers")
	require.NoError(t, err)
	require.Empty(t, got)

	meta := schema.FieldMeta{
		Caption:     "Status",
		ControlType: "combo",
		Required:    true,
		LookupSQL:   "SELECT id,name FROM statuses",
		ValueCol:    "id",
		DisplayCol:  "name",
	}
	require.NoError(t, app.SetFieldMeta("customers", "status", meta))

	got, err = app.GetFieldMeta("customers")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "customers", got[0].TableName)
	assert.Equal(t, "status", got[0].ColumnName)
	assert.Equal(t, "Status", got[0].Caption)
	assert.Equal(t, "combo", got[0].ControlType)
	assert.True(t, got[0].Required)

	// Upsert through the binding must not create a duplicate row.
	meta2 := meta
	meta2.Caption = "Account Status"
	meta2.Required = false
	require.NoError(t, app.SetFieldMeta("customers", "status", meta2))

	got, err = app.GetFieldMeta("customers")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Account Status", got[0].Caption)
	assert.False(t, got[0].Required)
}

// TestAppFieldMetaBindingsNoFileOpen verifies the no-file-open guards mirror
// the existing GetAllMeta/SetMeta bindings.
func TestAppFieldMetaBindingsNoFileOpen(t *testing.T) {
	app := NewApp(testLogger())
	app.startup(context.Background())

	_, err := app.GetFieldMeta("customers")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	err = app.SetFieldMeta("customers", "status", schema.FieldMeta{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")
}
