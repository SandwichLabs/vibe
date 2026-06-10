package schema_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyForeignKeyParentDeleteIntegrity is the regression test for the
// dangling-dependency defect found in adversarial review of P4.0: the original
// rebuild created the child table under a temp name and renamed it into place,
// which left DuckDB's recorded FK dependency on the PARENT table pointing at the
// dropped temp table. The result was that EVERY delete on the parent failed with
// a "Table ... does not exist" catalog error — including deletes of UNREFERENCED
// parent rows — which masqueraded as RESTRICT in the spike.
//
// A correct FK-emit must give clean RESTRICT semantics:
//   - deleting an UNREFERENCED parent row SUCCEEDS, and
//   - deleting a REFERENCED parent row is BLOCKED specifically by an FK/constraint
//     violation (not an unrelated catalog error).
func TestApplyForeignKeyParentDeleteIntegrity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fk-parent-delete.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)

	_, err = ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER, total DOUBLE);`)
	require.NoError(t, err)

	// customer 1 is UNREFERENCED; customer 2 is referenced by an order.
	_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'unreferenced'), (2, 'referenced');`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (10, 2, 5.0);`)
	require.NoError(t, err)

	require.NoError(t, schema.ApplyForeignKey(ddb, schema.ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "customer_id",
		ParentTable:  "customers",
		ParentColumn: "id",
		OnDelete:     "RESTRICT",
	}))

	// CONTROL ASSERTION: deleting an UNREFERENCED parent row must SUCCEED. This
	// is the assertion that the original buggy rebuild failed (it errored with a
	// dangling-temp-table catalog error). It is the proof that RESTRICT is real
	// and not a false positive.
	_, err = ddb.Exec(`DELETE FROM customers WHERE id = 1;`)
	require.NoError(t, err, "deleting an UNREFERENCED parent row must succeed (no dangling FK dependency)")

	var custCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers;`).Scan(&custCount))
	assert.Equal(t, 1, custCount, "unreferenced parent row should be gone")

	// Deleting a REFERENCED parent row must be BLOCKED, specifically by an
	// FK/constraint violation (not an unrelated catalog error).
	_, err = ddb.Exec(`DELETE FROM customers WHERE id = 2;`)
	require.Error(t, err, "deleting a REFERENCED parent row must be blocked (RESTRICT)")
	assert.True(t, isForeignKeyViolation(err),
		"referenced-parent delete must fail with a foreign-key/constraint violation, got: %v", err)

	var refStill int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = 2;`).Scan(&refStill))
	assert.Equal(t, 1, refStill, "referenced parent row must survive the blocked delete")
}

// isForeignKeyViolation reports whether err is a DuckDB foreign-key / referential
// integrity rejection, as opposed to an unrelated error (e.g. a "Table ... does
// not exist" catalog error). DuckDB phrases FK rejections with the constraint
// type and the word "key"/"reference".
func isForeignKeyViolation(err error) bool {
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

// TestForeignKeyRebuildPreservesDefaults guards the auto-number bug: the FK
// rebuild reconstructs the child table from introspection, and the original
// implementation dropped column DEFAULT expressions — silently breaking
// sequence-backed (AutoNumber) primary keys the moment a relationship was
// declared. Both ApplyForeignKey and StripForeignKeys must carry DEFAULTs over.
func TestForeignKeyRebuildPreservesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fk-defaults.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)

	_, err = ddb.Exec(`CREATE SEQUENCE _vibe_seq_orders_id START 1;`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE orders (
		id BIGINT DEFAULT nextval('_vibe_seq_orders_id') PRIMARY KEY,
		customer_id INTEGER,
		status TEXT DEFAULT 'open');`)
	require.NoError(t, err)

	require.NoError(t, schema.ApplyForeignKey(ddb, schema.ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "customer_id",
		ParentTable:  "customers",
		ParentColumn: "id",
		OnDelete:     "RESTRICT",
	}))

	// The rebuilt table must still auto-number and apply value defaults.
	_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'acme');`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders (customer_id) VALUES (1);`)
	require.NoError(t, err, "id-less insert must succeed after FK rebuild")

	var id int64
	var status string
	require.NoError(t, ddb.QueryRow(`SELECT id, status FROM orders`).Scan(&id, &status))
	assert.Equal(t, int64(1), id)
	assert.Equal(t, "open", status)

	// Stripping the FK must also keep the defaults.
	require.NoError(t, schema.StripForeignKeys(ddb, "orders"))
	_, err = ddb.Exec(`INSERT INTO orders (customer_id) VALUES (1);`)
	require.NoError(t, err)
	var n int
	require.NoError(t, ddb.QueryRow(`SELECT max(id) FROM orders`).Scan(&n))
	assert.Equal(t, 2, n, "sequence keeps numbering after FK strip")
}
