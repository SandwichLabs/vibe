package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandwich-labs/vibe-app/internal/crud"
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

// TestAutoNumberSurvivesForeignKeyRebuild guards the W3 commit path: tables are
// created first, then AddForeignKey REBUILDS the child table — the rebuild must
// preserve the sequence DEFAULT or id-less inserts break right after linking.
func TestAutoNumberSurvivesForeignKeyRebuild(t *testing.T) {
	app := newOpenApp(t)

	require.NoError(t, app.CreateTable("customers", []ColumnInfo{
		{Name: "id", Type: "BIGINT", PK: true},
		{Name: "name", Type: "VARCHAR"},
	}))
	require.NoError(t, app.CreateTable("service_calls", []ColumnInfo{
		{Name: "id", Type: "BIGINT", PK: true},
		{Name: "customer_id", Type: "BIGINT"},
		{Name: "notes", Type: "VARCHAR"},
	}))
	_, err := app.AddForeignKey(ForeignKeySpec{
		ChildTable: "service_calls", ChildColumn: "customer_id",
		ParentTable: "customers", ParentColumn: "id",
		OnDelete: "RESTRICT", OnUpdate: "NO ACTION",
	})
	require.NoError(t, err)

	_, err = app.InsertRow("customers", map[string]any{"name": "Acme"})
	require.NoError(t, err)
	// The rebuilt child table must still auto-number.
	_, err = app.InsertRow("service_calls", map[string]any{"customer_id": "1", "notes": "compressor click"})
	require.NoError(t, err, "child insert without id must auto-number after FK rebuild")

	rs, err := app.ExecuteQuery("SELECT id FROM service_calls")
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)
	assert.Equal(t, "1", toStr(rs.Rows[0][0]))
}

// TestSeededEmailValidation: CreateTable seeds field meta for recognizable
// columns — email gets a server-enforced format rule through the crud path.
func TestSeededEmailValidation(t *testing.T) {
	app := newOpenApp(t)

	require.NoError(t, app.CreateTable("customers", []ColumnInfo{
		{Name: "id", Type: "BIGINT", PK: true},
		{Name: "name", Type: "VARCHAR"},
		{Name: "email", Type: "VARCHAR"},
		{Name: "phone", Type: "VARCHAR"},
		{Name: "visited", Type: "DATE"},
	}))

	// Bad email is rejected with a field-keyed validation error.
	_, err := app.InsertRow("customers", map[string]any{"name": "Acme", "email": "not-an-email"})
	require.Error(t, err)
	we, ok := err.(*crud.WriteError)
	require.True(t, ok, "expected a field-keyed WriteError, got %T: %v", err, err)
	assert.Contains(t, we.Fields, "email")
	assert.Contains(t, we.Fields["email"], "valid email")

	// Good email passes.
	_, err = app.InsertRow("customers", map[string]any{"name": "Acme", "email": "ops@acme.com"})
	require.NoError(t, err)

	// Blank email passes — format rules judge entered values; blankness is
	// Required's domain.
	_, err = app.InsertRow("customers", map[string]any{"name": "Beta", "email": ""})
	require.NoError(t, err)

	// Partial write that doesn't touch email must not trip the email rule.
	_, err = app.InsertRow("customers", map[string]any{"name": "Gamma"})
	require.NoError(t, err)

	// Control types were inferred.
	metas, err := app.GetFieldMeta("customers")
	require.NoError(t, err)
	byCol := map[string]string{}
	captions := map[string]string{}
	for _, m := range metas {
		byCol[m.ColumnName] = m.ControlType
		captions[m.ColumnName] = m.Caption
	}
	assert.Equal(t, "email", byCol["email"])
	assert.Equal(t, "tel", byCol["phone"])
	assert.Equal(t, "date", byCol["visited"])
	assert.Equal(t, "Name", captions["name"])
}

// toStr renders a scanned cell for assertion regardless of driver type.
func toStr(v any) string {
	return fmt.Sprintf("%v", v)
}
