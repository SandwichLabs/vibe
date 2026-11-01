package queries

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeContract runs the behaviour every Store must share against the file
// store and the in-memory Mock. (These cases once ran against the legacy
// _vibe_queries Registry.)
func storeContract(t *testing.T, run func(t *testing.T, s Store)) {
	t.Run("file", func(t *testing.T) { run(t, NewFileStore(t.TempDir())) })
	t.Run("mock", func(t *testing.T) { run(t, &Mock{}) })
}

func TestStore_CRUDLifecycle(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		q := &Query{
			ID:     "revenue-by-month",
			Title:  "Revenue by Month",
			SQL:    "SELECT month, SUM(amount) FROM sales GROUP BY month",
			Engine: "duckdb",
			Parameters: []Param{
				{Name: "year", Type: "integer", Required: true},
				{Name: "region", Type: "text", Required: false, Default: "all"},
			},
		}
		require.NoError(t, s.Create(ctx, q))
		assert.NotEmpty(t, q.ID)

		got, err := s.Get(ctx, q.ID)
		require.NoError(t, err)
		assert.Equal(t, q.ID, got.ID)
		assert.Equal(t, q.Title, got.Title)
		assert.Equal(t, q.SQL, got.SQL)
		require.Len(t, got.Parameters, 2)
		assert.Equal(t, "year", got.Parameters[0].Name)
		assert.True(t, got.Parameters[0].Required)
		assert.Equal(t, "all", got.Parameters[1].Default)

		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, q.ID, list[0].ID)

		q.Title = "Monthly Revenue Report"
		q.SQL = "SELECT month, SUM(revenue) FROM sales GROUP BY month"
		q.Parameters = []Param{{Name: "year", Type: "integer", Required: true}}
		require.NoError(t, s.Update(ctx, q))

		got, err = s.Get(ctx, q.ID)
		require.NoError(t, err)
		assert.Equal(t, "Monthly Revenue Report", got.Title)
		assert.Len(t, got.Parameters, 1)

		require.NoError(t, s.Delete(ctx, q.ID))
		_, err = s.Get(ctx, q.ID)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestStore_GetNonExistent(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		_, err := s.Get(context.Background(), "does-not-exist")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestStore_ListEmpty(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		list, err := s.List(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, list)
		assert.Empty(t, list)
	})
}

func TestStore_UpdateAndDeleteNonExistent(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		err := s.Update(ctx, &Query{ID: "ghost", Title: "Ghost", SQL: "SELECT 1", Parameters: []Param{}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		err = s.Delete(ctx, "ghost")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}
