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
		"_vibe_agent_config",
		"_vibe_secrets",
		"_vibe_assets",
		"_vibe_duckdb",
		"_vibe_skills",
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
	assert.ErrorIs(t, err, ErrWrongPassphrase, "#91: the sentinel must survive the wrap chain so callers (Studio's OpenFile) can give a friendly message")
}

// TestOpen_UnreadableFile_CorrectPassphrase_IsNotWrongPassphrase is the
// review round-1 fix (required #1): an unreadable file (permission denied)
// opened with the CORRECT passphrase must never satisfy
// errors.Is(err, ErrWrongPassphrase) — a permission/IO problem is not the
// same as a wrong key or a corrupt/non-database file, and collapsing them
// together hid the real cause behind "check Caps Lock". Skipped when
// running as root, since root ignores file permission bits and the file
// would open successfully.
func TestOpen_UnreadableFile_CorrectPassphrase_IsNotWrongPassphrase(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err = Open(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrWrongPassphrase, "a permission error with the correct passphrase must not be reported as a wrong passphrase")
	assert.Contains(t, err.Error(), "permission denied")
}

// TestOpen_GarbageFile_IsWrongPassphrase pins the other side: a file that is
// simply not a SQLite/SQLCipher database at all (SQLITE_NOTADB) must still
// resolve to ErrWrongPassphrase, since the sentinel is documented to cover
// both "wrong key" and "not a .vibe file".
func TestOpen_GarbageFile_IsWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.vibe")
	require.NoError(t, os.WriteFile(path, []byte("hello this is not sqlite at all, just some text padding padding"), 0o600))

	_, err := Open(path,
		WithPassphrase("x"),
		WithLogger(testLogger()),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWrongPassphrase)
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

	// agent_sandbox is deliberately NOT seeded on a new file (#79): a
	// missing key means agents.DefaultSandboxMode, not a literal stored
	// here — see schema.go's defaultMeta comment.
	_, err = c.Meta("agent_sandbox")
	assert.Error(t, err, "a new file must not have agent_sandbox stored")
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

// TestMigrationCreatesSkillsTable (#62): a file created before _vibe_skills
// existed gains the table on open via the additive `CREATE TABLE IF NOT
// EXISTS` migration, without disturbing anything else.
func TestMigrationCreatesSkillsTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-skills.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	// Simulate a file created before #62: drop the table entirely.
	_, err = c.SQLiteDB().Exec(`DROP TABLE _vibe_skills;`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var name string
	require.NoError(t, c2.SQLiteDB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='_vibe_skills';",
	).Scan(&name))
	assert.Equal(t, "_vibe_skills", name)

	// The table must actually be usable (right columns).
	_, err = c2.SQLiteDB().Exec(
		`INSERT INTO _vibe_skills (name, description, instructions, table_name, source, created_at, updated_at)
		 VALUES ('t', 'd', 'i', '', 'user', 'now', 'now')`)
	require.NoError(t, err)
}

// TestOpenOldFileWithAccessLayerTables (#68): a file created BEFORE the
// Access application layer was removed still has _vibe_field_meta,
// _vibe_forms, _vibe_relationships, _vibe_reports, _vibe_nav, and
// _vibe_macros, each carrying a row. Opening it after #68 must succeed, and
// every one of those tables — and its data — must survive completely
// untouched: the additive-migration policy never drops a table from an
// existing file, it just stops creating it for new ones.
func TestOpenOldFileWithAccessLayerTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy-access-layer.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)

	// Simulate a pre-#68 file: recreate the removed tables with one row each.
	ddls := []string{
		`CREATE TABLE _vibe_field_meta (table_name TEXT, column_name TEXT, caption TEXT NOT NULL DEFAULT '',
			control_type TEXT NOT NULL DEFAULT '', default_value TEXT NOT NULL DEFAULT '', required INTEGER NOT NULL DEFAULT 0,
			format_code TEXT NOT NULL DEFAULT '', validation_rule TEXT NOT NULL DEFAULT '', validation_message TEXT NOT NULL DEFAULT '',
			input_mask TEXT NOT NULL DEFAULT '', lookup_sql TEXT NOT NULL DEFAULT '', display_col TEXT NOT NULL DEFAULT '',
			value_col TEXT NOT NULL DEFAULT '', control_tip TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (table_name, column_name));`,
		`INSERT INTO _vibe_field_meta (table_name, column_name, caption) VALUES ('orders', 'status', 'Status');`,
		`CREATE TABLE _vibe_forms (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', table_name TEXT NOT NULL DEFAULT '',
			archetype TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1, spec TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO _vibe_forms (id, title, table_name, archetype, spec, created_at, updated_at)
			VALUES ('f1', 'Orders Form', 'orders', 'columnar', '{}', 'now', 'now');`,
		`CREATE TABLE _vibe_relationships (id TEXT PRIMARY KEY, parent_table TEXT NOT NULL DEFAULT '', parent_column TEXT NOT NULL DEFAULT '',
			child_table TEXT NOT NULL DEFAULT '', child_column TEXT NOT NULL DEFAULT '', on_delete TEXT NOT NULL DEFAULT 'NO ACTION',
			on_update TEXT NOT NULL DEFAULT 'NO ACTION', enforce_integrity INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO _vibe_relationships (id, parent_table, parent_column, child_table, child_column, created_at)
			VALUES ('r1', 'customers', 'id', 'orders', 'customer_id', 'now');`,
		`CREATE TABLE _vibe_reports (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '',
			groups TEXT NOT NULL DEFAULT '[]', sort TEXT NOT NULL DEFAULT '[]', bands TEXT NOT NULL DEFAULT '{}',
			version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO _vibe_reports (id, title, source, created_at, updated_at) VALUES ('rep1', 'Orders Report', 'orders', 'now', 'now');`,
		`CREATE TABLE _vibe_nav (id TEXT PRIMARY KEY, parent_id TEXT NOT NULL DEFAULT '', label TEXT NOT NULL DEFAULT '',
			action_kind TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0,
			icon TEXT NOT NULL DEFAULT '', visible_to TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO _vibe_nav (id, label, action_kind, target_id) VALUES ('n1', 'Orders', 'open_form', 'f1');`,
		`CREATE TABLE _vibe_macros (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1,
			spec TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO _vibe_macros (id, title, spec, created_at, updated_at) VALUES ('m1', 'Startup', '{}', 'now', 'now');`,
	}
	for _, ddl := range ddls {
		_, err := c.SQLiteDB().Exec(ddl)
		require.NoError(t, err, "ddl: %s", ddl)
	}
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err, "opening a file with the (removed) Access layer's tables must still succeed")
	defer c2.Close() //nolint:errcheck

	for table, wantCount := range map[string]int{
		"_vibe_field_meta":    1,
		"_vibe_forms":         1,
		"_vibe_relationships": 1,
		"_vibe_reports":       1,
		"_vibe_nav":           1,
		"_vibe_macros":        1,
	} {
		var n int
		require.NoError(t, c2.SQLiteDB().QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
		assert.Equal(t, wantCount, n, "table %q must survive untouched on a file opened after #68", table)
	}
}

