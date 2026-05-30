package container

import (
	"database/sql"
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

// TestListTemplates asserts the registry advertises at least the three required
// Access-style starter templates by name.
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
// shape: relational tables with expected columns, enforced FK relationships,
// field metadata, at least one form + nav entry, and non-zero sample data.
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

			// 2. Declared FK relationships present in _vibe_relationships and enforced.
			rels := templateRelationships(t, c2.SQLiteDB())
			require.NotEmpty(t, rels, "template %q should declare at least one relationship", name)

			for _, r := range rels {
				assertParentDeleteBlocked(t, ddb, r)
			}

			// 3. At least one form and one nav entry.
			assert.GreaterOrEqual(t, countRows(t, c2.SQLiteDB(), "_vibe_forms"), 1,
				"template %q should seed at least one form", name)
			assert.GreaterOrEqual(t, countRows(t, c2.SQLiteDB(), "_vibe_nav"), 1,
				"template %q should seed at least one nav entry", name)

			// 4. Field metadata seeded.
			assert.GreaterOrEqual(t, countRows(t, c2.SQLiteDB(), "_vibe_field_meta"), 1,
				"template %q should seed field metadata", name)
		})
	}
}

// TestOrderEntryTemplate_MasterDetailForm asserts the order-entry template
// yields the Phase 1 acceptance pattern: a master-detail form (order header +
// line-item subform) bound on the orders -> order_items FK.
func TestOrderEntryTemplate_MasterDetailForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order-entry.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTemplate("order-entry"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	var (
		archetype string
		tableName string
		spec      string
	)
	err = c.SQLiteDB().QueryRow(
		`SELECT archetype, table_name, spec FROM _vibe_forms WHERE archetype = 'master_detail' LIMIT 1`,
	).Scan(&archetype, &tableName, &spec)
	require.NoError(t, err, "order-entry should include a master_detail form")

	assert.Equal(t, "master_detail", archetype)
	assert.Equal(t, "orders", tableName, "master form should bind the order header table")
	// The subform must be FK-linked to the line-item table.
	assert.Contains(t, spec, "order_items", "master_detail spec should reference the line-item subform")
}

// --- helpers ---

type tmplRel struct {
	parentTable, parentColumn string
	childTable, childColumn   string
}

func templateRelationships(t *testing.T, sdb *sql.DB) []tmplRel {
	t.Helper()
	rows, err := sdb.Query(
		`SELECT parent_table, parent_column, child_table, child_column FROM _vibe_relationships`)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck

	var rels []tmplRel
	for rows.Next() {
		var r tmplRel
		require.NoError(t, rows.Scan(&r.parentTable, &r.parentColumn, &r.childTable, &r.childColumn))
		rels = append(rels, r)
	}
	require.NoError(t, rows.Err())
	return rels
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
