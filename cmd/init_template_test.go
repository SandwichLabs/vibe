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
//     + a starter notebook (asserted via reopen).
//   - `vibe init --from <path.vibe>` still clones an existing file as before
//     (name lookup first, else treat as a path — deterministic disambiguation).
//   - `vibe init --from <unknown-name>` returns a clear, actionable error that
//     lists the available template names.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitCommand_FromBundledTemplate asserts `--from order-entry` seeds the
// registered bundled template (not a file clone), producing an openable .vibe
// that contains the order-entry relational tables and a starter notebook.
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

	// Exactly one starter notebook was seeded from the template (#68: forms
	// were dropped, templates are now tables + seed data + a notebook).
	var notebookCount int
	require.NoError(t, c.SQLiteDB().QueryRow("SELECT COUNT(*) FROM _vibe_notebooks").Scan(&notebookCount))
	assert.Equal(t, 1, notebookCount, "order-entry template should seed exactly one starter notebook")
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

// TestInitCommand_FromPathClone_ExistingTargetNotTruncated (#80): cloning
// into a path that already has content must be refused like Create, never
// silently truncated — the pre-#80 code used os.Create, which does exactly
// that.
func TestInitCommand_FromPathClone_ExistingTargetNotTruncated(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	templatePath := filepath.Join(dir, "source.vibe")
	_, err := executeCommand("init", templatePath, "--title", "Source App")
	require.NoError(t, err)

	targetPath := filepath.Join(dir, "target.vibe")
	const existingContent = "not a vibe file, must survive"
	require.NoError(t, os.WriteFile(targetPath, []byte(existingContent), 0o600))

	_, err = executeCommand("init", targetPath, "--from", templatePath)
	require.Error(t, err, "cloning into an existing target must be refused")
	assert.Contains(t, err.Error(), "already exists")

	got, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(got), "the existing target must be left untouched")
}

// TestInitCommand_FromPathClone_LockedTargetLeftIntact (#80): cloning into a
// path another process holds open (a writer lock on the SAME name #80 just
// added) must be refused before it can ever delete that process's file out
// from under it. The O_EXCL guard fixing item 2 catches this at the same
// "already exists" check as the plain-existing-file case above — the target
// being locked never even reaches container.Open — so this asserts the
// held container is left fully intact and still usable afterward, not a
// distinct error string.
func TestInitCommand_FromPathClone_LockedTargetLeftIntact(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	templatePath := filepath.Join(dir, "source.vibe")
	_, err := executeCommand("init", templatePath, "--title", "Source App")
	require.NoError(t, err)

	targetPath := filepath.Join(dir, "target.vibe")
	holder, err := container.Create(targetPath, container.WithPassphrase("held-pass"), container.WithLogger(logger))
	require.NoError(t, err)

	_, err = executeCommand("init", targetPath, "--from", templatePath)
	require.Error(t, err, "cloning into a locked, already-existing target must be refused")
	assert.Contains(t, err.Error(), "already exists")

	// The holder's own container must still be fully usable — nothing was
	// deleted or truncated out from under it.
	_, err = holder.DuckDBDB().Exec("CREATE TABLE still_alive AS SELECT 1")
	require.NoError(t, err, "the held container must be untouched by the refused clone")
	require.NoError(t, holder.Close())
}
