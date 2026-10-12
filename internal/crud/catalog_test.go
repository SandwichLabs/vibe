package crud

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

// hubWithOther returns the hub's pool with catalog "other" attached holding
// items(id PK, qty INTEGER); the default catalog holds items(sku VARCHAR).
func hubWithOther(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	s, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	path := filepath.Join(dir, "other.duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = f.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, qty INTEGER)")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.Attach(context.Background(), session.Spec{Name: "other", Path: path}))
	_, err = s.DB().Exec("CREATE TABLE items (sku VARCHAR)")
	require.NoError(t, err)
	return s.DB()
}

func TestColumnTypes_QualifiedAndUnqualified(t *testing.T) {
	db := hubWithOther(t)

	def, err := ColumnTypes(db, "items")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"sku": "VARCHAR"}, def, "a bare name is the default catalog's table")

	other, err := ColumnTypes(db, "other.main.items")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"id": "INTEGER", "qty": "INTEGER"}, other)

	_, err = ColumnTypes(db, "other.main.nope")
	assert.Error(t, err)
	_, err = ColumnTypes(db, "ghost.main.items")
	assert.Error(t, err)
}

func TestWriteCore_OnSecondCatalog(t *testing.T) {
	db := hubWithOther(t)
	const tbl = "other.main.items"
	types, err := ColumnTypes(db, tbl)
	require.NoError(t, err)

	tx, err := db.Begin()
	require.NoError(t, err)
	res, err := InsertRowTx(tx, types, tbl, map[string]any{"id": "1", "qty": "5"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.Affected)
	_, err = UpdateRowTx(tx, types, tbl, "id", "1", map[string]any{"qty": "9"})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	var qty int
	require.NoError(t, db.QueryRow("SELECT qty FROM other.main.items WHERE id = 1").Scan(&qty))
	assert.Equal(t, 9, qty)
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&n))
	assert.Equal(t, 0, n, "the default catalog's items is untouched")

	tx, err = db.Begin()
	require.NoError(t, err)
	_, err = DeleteRowTx(tx, types, tbl, "id", "1")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM other.main.items").Scan(&n))
	assert.Equal(t, 0, n)

	// An injection attempt in a table part stays one quoted identifier.
	tx, err = db.Begin()
	require.NoError(t, err)
	_, err = InsertRowTx(tx, types, `other.main."items"; DROP TABLE items; --`, map[string]any{"id": "2"})
	assert.Error(t, err)
	_ = tx.Rollback()
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'items'").Scan(&n))
	assert.Equal(t, 2, n)
}

// DuckDB reports estimated_size for a table of an attached DuckDB catalog (the
// TUI and the Studio rail show it as an approximate count); a table with no rows
// has no estimate.
func TestEstimatedRows_AttachedCatalog(t *testing.T) {
	db := hubWithOther(t)
	_, err := db.Exec(`INSERT INTO other.main.items SELECT range, range FROM range(3000)`)
	require.NoError(t, err)
	est := session.EstimatedRows(context.Background(), db, "other")
	assert.Equal(t, int64(3000), est[session.TableKey{Schema: "main", Name: "items"}])
	assert.NotContains(t, session.EstimatedRows(context.Background(), db, "nope"), session.TableKey{Schema: "main", Name: "items"})
}
