package queries

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const createTable = `CREATE TABLE _vibe_queries (
	id         TEXT PRIMARY KEY,
	title      TEXT NOT NULL DEFAULT '',
	sql        TEXT NOT NULL DEFAULT '',
	engine     TEXT NOT NULL DEFAULT 'sqlite',
	parameters TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(createTable)
	require.NoError(t, err)

	return db
}

func newTestRegistry(t *testing.T) (*Registry, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	logger := slog.Default()
	return NewRegistry(db, logger), db
}

func TestCRUDLifecycle(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	// Create
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
	err := reg.Create(ctx, q)
	require.NoError(t, err)
	assert.NotEmpty(t, q.CreatedAt)
	assert.NotEmpty(t, q.UpdatedAt)

	// Get
	got, err := reg.Get(ctx, "revenue-by-month")
	require.NoError(t, err)
	assert.Equal(t, q.ID, got.ID)
	assert.Equal(t, q.Title, got.Title)
	assert.Equal(t, q.SQL, got.SQL)
	assert.Equal(t, q.Engine, got.Engine)
	assert.Len(t, got.Parameters, 2)
	assert.Equal(t, "year", got.Parameters[0].Name)
	assert.True(t, got.Parameters[0].Required)
	assert.Equal(t, "all", got.Parameters[1].Default)

	// List
	queries, err := reg.List(ctx)
	require.NoError(t, err)
	assert.Len(t, queries, 1)
	assert.Equal(t, "revenue-by-month", queries[0].ID)

	// Update
	q.Title = "Monthly Revenue Report"
	q.SQL = "SELECT month, SUM(revenue) FROM sales GROUP BY month"
	q.Parameters = []Param{
		{Name: "year", Type: "integer", Required: true},
	}
	err = reg.Update(ctx, q)
	require.NoError(t, err)
	assert.NotEmpty(t, q.UpdatedAt)

	got, err = reg.Get(ctx, "revenue-by-month")
	require.NoError(t, err)
	assert.Equal(t, "Monthly Revenue Report", got.Title)
	assert.Len(t, got.Parameters, 1)

	// Delete
	err = reg.Delete(ctx, "revenue-by-month")
	require.NoError(t, err)

	_, err = reg.Get(ctx, "revenue-by-month")
	assert.Error(t, err)
}

func TestGetNonExistent(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	_, err := reg.Get(ctx, "does-not-exist")
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateDuplicate(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	q := &Query{
		ID:         "my-query",
		Title:      "My Query",
		SQL:        "SELECT 1",
		Engine:     "sqlite",
		Parameters: []Param{},
	}
	err := reg.Create(ctx, q)
	require.NoError(t, err)

	dup := &Query{
		ID:         "my-query",
		Title:      "Duplicate",
		SQL:        "SELECT 2",
		Engine:     "sqlite",
		Parameters: []Param{},
	}
	err = reg.Create(ctx, dup)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create query")
}

func TestListEmpty(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	queries, err := reg.List(ctx)
	require.NoError(t, err)
	assert.NotNil(t, queries)
	assert.Empty(t, queries)
}

func TestUpdateNonExistent(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	q := &Query{
		ID:         "ghost",
		Title:      "Ghost Query",
		SQL:        "SELECT 1",
		Engine:     "sqlite",
		Parameters: []Param{},
	}
	err := reg.Update(ctx, q)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteNonExistent(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	err := reg.Delete(ctx, "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
