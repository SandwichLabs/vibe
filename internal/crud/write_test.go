package crud

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertUpdateDeleteRow_OwnTransaction(t *testing.T) {
	db := hubWithOther(t)
	const tbl = "other.main.items"
	types, err := ColumnTypes(db, tbl)
	require.NoError(t, err)

	res, err := InsertRow(db, types, tbl, map[string]any{"id": "1", "qty": "5"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.Affected)
	_, err = UpdateRow(db, types, tbl, "id", "1", map[string]any{"qty": "9"})
	require.NoError(t, err)
	var qty int
	require.NoError(t, db.QueryRow("SELECT qty FROM other.main.items WHERE id = 1").Scan(&qty))
	assert.Equal(t, 9, qty)
	_, err = DeleteRow(db, types, tbl, "id", "1")
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM other.main.items").Scan(&n))
	assert.Equal(t, 0, n)
}

func TestRowWrites_ARefusedWriteRollsBackAndLeavesNoTransactionOpen(t *testing.T) {
	db := hubWithOther(t)
	const tbl = "other.main.items"
	types, err := ColumnTypes(db, tbl)
	require.NoError(t, err)
	_, err = InsertRow(db, types, tbl, map[string]any{"id": "1", "qty": "5"})
	require.NoError(t, err)

	// A duplicate key is refused as a structured write error.
	_, err = InsertRow(db, types, tbl, map[string]any{"id": "1", "qty": "6"})
	var we *WriteError
	require.ErrorAs(t, err, &we)
	// A 0-row outcome (no such key) is refused too.
	_, err = UpdateRow(db, types, tbl, "id", "99", map[string]any{"qty": "1"})
	require.Error(t, err)
	_, err = DeleteRow(db, types, tbl, "id", "99")
	require.Error(t, err)

	// Nothing was left half-done, and the connection is usable for the next write.
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM other.main.items").Scan(&n))
	assert.Equal(t, 1, n)
	_, err = InsertRow(db, types, tbl, map[string]any{"id": "2", "qty": "7"})
	require.NoError(t, err)
}
