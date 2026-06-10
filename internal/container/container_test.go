package container

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "test-passphrase-12345"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCreate_SchemaTablesExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Test Container"),
		WithDescription("A test container"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	// Verify all _vibe_* tables exist.
	expectedTables := []string{
		"_vibe_meta",
		"_vibe_pages",
		"_vibe_queries",
		"_vibe_components",
		"_vibe_sources",
		"_vibe_agent_config",
		"_vibe_secrets",
		"_vibe_assets",
		"_vibe_duckdb",
	}

	for _, table := range expectedTables {
		var name string
		err := c.SQLiteDB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?;",
			table,
		).Scan(&name)
		require.NoError(t, err, "table %s should exist", table)
		assert.Equal(t, table, name)
	}
}

func TestCreate_SaveCloseReopen_DataPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	// Create and write data.
	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Persist Test"),
	)
	require.NoError(t, err)

	// Insert a page.
	_, err = c.SQLiteDB().Exec(
		`INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at)
		 VALUES ('page-1', 'Hello', '# Hello World', 'markdown', 1, datetime('now'), datetime('now'));`,
	)
	require.NoError(t, err)

	// Insert data into DuckDB.
	_, err = c.DuckDBDB().Exec("CREATE TABLE test_analytics (id INTEGER, value TEXT);")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("INSERT INTO test_analytics VALUES (1, 'hello');")
	require.NoError(t, err)

	// Save and close.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	// Reopen.
	c2, err := Open(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	// Verify SQLite data persists.
	var title string
	err = c2.SQLiteDB().QueryRow("SELECT title FROM _vibe_pages WHERE id = 'page-1';").Scan(&title)
	require.NoError(t, err)
	assert.Equal(t, "Hello", title)

	// Verify DuckDB data persists.
	var value string
	err = c2.DuckDBDB().QueryRow("SELECT value FROM test_analytics WHERE id = 1;").Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "hello", value)
}

func TestOpen_WrongPassphrase_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	// Create with correct passphrase.
	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// Try to open with wrong passphrase.
	_, err = Open(path,
		WithPassphrase("wrong-passphrase"),
		WithLogger(testLogger()),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open container")
}

func TestClose_TempDirectoryRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)

	// Capture the DuckDB temp directory.
	tempDir := c.duckDB.tempDir
	require.DirExists(t, tempDir)

	// Close the container.
	require.NoError(t, c.Close())

	// Verify temp directory was removed.
	_, err = os.Stat(tempDir)
	assert.True(t, os.IsNotExist(err), "temp directory should be removed after Close")
}

func TestMeta_ContainsRequiredKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Meta Test"),
		WithDescription("Testing metadata"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	requiredKeys := []string{
		"schema_version",
		"format_version",
		"created_at",
		"updated_at",
		"title",
		"description",
		"agent_sandbox",
	}

	for _, key := range requiredKeys {
		value, err := c.Meta(key)
		require.NoError(t, err, "meta key %q should exist", key)
		assert.NotEmpty(t, value, "meta key %q should have a value", key)
	}

	// Verify specific values.
	version, err := c.Meta("schema_version")
	require.NoError(t, err)
	assert.Equal(t, "1", version)

	title, err := c.Meta("title")
	require.NoError(t, err)
	assert.Equal(t, "Meta Test", title)

	description, err := c.Meta("description")
	require.NoError(t, err)
	assert.Equal(t, "Testing metadata", description)

	sandbox, err := c.Meta("agent_sandbox")
	require.NoError(t, err)
	// The in-app wizard agent (Studio-only) defaults to full write access.
	assert.Equal(t, "full", sandbox)
}

func TestCreate_MissingPassphrase_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	_, err := Create(path, WithLogger(testLogger()))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "passphrase is required")
}

func TestCreate_FileAlreadyExists_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	// Create the file first.
	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// Try to create again.
	_, err = Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file already exists")
}

func TestSetMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	// Set a new meta key.
	require.NoError(t, c.SetMeta("custom_key", "custom_value"))

	value, err := c.Meta("custom_key")
	require.NoError(t, err)
	assert.Equal(t, "custom_value", value)

	// Update existing key.
	require.NoError(t, c.SetMeta("title", "Updated Title"))

	title, err := c.Meta("title")
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", title)
}

func TestReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	// Create the container.
	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// Reopen as read-only.
	c2, err := Open(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithReadOnly(true),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	assert.True(t, c2.IsReadOnly())
	assert.Error(t, c2.Save())
	assert.Error(t, c2.SetMeta("key", "value"))
}

// TestMigrationAddsNavAccessColumns (W4): a file whose _vibe_nav predates the
// icon/visible_to columns gains them on open via the additive ALTER migration,
// with existing rows defaulting to visible-to-everyone.
func TestMigrationAddsNavAccessColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-nav.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	// Regress _vibe_nav to its pre-W4 shape with a row, simulating an old file.
	_, err = c.SQLiteDB().Exec(`DROP TABLE _vibe_nav;`)
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(`CREATE TABLE _vibe_nav (
		id TEXT PRIMARY KEY, parent_id TEXT NOT NULL DEFAULT '',
		label TEXT NOT NULL DEFAULT '', action_kind TEXT NOT NULL DEFAULT '',
		target_id TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0);`)
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(
		`INSERT INTO _vibe_nav (id, label, action_kind, target_id) VALUES ('old1', 'Orders', 'open_form', 'f1');`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var label, icon, visibleTo string
	require.NoError(t, c2.SQLiteDB().QueryRow(
		`SELECT label, icon, visible_to FROM _vibe_nav WHERE id = 'old1'`,
	).Scan(&label, &icon, &visibleTo))
	assert.Equal(t, "Orders", label)
	assert.Equal(t, "", icon)
	assert.Equal(t, "", visibleTo)
}

// TestOpenBackfillsAutoNumberPKs: files created before the sequence-backed-PK
// fix have bare integer PRIMARY KEYs; opening such a file must retrofit a
// sequence DEFAULT (seeded past existing rows) so id-less form inserts work.
func TestOpenBackfillsAutoNumberPKs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backfill.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	// A pre-fix-shaped table: bare integer PK, existing rows up to id 7.
	_, err = c.DuckDBDB().Exec(`CREATE TABLE customers (id BIGINT PRIMARY KEY, name VARCHAR)`)
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec(`INSERT INTO customers VALUES (7, 'legacy')`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	// The retrofit makes an id-less insert work and number past the max row.
	_, err = c2.DuckDBDB().Exec(`INSERT INTO customers (name) VALUES ('new')`)
	require.NoError(t, err, "id-less insert must succeed after open-time backfill")
	var id int64
	require.NoError(t, c2.DuckDBDB().QueryRow(`SELECT id FROM customers WHERE name = 'new'`).Scan(&id))
	assert.Equal(t, int64(8), id)
}
