package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateTableAutoNumberPK reproduces the reported bug: an agent/builder
// -created table's integer PK must auto-number, so a data-entry form insert
// that omits (or blanks) the id succeeds instead of failing
// "NOT NULL constraint failed: customers.id".
func TestCreateTableAutoNumberPK(t *testing.T) {
	app := newOpenApp(t)

	require.NoError(t, app.CreateTable("customers", []ColumnInfo{
		{Name: "id", Type: "BIGINT", PK: true, NotNull: true},
		{Name: "name", Type: "VARCHAR", NotNull: true},
		{Name: "email", Type: "VARCHAR"},
	}))

	// Form-style insert: no id at all.
	_, err := app.InsertRow("customers", map[string]any{"name": "Acme", "email": "ops@acme.com"})
	require.NoError(t, err, "insert without id must auto-number")

	// Blank id from an id-bearing form: empty string for a numeric column is a
	// blank control, not a value — the sequence must still fill it.
	_, err = app.InsertRow("customers", map[string]any{"id": "", "name": "Beta", "email": "hi@beta.io"})
	require.NoError(t, err, "insert with blank id must auto-number")

	rs, err := app.ExecuteQuery("SELECT id, name FROM customers ORDER BY id")
	require.NoError(t, err)
	require.Len(t, rs.Rows, 2)
	assert.Equal(t, "1", toStr(rs.Rows[0][0]))
	assert.Equal(t, "2", toStr(rs.Rows[1][0]))
}

// toStr renders a scanned cell for assertion regardless of driver type.
func toStr(v any) string {
	return fmt.Sprintf("%v", v)
}
