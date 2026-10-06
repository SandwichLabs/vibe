package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasColumn reports whether DescribeTable lists a column with the given name.
func hasColumn(info *TableInfo, name string) bool {
	for _, c := range info.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

// columnType returns the data type DescribeTable reports for the named column.
func columnType(info *TableInfo, name string) (string, bool) {
	for _, c := range info.Columns {
		if c.Name == name {
			return c.Type, true
		}
	}
	return "", false
}

// TestAppSchemaDesignerDDL drives the P2.4 Schema Designer DDL bindings against
// a real .vibe container + DuckDB, asserting the full acceptance behavior:
// AddColumn shows up in DescribeTable; a UNIQUE AddIndex enforces uniqueness on
// InsertRow; DropColumn removes the column; AlterColumnType changes a type
// while keeping data readable — all surviving SaveFile + reopen.
func TestAppSchemaDesignerDDL(t *testing.T) {
	app := newOpenApp(t)

	require.NoError(t, app.CreateTable("people", []ColumnInfo{
		{Name: "id", Type: "INTEGER", PK: true},
		{Name: "name", Type: "VARCHAR"},
	}))

	// AddColumn: extend the table with an email column.
	require.NoError(t, app.AddColumn("people", "email", "VARCHAR"))

	info, err := app.DescribeTable("people")
	require.NoError(t, err)
	require.True(t, hasColumn(info, "email"), "AddColumn must surface in DescribeTable")

	// AddIndex (unique): a duplicate email must be rejected on subsequent insert.
	require.NoError(t, app.AddIndex("people", "idx_email", []string{"email"}, true))

	_, err = app.InsertRow("people", map[string]any{"id": 1, "name": "Ada", "email": "ada@example.com"})
	require.NoError(t, err)

	_, err = app.InsertRow("people", map[string]any{"id": 2, "name": "Bob", "email": "ada@example.com"})
	require.Error(t, err, "unique index must reject a duplicate email")

	// AlterColumnType: change name from VARCHAR to VARCHAR(64)-ish (use a real
	// type change that keeps data readable). Add an integer column then widen.
	require.NoError(t, app.AddColumn("people", "score", "INTEGER"))
	_, err = app.InsertRow("people", map[string]any{"id": 3, "name": "Cy", "email": "cy@example.com", "score": 7})
	require.NoError(t, err)
	require.NoError(t, app.AlterColumnType("people", "score", "BIGINT"))

	info, err = app.DescribeTable("people")
	require.NoError(t, err)
	typ, ok := columnType(info, "score")
	require.True(t, ok)
	assert.Contains(t, typ, "BIGINT", "AlterColumnType must change the reported type")

	// Existing data is still readable after the type change.
	data, err := app.GetTableData("people", 1, 50, "", "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, data.TotalRows, 2, "existing rows must survive the type alter")

	// DropColumn: remove the score column.
	require.NoError(t, app.DropColumn("people", "score"))
	info, err = app.DescribeTable("people")
	require.NoError(t, err)
	assert.False(t, hasColumn(info, "score"), "DropColumn must remove the column from DescribeTable")

	// All of the above must survive Save + reopen.
	reopenApp(t, app)

	info, err = app.DescribeTable("people")
	require.NoError(t, err)
	assert.True(t, hasColumn(info, "email"), "added column must persist across reopen")
	assert.False(t, hasColumn(info, "score"), "dropped column must stay dropped across reopen")

	// Unique index still enforced after reopen.
	_, err = app.InsertRow("people", map[string]any{"id": 4, "name": "Dot", "email": "ada@example.com"})
	require.Error(t, err, "unique index must remain enforced after reopen")
}

// TestAppSchemaDesignerValidation verifies the DDL bindings reject invalid
// identifiers with wrapped errors and do not mutate the schema.
func TestAppSchemaDesignerValidation(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{
		{Name: "id", Type: "INTEGER", PK: true},
	}))

	require.Error(t, app.AddColumn("widgets", "bad name; DROP TABLE widgets;--", "VARCHAR"),
		"invalid column identifier must be rejected")
	require.Error(t, app.AddColumn("no_such_table", "x", "VARCHAR"),
		"nonexistent table must be rejected")
	require.Error(t, app.AddIndex("widgets", "idx; DROP", []string{"id"}, false),
		"invalid index name must be rejected")
	require.Error(t, app.DropColumn("widgets", "no_such_col"),
		"dropping a nonexistent column must be rejected")
	require.Error(t, app.AlterColumnType("widgets", "id", "NOT_A_TYPE"),
		"invalid type must be rejected")
}

// TestAppSchemaDesignerNoFileOpen verifies the no-file-open guards mirror the
// other bindings (mutex-guarded, clear wrapped error).
func TestAppSchemaDesignerNoFileOpen(t *testing.T) {
	app := NewApp(testLogger())
	app.startup(context.Background())

	err := app.AddColumn("t", "c", "VARCHAR")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace open")

	err = app.AddIndex("t", "idx", []string{"c"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace open")

	err = app.DropColumn("t", "c")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace open")

	err = app.AlterColumnType("t", "c", "INTEGER")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace open")
}
