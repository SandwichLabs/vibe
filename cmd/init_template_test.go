package cmd

// TDD RED — Phase 6 / unit P6.7: "vibe init --from <template-name> CLI + Studio
// Home template picker" (parity plan §6, P6.6 registry wired into create flows).
//
// These are REAL integration tests: they invoke the production `init` command
// end-to-end (cobra), let it create a real encrypted .vibe (SQLCipher + DuckDB),
// then reopen the container and assert the bundled template was seeded.
//
// Acceptance behavior asserted here:
//   - `vibe init --from order-entry` builds from the bundled template registry
//     (P6.6): the file is created, opens, and contains the order-entry tables
//     + at least one form (asserted via reopen).
//   - `vibe init --from <path.vibe>` still clones an existing file as before
//     (name lookup first, else treat as a path — deterministic disambiguation).
//   - `vibe init --from <unknown-name>` returns a clear, actionable error that
//     lists the available template names.

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitCommand_FromBundledTemplate asserts `--from order-entry` seeds the
// registered bundled template (not a file clone), producing an openable .vibe
// that contains the order-entry relational tables and at least one form.
func TestInitCommand_FromBundledTemplate(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	newPath := filepath.Join(dir, "orders.vibe")

	output, err := executeCommand("init", newPath, "--from", "order-entry")
	require.NoError(t, err, "init --from order-entry should succeed")
	assert.Contains(t, output, "Created")

	// Reopen and assert the bundled template was actually seeded.
	c, err := container.Open(newPath,
		container.WithPassphrase("testpass123"),
		container.WithLogger(logger),
	)
	require.NoError(t, err, "the created .vibe should open with the supplied passphrase")
	defer c.Close() //nolint:errcheck

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)

	// order-entry tables exist with sample data.
	for _, table := range []string{"customers", "orders", "order_items"} {
		var n int
		require.NoError(t, ddb.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n),
			"table %q should exist after seeding the order-entry template", table)
		assert.Greater(t, n, 0, "table %q should have sample data", table)
	}

	// At least one form was seeded from the template.
	var formCount int
	require.NoError(t, c.SQLiteDB().QueryRow("SELECT COUNT(*) FROM _vibe_forms").Scan(&formCount))
	assert.GreaterOrEqual(t, formCount, 1, "order-entry template should seed at least one form")
}

// TestInitCommand_FromPathStillClones asserts the existing clone-from-file path
// is preserved: when --from is a real .vibe path (not a registered name), it is
// treated as a path and cloned as before.
func TestInitCommand_FromPathStillClones(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a source .vibe to clone from.
	templatePath := filepath.Join(dir, "source.vibe")
	_, err := executeCommand("init", templatePath, "--title", "Source App")
	require.NoError(t, err)

	// Clone it via --from <path>.
	clonePath := filepath.Join(dir, "clone.vibe")
	output, err := executeCommand("init", clonePath, "--from", templatePath, "--title", "Clone")
	require.NoError(t, err, "init --from <path.vibe> should still clone a file")
	assert.Contains(t, output, "Created")

	c, err := container.Open(clonePath,
		container.WithPassphrase("testpass123"),
		container.WithLogger(logger),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	title, err := c.Meta("title")
	require.NoError(t, err)
	assert.Equal(t, "Clone", title, "clone should carry the overridden title")
}

// TestInitCommand_FromUnknownName_ListsTemplates asserts an unknown --from value
// (that is neither a registered template name nor an existing file path) yields
// an actionable error listing the valid template names.
func TestInitCommand_FromUnknownName_ListsTemplates(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	newPath := filepath.Join(dir, "bad.vibe")

	_, err := executeCommand("init", newPath, "--from", "does-not-exist")
	require.Error(t, err, "unknown --from value should fail")

	// The error must list the available template names so the user can recover.
	for _, name := range container.ListTemplates() {
		assert.Contains(t, err.Error(), name,
			"error should list available template name %q", name)
	}
}
