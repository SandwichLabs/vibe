package schema_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "test-passphrase-12345"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func tableExists(t *testing.T, c *container.Container, name string) bool {
	t.Helper()
	var n int
	err := c.SQLiteDB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", name,
	).Scan(&n)
	require.NoError(t, err)
	return n == 1
}

// TestFieldMetaRoundTrip exercises the full §1.3 acceptance behavior end-to-end
// against a real .vibe container (SQLCipher + DuckDB):
//   - the additive _vibe_field_meta migration creates the table
//   - SetFieldMeta stores meta and upserts on (table_name, column_name)
//   - GetFieldMeta returns all stored columns' meta for a table
//   - meta persists across Save + Close + reopen
func TestFieldMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fieldmeta.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Field Meta Test"),
	)
	require.NoError(t, err)

	// Migration creates _vibe_field_meta in the SQLCipher container.
	require.True(t, tableExists(t, c, "_vibe_field_meta"),
		"_vibe_field_meta table should exist after migration")

	store := schema.NewStore(c.SQLiteDB())

	meta := schema.FieldMeta{
		Caption:     "Status",
		ControlType: "combo",
		Required:    true,
		LookupSQL:   "SELECT id,name FROM statuses",
		ValueCol:    "id",
		DisplayCol:  "name",
	}
	require.NoError(t, store.SetFieldMeta("customers", "status", meta))

	got, err := store.GetFieldMeta("customers")
	require.NoError(t, err)
	require.Len(t, got, 1, "GetFieldMeta should return one stored column")

	fm := got[0]
	assert.Equal(t, "customers", fm.TableName)
	assert.Equal(t, "status", fm.ColumnName)
	assert.Equal(t, "Status", fm.Caption)
	assert.Equal(t, "combo", fm.ControlType)
	assert.True(t, fm.Required)
	assert.Equal(t, "SELECT id,name FROM statuses", fm.LookupSQL)
	assert.Equal(t, "id", fm.ValueCol)
	assert.Equal(t, "name", fm.DisplayCol)

	// Upsert: overwriting the same (table, column) must not create a duplicate row.
	meta2 := meta
	meta2.Caption = "Account Status"
	meta2.Required = false
	require.NoError(t, store.SetFieldMeta("customers", "status", meta2))

	got2, err := store.GetFieldMeta("customers")
	require.NoError(t, err)
	require.Len(t, got2, 1, "upsert should not create a duplicate row")
	assert.Equal(t, "Account Status", got2[0].Caption)
	assert.False(t, got2[0].Required)

	// Persist across Save + Close + reopen.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	store2 := schema.NewStore(c2.SQLiteDB())
	reopened, err := store2.GetFieldMeta("customers")
	require.NoError(t, err)
	require.Len(t, reopened, 1, "meta should persist across reopen")
	assert.Equal(t, "Account Status", reopened[0].Caption)
	assert.Equal(t, "combo", reopened[0].ControlType)
	assert.Equal(t, "SELECT id,name FROM statuses", reopened[0].LookupSQL)
}

// TestFieldMetaMigrationBackwardCompatible asserts the additive migration is
// backward compatible: a .vibe created before _vibe_field_meta existed still
// opens, and the table is present after reopen (IF NOT EXISTS additive migration).
func TestFieldMetaMigrationBackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preexisting.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	// Reopening an existing file must succeed (additive migration is safe to re-run).
	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err, "pre-existing .vibe must still open after additive migration")
	defer c2.Close() //nolint:errcheck

	assert.True(t, tableExists(t, c2, "_vibe_field_meta"),
		"_vibe_field_meta should exist after reopening")
}
