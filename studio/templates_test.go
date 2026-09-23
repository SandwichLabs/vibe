package main

// TDD RED — Phase 6 / unit P6.7: "vibe init --from <template-name> CLI + Studio
// Home template picker" (parity plan §6, P6.6 registry wired into create flows).
//
// These are REAL integration tests of the Wails-bound template surface that the
// Studio Home/Dashboard quick-start template cards (mockup #2) invoke:
//   - App.ListTemplates exposes the registered template name + description to
//     the frontend.
//   - App.CreateFromTemplate creates a new encrypted .vibe seeded from a bundled
//     template, producing an openable, usable container (asserted via reopen).
//
// No mocks: a real SQLCipher + DuckDB .vibe is created and reopened.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApp_ListTemplates asserts the binding returns the registered starter
// templates with both a name and a human-readable description for the cards.
func TestApp_ListTemplates(t *testing.T) {
	app := NewApp(testLogger())

	templates, err := app.ListTemplates()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(templates), 3, "at least 3 starter templates should be exposed")

	byName := make(map[string]string, len(templates))
	for _, tmpl := range templates {
		assert.NotEmpty(t, tmpl.Name, "each template must expose a name")
		assert.NotEmpty(t, tmpl.Description, "each template must expose a description for the card")
		byName[tmpl.Name] = tmpl.Description
	}

	// The registry's names must all be surfaced to the frontend.
	for _, name := range container.ListTemplates() {
		_, ok := byName[name]
		assert.True(t, ok, "ListTemplates should surface registered template %q", name)
	}

	assert.Contains(t, byName, "order-entry", "order-entry should be a quick-start template")
}

// TestApp_CreateFromTemplate asserts the binding creates a usable encrypted
// .vibe seeded from the bundled order-entry template: it opens, holds the
// expected relational tables with data, and carries a starter notebook.
func TestApp_CreateFromTemplate(t *testing.T) {
	app := NewApp(testLogger())
	app.ctx = context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "from-template.vibe")

	err := app.CreateFromTemplate(path, testPassphrase, "order-entry")
	require.NoError(t, err, "CreateFromTemplate should produce a .vibe from the bundled template")

	// Close the app's handle so we can independently reopen and verify.
	require.NoError(t, app.CloseFile())

	c, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err, "the created .vibe should be openable")
	defer c.Close() //nolint:errcheck

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)

	for _, table := range []string{"customers", "orders", "order_items"} {
		var n int
		require.NoError(t, ddb.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n),
			"table %q should exist in the template-seeded container", table)
		assert.Greater(t, n, 0, "table %q should have sample data", table)
	}

	var notebookCount int
	require.NoError(t, c.SQLiteDB().QueryRow("SELECT COUNT(*) FROM _vibe_notebooks").Scan(&notebookCount))
	assert.Equal(t, 1, notebookCount, "order-entry template should seed exactly one starter notebook")
}

// TestApp_CreateFromTemplate_UnknownName asserts an unknown template name yields
// an actionable error listing the available names (mirrors the CLI contract).
func TestApp_CreateFromTemplate_UnknownName(t *testing.T) {
	app := NewApp(testLogger())
	app.ctx = context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.vibe")

	err := app.CreateFromTemplate(path, testPassphrase, "does-not-exist")
	require.Error(t, err, "unknown template name should fail")
	for _, name := range container.ListTemplates() {
		assert.Contains(t, err.Error(), name, "error should list available template %q", name)
	}
}
