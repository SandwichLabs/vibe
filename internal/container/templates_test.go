package container

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedTemplateTables maps each registered starter template to the user-data
// tables (and a representative subset of columns) it must seed into DuckDB.
var expectedTemplateTables = map[string]map[string][]string{
	"contacts": {
		"contacts": {"id", "name"},
	},
	"inventory": {
		"products":  {"id", "name"},
		"suppliers": {"id", "name"},
	},
	"order-entry": {
		"customers":   {"id", "name"},
		"orders":      {"id", "customer_id"},
		"order_items": {"id", "order_id"},
	},
	"training-registrations": {
		"courses":       {"id", "title"},
		"registrations": {"id", "course_id"},
	},
}

// expectedTemplateRelationships maps each template to the plain-SQL inline
// foreign keys (child.column -> parent.column) its CREATE TABLE statements
// declare (#68 dropped the _vibe_relationships metadata table; the FK itself
// is still real DuckDB DDL, so it's asserted directly against the known
// template schema rather than reparsed out of DuckDB's catalog).
var expectedTemplateRelationships = map[string][]tmplRel{
	"contacts": {
		{parentTable: "contacts", parentColumn: "id", childTable: "contact_notes", childColumn: "contact_id"},
	},
	"inventory": {
		{parentTable: "suppliers", parentColumn: "id", childTable: "products", childColumn: "supplier_id"},
	},
	"order-entry": {
		{parentTable: "customers", parentColumn: "id", childTable: "orders", childColumn: "customer_id"},
		{parentTable: "orders", parentColumn: "id", childTable: "order_items", childColumn: "order_id"},
	},
	"training-registrations": {
		{parentTable: "courses", parentColumn: "id", childTable: "registrations", childColumn: "course_id"},
	},
}

// TestListTemplates asserts the registry advertises at least the four
// starter templates by name.
func TestListTemplates(t *testing.T) {
	names := ListTemplates()
	require.GreaterOrEqual(t, len(names), 3, "expected at least 3 registered templates")

	for _, want := range []string{"contacts", "inventory", "order-entry", "training-registrations"} {
		assert.Contains(t, names, want, "template %q should be registered", want)
	}
}

// TestCreate_UnknownTemplate_Error asserts an unknown template name produces a
// clear, wrapped error.
func TestCreate_UnknownTemplate_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unknown.vibe")

	_, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTemplate("does-not-exist"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
	assert.Contains(t, strings.ToLower(err.Error()), "template")
}

// TestCreate_WithTemplate_SeedsRelationalApp builds every registered template
// into a fresh encrypted .vibe, reopens it, and asserts the full acceptance
// shape (#68: tables + seed data + a runnable notebook, no forms/nav/macros/
// relationships metadata): relational tables with expected columns and
// enforced inline FK constraints, non-zero sample data, and exactly one
// starter notebook with at least one sql cell.
func TestCreate_WithTemplate_SeedsRelationalApp(t *testing.T) {
	for name, tables := range expectedTemplateTables {
		name, tables := name, tables
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name+".vibe")

			c, err := Create(path,
				WithPassphrase(testPassphrase),
				WithLogger(testLogger()),
				WithTemplate(name),
			)
			require.NoError(t, err, "building template %q should succeed", name)
			require.NoError(t, c.Save())
			require.NoError(t, c.Close())

			c2, err := Open(path,
				WithPassphrase(testPassphrase),
				WithLogger(testLogger()),
			)
			require.NoError(t, err)
			defer c2.Close() //nolint:errcheck

			ddb := c2.DuckDBDB()
			require.NotNil(t, ddb)

			// 1. Expected tables exist with expected columns, seeded with data.
			for table, cols := range tables {
				for _, col := range cols {
					var n int
					err := ddb.QueryRow(
						`SELECT COUNT(*) FROM information_schema.columns
						  WHERE table_schema = 'main' AND table_name = ? AND column_name = ?`,
						table, col,
					).Scan(&n)
					require.NoError(t, err)
					assert.Equal(t, 1, n, "table %q should have column %q", table, col)
				}

				var rowCount int
				err := ddb.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&rowCount)
				require.NoError(t, err, "table %q should be queryable", table)
				assert.Greater(t, rowCount, 0, "table %q should have sample data", table)
			}

			// 2. Inline FK relationships (declared at CREATE TABLE time, plain
			// SQL — #68 dropped the _vibe_relationships metadata table) are
			// actually enforced by DuckDB.
			rels := expectedTemplateRelationships[name]
			require.NotEmpty(t, rels, "template %q should declare at least one foreign key", name)
			for _, r := range rels {
				assertParentDeleteBlocked(t, ddb, r)
			}

			// 3. Exactly one starter notebook, with at least one sql cell.
			require.Equal(t, 1, countRows(t, c2.SQLiteDB(), "_vibe_notebooks"),
				"template %q should seed exactly one starter notebook", name)
			var cellsJSON string
			require.NoError(t, c2.SQLiteDB().QueryRow(`SELECT cells FROM _vibe_notebooks LIMIT 1`).Scan(&cellsJSON))
			var cells []map[string]any
			require.NoError(t, json.Unmarshal([]byte(cellsJSON), &cells))
			hasSQL := false
			for _, cell := range cells {
				if cell["type"] == "sql" {
					hasSQL = true
					break
				}
			}
			assert.True(t, hasSQL, "template %q's starter notebook should have at least one sql cell", name)
		})
	}
}

// TestOrderEntryTemplate_NotebookReferencesLineItems asserts the order-entry
// template's starter notebook actually queries the master/detail relationship
// (orders -> order_items) it seeds, not just the top-level tables.
func TestOrderEntryTemplate_NotebookReferencesLineItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order-entry.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTemplate("order-entry"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	var cellsJSON string
	require.NoError(t, c.SQLiteDB().QueryRow(`SELECT cells FROM _vibe_notebooks LIMIT 1`).Scan(&cellsJSON))
	assert.Contains(t, cellsJSON, "order_items", "order-entry notebook should include a cell over the line-item table")
}

// --- helpers ---

type tmplRel struct {
	parentTable, parentColumn string
	childTable, childColumn   string
}

// assertParentDeleteBlocked verifies the FK is actually enforced in DuckDB:
// deleting a referenced parent row must fail with a foreign-key violation.
func assertParentDeleteBlocked(t *testing.T, ddb *sql.DB, r tmplRel) {
	t.Helper()

	var childVal any
	err := ddb.QueryRow(
		"SELECT " + r.childColumn + " FROM " + r.childTable +
			" WHERE " + r.childColumn + " IS NOT NULL LIMIT 1").Scan(&childVal)
	if err == sql.ErrNoRows {
		t.Fatalf("relationship %s.%s -> %s.%s has no referencing child rows to test enforcement",
			r.childTable, r.childColumn, r.parentTable, r.parentColumn)
	}
	require.NoError(t, err)

	_, delErr := ddb.Exec(
		"DELETE FROM "+r.parentTable+" WHERE "+r.parentColumn+" = ?", childVal)
	require.Error(t, delErr,
		"deleting referenced parent %s.%s should be blocked by the enforced FK", r.parentTable, r.parentColumn)
	assert.True(t, isFKViolation(delErr),
		"expected FK violation deleting referenced parent, got: %v", delErr)
}

func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "does not exist") {
		return false
	}
	return strings.Contains(msg, "foreign key") ||
		strings.Contains(msg, "violates foreign key") ||
		(strings.Contains(msg, "constraint") && strings.Contains(msg, "key")) ||
		strings.Contains(msg, "referenced")
}

func countRows(t *testing.T, sdb *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, sdb.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
	return n
}
