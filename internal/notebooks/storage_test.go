package notebooks

import (
	"database/sql"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE _vibe_notebooks (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', cells TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
	)`)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateGetList(t *testing.T) {
	db := testDB(t)

	nb, err := Create(db, "My Notebook")
	require.NoError(t, err)
	assert.NotEmpty(t, nb.ID)

	got, warnings, err := Get(db, nb.ID)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "My Notebook", got.Title)
	assert.Equal(t, nb.ID, got.ID)

	list, err := List(db)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, nb.ID, list[0].ID)
	assert.Equal(t, 0, list[0].CellCount)
}

func TestSave_UpsertsAndCoerces(t *testing.T) {
	db := testDB(t)
	nb, err := Create(db, "N")
	require.NoError(t, err)

	raw := map[string]any{
		"id": nb.ID, "title": "Renamed",
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "malicious": "bad"},
		},
	}
	saved, warnings, err := Save(db, raw)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "Renamed", saved.Title)
	require.Len(t, saved.Cells, 1)

	list, err := List(db)
	require.NoError(t, err)
	require.Len(t, list, 1) // upsert, not a second row
	assert.Equal(t, 1, list[0].CellCount)
}

func TestSave_RejectsMalformedInput(t *testing.T) {
	db := testDB(t)
	_, _, err := Save(db, map[string]any{"title": "no cells here"})
	assert.Error(t, err)

	list, err := List(db)
	require.NoError(t, err)
	assert.Empty(t, list, "a rejected save must not write a row")
}

func TestRename(t *testing.T) {
	db := testDB(t)
	nb, err := Create(db, "Old")
	require.NoError(t, err)

	require.NoError(t, Rename(db, nb.ID, "New"))
	got, _, err := Get(db, nb.ID)
	require.NoError(t, err)
	assert.Equal(t, "New", got.Title)

	assert.Error(t, Rename(db, "does-not-exist", "x"))
}

func TestDelete(t *testing.T) {
	db := testDB(t)
	nb, err := Create(db, "Gone soon")
	require.NoError(t, err)

	require.NoError(t, Delete(db, nb.ID))
	list, err := List(db)
	require.NoError(t, err)
	assert.Empty(t, list)

	// Deleting again is a no-op, not an error.
	assert.NoError(t, Delete(db, nb.ID))
}

func TestGet_RebuildsThroughCoerce(t *testing.T) {
	db := testDB(t)
	// Insert a row directly with a smuggled field and an out-of-range view,
	// bypassing Save, to prove Get() rebuilds through Coerce rather than
	// trusting whatever is in the column.
	_, err := db.Exec(
		`INSERT INTO _vibe_notebooks (id, title, cells, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"nb_x", "Untrusted", `[{"id":"c1","type":"sql","source":"SELECT 1","view":"pie","evil":"<script>"}]`,
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	require.NoError(t, err)

	nb, warnings, err := Get(db, "nb_x")
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, "table", nb.Cells[0].View) // clamped from "pie"
	assert.NotEmpty(t, warnings)
}

func TestInsert(t *testing.T) {
	db := testDB(t)
	nb := Empty("Direct insert")
	cell, err := NewCell(CellMarkdown)
	require.NoError(t, err)
	cell.Source = "hello"
	nb.Cells = append(nb.Cells, cell)

	saved, warnings, err := Insert(db, nb)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, saved.Cells, 1)

	got, _, err := Get(db, saved.ID)
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Cells[0].Source)
}
