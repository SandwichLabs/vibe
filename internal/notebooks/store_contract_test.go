package notebooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeContract runs the behaviour every Store must share against the file
// store and the in-memory Mock. (These cases once ran against the legacy
// _vibe_notebooks store.)
func storeContract(t *testing.T, run func(t *testing.T, s Store)) {
	t.Run("file", func(t *testing.T) { run(t, NewFileStore(t.TempDir())) })
	t.Run("mock", func(t *testing.T) { run(t, &Mock{}) })
}

func TestStore_CreateGetList(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		nb, err := s.Create(ctx, "My Notebook")
		require.NoError(t, err)
		assert.NotEmpty(t, nb.ID)

		got, warnings, err := s.Get(ctx, nb.ID)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, "My Notebook", got.Title)
		assert.Equal(t, nb.ID, got.ID)

		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, nb.ID, list[0].ID)
		assert.Equal(t, 0, list[0].CellCount)
	})
}

func TestStore_SaveUpsertsAndCoerces(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		nb, err := s.Create(ctx, "N")
		require.NoError(t, err)
		raw := map[string]any{
			"id": nb.ID, "title": "Renamed", "rev": nb.Rev,
			"cells": []any{
				map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "malicious": "bad"},
			},
		}
		saved, warnings, err := s.Save(ctx, raw)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, "Renamed", saved.Title)
		require.Len(t, saved.Cells, 1)

		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1) // upsert, not a second notebook
		assert.Equal(t, 1, list[0].CellCount)
	})
}

func TestStore_SaveRejectsMalformedInput(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		_, _, err := s.Save(ctx, map[string]any{"title": "no cells here"})
		assert.Error(t, err)
		list, err := s.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, list, "a rejected save must not write")
	})
}

func TestStore_Rename(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		nb, err := s.Create(ctx, "Old")
		require.NoError(t, err)
		require.NoError(t, s.Rename(ctx, nb.ID, "New"))
		got, _, err := s.Get(ctx, nb.ID)
		require.NoError(t, err)
		assert.Equal(t, "New", got.Title)
		assert.Error(t, s.Rename(ctx, "does-not-exist", "x"))
	})
}

func TestStore_Delete(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		nb, err := s.Create(ctx, "Gone soon")
		require.NoError(t, err)
		require.NoError(t, s.Delete(ctx, nb.ID))
		list, err := s.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, list)
	})
}

func TestStore_Insert(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		nb := Empty("Direct insert")
		cell, err := NewCell(CellMarkdown)
		require.NoError(t, err)
		cell.Source = "hello"
		nb.Cells = append(nb.Cells, cell)

		saved, warnings, err := s.Insert(ctx, nb)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		require.Len(t, saved.Cells, 1)

		got, _, err := s.Get(ctx, saved.ID)
		require.NoError(t, err)
		assert.Equal(t, "hello", got.Cells[0].Source)
	})
}
