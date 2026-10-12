package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/SandwichLabs/vibe/internal/session/sessiontest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func seedTestDB(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE customers (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO customers (id, name, email) VALUES (1, 'Alice', 'alice@example.com')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name, email) VALUES (2, 'Bob', 'bob@example.com')`)
	require.NoError(t, err)
}

// --- Parameter binding tests ---

func TestBindParams_ReplacesPlaceholders(t *testing.T) {
	sql, args, err := BindParams(
		"SELECT * FROM customers WHERE id = $customer_id AND name = $name",
		map[string]any{"customer_id": 42, "name": "Alice"},
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM customers WHERE id = ? AND name = ?", sql)
	assert.Equal(t, []any{42, "Alice"}, args)
}

func TestBindParams_NoPlaceholders(t *testing.T) {
	sql, args, err := BindParams("SELECT * FROM customers", nil)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM customers", sql)
	assert.Nil(t, args)
}

func TestBindParams_DuplicatePlaceholder(t *testing.T) {
	sql, args, err := BindParams(
		"SELECT * FROM t WHERE a = $x OR b = $x",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM t WHERE a = ? OR b = ?", sql)
	assert.Equal(t, []any{1, 1}, args)
}

func TestBindParams_MissingParam(t *testing.T) {
	_, _, err := BindParams(
		"SELECT * FROM t WHERE id = $missing",
		map[string]any{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing parameter")
}

func TestBindParams_SQLInjectionInParamName(t *testing.T) {
	_, _, err := BindParams(
		"SELECT * FROM t WHERE id = $ok",
		map[string]any{"ok": 1, "'; DROP TABLE--": "bad"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid parameter name")
}

func TestBindParams_SQLInjectionAttemptInParamMap(t *testing.T) {
	_, _, err := BindParams(
		"SELECT 1",
		map[string]any{"Robert'); DROP TABLE students;--": 1},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid parameter name")
}

func TestValidateParamName_Valid(t *testing.T) {
	assert.NoError(t, ValidateParamName("customer_id"))
	assert.NoError(t, ValidateParamName("x"))
	assert.NoError(t, ValidateParamName("ABC123"))
}

func TestValidateParamName_Invalid(t *testing.T) {
	assert.Error(t, ValidateParamName(""))
	assert.Error(t, ValidateParamName("foo bar"))
	assert.Error(t, ValidateParamName("foo-bar"))
	assert.Error(t, ValidateParamName("foo;bar"))
}

// --- ResultSet tests ---

func TestResultSet_RowCount(t *testing.T) {
	rs := &ResultSet{
		Columns: []Column{{Name: "id", Type: "int64"}},
		Rows:    [][]any{{1}, {2}, {3}},
	}
	assert.Equal(t, 3, rs.RowCount())
}

func TestResultSet_RowCountEmpty(t *testing.T) {
	rs := &ResultSet{}
	assert.Equal(t, 0, rs.RowCount())
}

func TestResultSet_Value(t *testing.T) {
	rs := &ResultSet{
		Columns: []Column{{Name: "id", Type: "int64"}, {Name: "name", Type: "string"}},
		Rows:    [][]any{{1, "Alice"}, {2, "Bob"}},
	}
	assert.Equal(t, 1, rs.Value(0, 0))
	assert.Equal(t, "Bob", rs.Value(1, 1))
}

func TestResultSet_ValueOutOfBounds(t *testing.T) {
	rs := &ResultSet{
		Columns: []Column{{Name: "id", Type: "int64"}},
		Rows:    [][]any{{1}},
	}
	assert.Nil(t, rs.Value(-1, 0))
	assert.Nil(t, rs.Value(5, 0))
	assert.Nil(t, rs.Value(0, 5))
	assert.Nil(t, rs.Value(0, -1))
}

func TestResultSet_ColumnIndex(t *testing.T) {
	rs := &ResultSet{
		Columns: []Column{
			{Name: "id", Type: "int64"},
			{Name: "name", Type: "string"},
			{Name: "email", Type: "string"},
		},
	}
	assert.Equal(t, 0, rs.ColumnIndex("id"))
	assert.Equal(t, 1, rs.ColumnIndex("name"))
	assert.Equal(t, 2, rs.ColumnIndex("email"))
	assert.Equal(t, -1, rs.ColumnIndex("nonexistent"))
}

// --- Manager tests ---

func TestManager_NilDuckDB(t *testing.T) {
	m := NewManager(nil, WithLogger(slog.Default()))

	ctx := context.Background()
	_, err := m.Execute(ctx, "SELECT 1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestManager_WithDuckDB(t *testing.T) {
	// An in-memory DuckDB handle.
	duckDB := openTestDB(t)
	m := NewManager(sessiontest.ForTests(duckDB))

	assert.NotNil(t, m.duckdb)
}

// --- DuckDB engine integration tests ---

func TestDuckDBEngine_Execute(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	eng := NewDuckDBEngine(sessiontest.ForTests(db))
	ctx := context.Background()

	rs, err := eng.Execute(ctx, "SELECT id, name, email FROM customers ORDER BY id", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, rs.RowCount())
	assert.Equal(t, 3, len(rs.Columns))
	assert.Equal(t, "id", rs.Columns[0].Name)
	assert.Equal(t, "name", rs.Columns[1].Name)
	assert.Equal(t, "email", rs.Columns[2].Name)

	assert.Equal(t, int32(1), rs.Value(0, 0)) // DuckDB INTEGER
	assert.Equal(t, "Alice", rs.Value(0, 1))
	assert.Equal(t, "alice@example.com", rs.Value(0, 2))
}

func TestDuckDBEngine_ExecuteWithParams(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	eng := NewDuckDBEngine(sessiontest.ForTests(db))
	ctx := context.Background()

	rs, err := eng.Execute(ctx, "SELECT name FROM customers WHERE id = $customer_id", map[string]any{
		"customer_id": 1,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, rs.RowCount())
	assert.Equal(t, "Alice", rs.Value(0, 0))
}

func TestManager_ExecuteViaManager(t *testing.T) {
	db := openTestDB(t)
	seedTestDB(t, db)

	m := NewManager(sessiontest.ForTests(db), WithLogger(slog.Default()))
	ctx := context.Background()

	rs, err := m.Execute(ctx, "SELECT name FROM customers WHERE id = $id", map[string]any{
		"id": 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, rs.RowCount())
	assert.Equal(t, "Bob", rs.Value(0, 0))
}

// --- ExecuteCapped tests ---

func TestDuckDBEngine_ExecuteCapped(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(sessiontest.ForTests(db))
	ctx := context.Background()

	rs, total, err := eng.ExecuteCapped(ctx, "SELECT * FROM range(10)", nil, 3)
	require.NoError(t, err)
	assert.Equal(t, 3, rs.RowCount())
	assert.Equal(t, 10, total)

	rs, total, err = eng.ExecuteCapped(ctx, "SELECT * FROM range(10)", nil, 0)
	require.NoError(t, err)
	assert.Equal(t, 10, rs.RowCount())
	assert.Equal(t, 10, total)

	rs, total, err = eng.ExecuteCapped(ctx, "CREATE TABLE cap_ddl (x INTEGER)", nil, 3)
	require.NoError(t, err)
	assert.Equal(t, 0, rs.RowCount())
	assert.Equal(t, 0, total)
}

func TestManager_ExecuteCapped(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := NewManager(sessiontest.ForTests(db), WithLogger(slog.Default()))
	ctx := context.Background()

	rs, total, err := m.ExecuteCapped(ctx, "SELECT * FROM range(6000)", nil, 5000)
	require.NoError(t, err)
	assert.Equal(t, 5000, rs.RowCount())
	assert.Equal(t, 6000, total)
}

func TestDuckDBEngine_Close(t *testing.T) {
	db := openTestDB(t)
	eng := NewDuckDBEngine(sessiontest.ForTests(db))
	assert.NoError(t, eng.Close())
}
