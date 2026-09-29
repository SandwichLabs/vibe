package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevert_DiscardsUnsavedTableChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	_, err = c.DuckDBDB().Exec("CREATE TABLE t1 (id INTEGER, v TEXT);")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("INSERT INTO t1 VALUES (1, 'saved');")
	require.NoError(t, err)
	require.NoError(t, c.Save())

	_, err = c.DuckDBDB().Exec("INSERT INTO t1 VALUES (2, 'unsaved');")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("CREATE TABLE t2 (x INTEGER);")
	require.NoError(t, err)
	c.MarkDirty()

	require.NoError(t, c.Revert())

	assert.False(t, c.Dirty())

	var count int
	require.NoError(t, c.DuckDBDB().QueryRow("SELECT count(*) FROM t1").Scan(&count))
	assert.Equal(t, 1, count)

	var name string
	err = c.DuckDBDB().QueryRow(
		"SELECT table_name FROM information_schema.tables WHERE table_name = 't2'",
	).Scan(&name)
	assert.Error(t, err, "t2 should not exist after revert")
}

func TestRevert_NeverSavedFileRevertsToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	_, err = c.DuckDBDB().Exec("CREATE TABLE t1 (id INTEGER);")
	require.NoError(t, err)

	require.NoError(t, c.Revert())

	var name string
	err = c.DuckDBDB().QueryRow(
		"SELECT table_name FROM information_schema.tables WHERE table_name = 't1'",
	).Scan(&name)
	assert.Error(t, err, "t1 should not exist after reverting a never-saved file")
}

func TestRevert_SQLiteContentSurvives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	require.NoError(t, c.Save())
	require.NoError(t, c.SetMeta("qa98-key", "qa98-value"))

	require.NoError(t, c.Revert())

	val, err := c.Meta("qa98-key")
	require.NoError(t, err)
	assert.Equal(t, "qa98-value", val)
}

func TestRevert_KeepsWriterLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)

	require.NoError(t, c.Revert())

	_, err = Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	assert.ErrorIs(t, err, ErrFileInUse)

	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c2.Close())
}

func TestRevert_OldDBClosedNewDBLive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	require.NoError(t, c.Save())

	old := c.DuckDBDB()
	oldTempDir := c.DuckDBTempDir()

	require.NoError(t, c.Revert())

	assert.Error(t, old.Ping())
	assert.NoError(t, c.DuckDBDB().Ping())
	assert.NotEqual(t, oldTempDir, c.DuckDBTempDir())

	_, statErr := os.Stat(oldTempDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRevert_ReadOnlyRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	ro, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithReadOnly(true))
	require.NoError(t, err)
	defer ro.Close() //nolint:errcheck

	err = ro.Revert()
	assert.ErrorContains(t, err, "read-only")
}

func TestRevert_ClosedContainerErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	err = c.Revert()
	assert.Error(t, err)
}

func TestRevert_SaveAfterRevertPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)

	require.NoError(t, c.Save())
	require.NoError(t, c.Revert())

	_, err = c.DuckDBDB().Exec("CREATE TABLE t1 (id INTEGER);")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("INSERT INTO t1 VALUES (42);")
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var id int
	require.NoError(t, c2.DuckDBDB().QueryRow("SELECT id FROM t1").Scan(&id))
	assert.Equal(t, 42, id)
}

// TestRevert_FailureLeavesOldSessionLive proves Revert is atomic on failure
// (as its doc comment claims): the fresh session is built before the old
// one is touched, so a materialize failure must leave the CURRENT session
// exactly as it was, still answering queries, rather than half-swapped or
// closed.
func TestRevert_FailureLeavesOldSessionLive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	_, err = c.DuckDBDB().Exec("CREATE TABLE t1 (id INTEGER);")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("INSERT INTO t1 VALUES (7);")
	require.NoError(t, err)
	require.NoError(t, c.Save())

	// Corrupt the saved blob directly in SQLCipher so the next
	// openDuckDBSession's materialize fails to open it as a DuckDB file.
	_, err = c.SQLiteDB().Exec(`UPDATE _vibe_duckdb SET data = ? WHERE id = 1;`, []byte("not a duckdb file"))
	require.NoError(t, err)

	before := c.DuckDBDB()

	err = c.Revert()
	require.Error(t, err)

	assert.Same(t, before, c.DuckDBDB(), "the live session must not have been swapped out on failure")

	var id int
	require.NoError(t, c.DuckDBDB().QueryRow("SELECT id FROM t1").Scan(&id))
	assert.Equal(t, 7, id)
}