// TestMigrationCreatesNotebooksTable (#66 part A): a file created before
// _vibe_notebooks existed gains the table on open via the additive `CREATE
// TABLE IF NOT EXISTS` migration.
func TestMigrationCreatesNotebooksTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-notebooks.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	// Simulate a file created before #66: drop the table entirely.
	_, err = c.SQLiteDB().Exec(`DROP TABLE _vibe_notebooks;`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var name string
	require.NoError(t, c2.SQLiteDB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='_vibe_notebooks';",
	).Scan(&name))
	assert.Equal(t, "_vibe_notebooks", name)

	_, err = c2.SQLiteDB().Exec(
		`INSERT INTO _vibe_notebooks (id, title, cells, created_at, updated_at) VALUES ('n', 't', '[]', 'now', 'now')`)
	require.NoError(t, err)
}

// TestMigrationAddsPagesMigratedToColumn (#66 part A): a file whose
// _vibe_pages predates the migrated_to column gains it on open via the
// additive ALTER migration, with existing rows defaulting to ” (not yet
// migrated), and no page rows are disturbed.
func TestMigrationAddsPagesMigratedToColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-pages.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	// Regress _vibe_pages to its pre-#66 shape, simulating an old file.
	_, err = c.SQLiteDB().Exec(`DROP TABLE _vibe_pages;`)
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(`CREATE TABLE _vibe_pages (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
		page_type TEXT NOT NULL DEFAULT 'markdown', sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL, metadata TEXT NOT NULL DEFAULT '{}');`)
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(
		`INSERT INTO _vibe_pages (id, title, content, created_at, updated_at) VALUES ('old1', 'Old Page', '# hi', 'now', 'now');`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var title, migratedTo string
	require.NoError(t, c2.SQLiteDB().QueryRow(
		`SELECT title, migrated_to FROM _vibe_pages WHERE id = 'old1'`,
	).Scan(&title, &migratedTo))
	assert.Equal(t, "Old Page", title)
	assert.Equal(t, "", migratedTo)
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

// TestDirty_MarkAndSave: a fresh container is clean; MarkDirty reports true
// on the clean->dirty edge and false on a repeat call; Dirty() reflects it;
// Save() clears it.
func TestDirty_MarkAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dirty.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	assert.False(t, c.Dirty())
	assert.True(t, c.MarkDirty(), "the first call must report the clean->dirty edge")
	assert.False(t, c.MarkDirty(), "a repeat call while already dirty reports no edge")
	assert.True(t, c.Dirty())

	require.NoError(t, c.Save())
	assert.False(t, c.Dirty())
}

// TestDirty_ReadOnlyNeverDirty: MarkDirty is a no-op on a read-only container.
func TestDirty_ReadOnlyNeverDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dirty-ro.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithReadOnly(true))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	assert.False(t, c2.MarkDirty())
	assert.False(t, c2.Dirty())
}

// TestDirty_SaveFailureKeepsDirty: a Save that fails to checkpoint (the
// DuckDB connection is already closed out from under it) must leave the
// dirty flag set rather than silently clearing it.
func TestDirty_SaveFailureKeepsDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dirty-fail.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	c.MarkDirty()
	require.NoError(t, c.DuckDBDB().Close())

	assert.Error(t, c.Save())
	assert.True(t, c.Dirty(), "a failed save must leave the file dirty")
}

// TestCreate_TemplateTablesDurableWithoutSave: Create(WithTemplate(...))
// must save once after seeding, so a crash right after Create (no explicit
// Save) still leaves the template's tables on disk.
func TestCreate_TemplateTablesDurableWithoutSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "template.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithTemplate("contacts"))
	require.NoError(t, err)
	require.NoError(t, c.Close()) // no explicit Save

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var count int
	require.NoError(t, c2.DuckDBDB().QueryRow(`SELECT COUNT(*) FROM contacts`).Scan(&count))
	assert.Greater(t, count, 0, "the template's seeded rows must survive a Close with no explicit Save")
}
