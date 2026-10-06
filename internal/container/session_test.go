package container

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandwich-labs/vibe-app/internal/session"
)

func settingOf(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v sql.NullString
	require.NoError(t, db.QueryRow("SELECT CAST(current_setting('"+name+"') AS VARCHAR)").Scan(&v))
	return v.String
}

// The container's DuckDB is a hardened session: its default catalog is the
// materialised file, under a stable name, and every hardening option reads
// back.
func TestSession_HardenedAndNamedCatalog(t *testing.T) {
	c, err := Create(filepath.Join(t.TempDir(), "s.vibe"), WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	sess := c.Session()
	require.NotNil(t, sess)
	assert.Equal(t, "analytics", sess.DefaultCatalog(), "the legacy container's data catalog has a stable name")
	db := c.DuckDBDB()
	assert.Same(t, db, sess.DB())

	assert.Equal(t, "false", settingOf(t, db, "allow_persistent_secrets"))
	assert.Equal(t, "false", settingOf(t, db, "autoinstall_known_extensions"))
	assert.Equal(t, "false", settingOf(t, db, "autoload_known_extensions"))
	assert.Equal(t, "false", settingOf(t, db, "allow_community_extensions"))
	assert.Equal(t, "true", settingOf(t, db, "lock_configuration"))
	assert.Equal(t, filepath.Join(c.DuckDBTempDir(), "spill"), filepath.Clean(settingOf(t, db, "temp_directory")),
		"spill files live inside the container's 0700 temp dir")
	ext, _ := session.DefaultDirs()
	assert.Equal(t, ext, filepath.Clean(settingOf(t, db, "extension_directory")))

	_, err = db.Exec("CREATE TABLE people (id INTEGER)")
	require.NoError(t, err)
	tabs, err := sess.ListTables(context.Background(), "", "")
	require.NoError(t, err)
	require.Len(t, tabs, 1)
	assert.Equal(t, "analytics", tabs[0].Catalog)
	assert.Equal(t, "people", tabs[0].Name)
}

// Revert re-materialises the file: the session is rebuilt over the new
// instance, keeps the catalog name and drops unsaved changes. Save still
// round-trips through the session's instance.
func TestSession_SurvivesSaveAndRevert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	_, err = c.Session().Exec(context.Background(), "CREATE TABLE kept (id INTEGER)")
	require.NoError(t, err)
	require.NoError(t, c.Save())
	_, err = c.Session().Exec(context.Background(), "CREATE TABLE lost (id INTEGER)")
	require.NoError(t, err)

	before := c.Session()
	require.NoError(t, c.Revert())
	after := c.Session()
	assert.NotSame(t, before, after, "a new session over the re-materialised file")
	assert.Equal(t, "analytics", after.DefaultCatalog())
	assert.Same(t, c.DuckDBDB(), after.DB())

	tabs, err := after.ListTables(context.Background(), "", "")
	require.NoError(t, err)
	require.Len(t, tabs, 1, "the schema cache did not outlive the revert")
	assert.Equal(t, "kept", tabs[0].Name)
	assert.Equal(t, "true", settingOf(t, c.DuckDBDB(), "lock_configuration"), "the new instance is hardened too")

	// A second catalog attaches next to the container's data.
	other := filepath.Join(t.TempDir(), "other.duckdb")
	f, err := sql.Open("duckdb", other)
	require.NoError(t, err)
	_, err = f.Exec("CREATE TABLE o (x INTEGER); INSERT INTO o VALUES (1)")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, after.Attach(context.Background(), session.Spec{Name: "other", Path: other, ReadOnly: true}))
	var n int
	require.NoError(t, c.DuckDBDB().QueryRow("SELECT (SELECT count(*) FROM other.main.o) + (SELECT count(*) FROM kept)").Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, c.Save(), "checkpointing with an attached catalog still works")
}

// Close releases the temp dir (spill included) and the session.
func TestSession_CloseRemovesTempDir(t *testing.T) {
	c, err := Create(filepath.Join(t.TempDir(), "s.vibe"), WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	dir := c.DuckDBTempDir()
	require.DirExists(t, dir)
	require.NoError(t, c.Close())
	assert.NoDirExists(t, dir)
	assert.Nil(t, c.Session())
}
