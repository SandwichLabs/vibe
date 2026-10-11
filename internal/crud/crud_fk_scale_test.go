package crud_test

// P6.1: DuckDB CRUD/FK viability regression spike at Phase-6 scale (parity plan
// §8/§4.1). Phase 6 introduces bulk xlsx/CSV loads into FK-constrained relational
// apps, which re-stresses the per-row write + FK-enforcement path at larger row
// counts. This is a focused INTEGRATION test against a REAL workspace + REAL
// DuckDB (no mocks): it bulk-loads a parent+child table set with an enforced FK,
// then exercises per-row UPDATE/DELETE, a referential-integrity rejection, a
// successful child-then-parent delete, and a close+reopen round-trip.
//
// No production code is expected to change unless this spike surfaces a defect.

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/crud"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fkScaleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const (
	numCustomers = 5000
	numOrders    = 20000
	numPerRow    = 200
	// SMB-scale latency budget for the 200 per-row updates (generous; the spike
	// records the actual measured wall-clock in the test log).
	perRowUpdateBudget = 10 * time.Second
)

// TestDuckDBCRUDFKScale is the Phase-6 viability spike / regression guard.
func TestDuckDBCRUDFKScale(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FK-scale spike in -short mode")
	}

	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	res, err := workspace.Init(filepath.Join(t.TempDir(), "fk-scale"))
	require.NoError(t, err)
	ws := res.Workspace
	t.Cleanup(func() { _ = ws.Close() })
	ctx := context.Background()
	sess, _, err := workspace.OpenSession(ctx, ws, fkScaleLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })

	ddb := sess.DB()
	require.NotNil(t, ddb)

	// Relational app: customers(id PK) and orders(id PK, customer_id) + total.
	// Foreign keys are plain SQL now (#68 dropped the FK-emit wizard helper) —
	// orders.customer_id REFERENCES customers(id) is declared inline, matching
	// how a user or the agent's run_query tool would create it.
	_, err = ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id), total DOUBLE);`)
	require.NoError(t, err)

	// Bulk-insert ~5,000 customers.
	bulkInsert(t, ddb, func(b *strings.Builder) {
		b.WriteString("INSERT INTO customers (id, name) VALUES ")
		for i := 1; i <= numCustomers; i++ {
			if i > 1 {
				b.WriteString(",")
			}
			b.WriteString("(")
			b.WriteString(itoa(i))
			b.WriteString(", 'cust-")
			b.WriteString(itoa(i))
			b.WriteString("')")
		}
	})

	// Bulk-insert ~20,000 orders, each referencing a valid customer.
	bulkInsert(t, ddb, func(b *strings.Builder) {
		b.WriteString("INSERT INTO orders (id, customer_id, total) VALUES ")
		for i := 1; i <= numOrders; i++ {
			if i > 1 {
				b.WriteString(",")
			}
			cust := ((i - 1) % numCustomers) + 1
			b.WriteString("(")
			b.WriteString(itoa(i))
			b.WriteString(", ")
			b.WriteString(itoa(cust))
			b.WriteString(", 1.5)")
		}
	})

	assertCount(t, ddb, "customers", numCustomers)
	assertCount(t, ddb, "orders", numOrders)

	ordersColTypes, err := crud.ColumnTypes(ddb, "orders")
	require.NoError(t, err)
	custColTypes, err := crud.ColumnTypes(ddb, "customers")
	require.NoError(t, err)

	// (a) 200 per-row UPDATE on orders by PK; each must affect exactly 1 row, and
	// the total wall-clock must stay within the SMB-scale budget.
	start := time.Now()
	for i := 1; i <= numPerRow; i++ {
		tx, txErr := ddb.Begin()
		require.NoError(t, txErr)
		res, uErr := crud.UpdateRowTx(tx, ordersColTypes, "orders", "id", i,
			map[string]any{"total": "9.99"})
		require.NoError(t, uErr)
		require.NotNil(t, res)
		assert.Equal(t, int64(1), res.Affected, "per-row update must affect exactly one row")
		require.NoError(t, tx.Commit())
	}
	elapsed := time.Since(start)
	t.Logf("P6.1 per-row UPDATE latency: %d updates in %s (avg %s/row)",
		numPerRow, elapsed, elapsed/time.Duration(numPerRow))
	assert.Less(t, elapsed, perRowUpdateBudget,
		"200 per-row updates should complete within the SMB-scale budget (%s)", perRowUpdateBudget)

	// (b) DELETE of a referenced parent customer must be rejected as an FK /
	// constraint violation (reusing the isForeignKeyViolation matcher).
	const referencedCustomer = 1 // referenced by order 1 (and others).
	tx, err := ddb.Begin()
	require.NoError(t, err)
	_, delErr := crud.DeleteRowTx(tx, custColTypes, "customers", "id", referencedCustomer)
	require.Error(t, delErr, "deleting a referenced parent must be blocked (RESTRICT)")
	assert.True(t, isForeignKeyViolation(delErr),
		"referenced-parent delete must fail with an FK/constraint violation, got: %v", delErr)
	_ = tx.Rollback()

	// referenced parent row must survive the blocked delete.
	var stillThere int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = ?;`, referencedCustomer).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "referenced parent row must survive the blocked delete")

	// (c) Delete the child rows first, then the parent — must succeed.
	_, err = ddb.Exec(`DELETE FROM orders WHERE customer_id = ?;`, referencedCustomer)
	require.NoError(t, err)
	tx, err = ddb.Begin()
	require.NoError(t, err)
	delRes, err := crud.DeleteRowTx(tx, custColTypes, "customers", "id", referencedCustomer)
	require.NoError(t, err, "deleting the parent after its children should succeed")
	require.NotNil(t, delRes)
	assert.Equal(t, int64(1), delRes.Affected)
	require.NoError(t, tx.Commit())

	assertCount(t, ddb, "customers", numCustomers-1)

	// (d) Close + reopen: row counts and the FK constraint must survive the
	// round-trip through the workspace's own DuckDB file.
	require.NoError(t, sess.Close())

	reopened, _, err := workspace.OpenSession(ctx, ws, fkScaleLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })

	rddb := reopened.DB()
	require.NotNil(t, rddb)

	assertCount(t, rddb, "customers", numCustomers-1)
	// orders for the deleted customer were removed in (c).
	assertCount(t, rddb, "orders", numOrders-(numOrders/numCustomers))

	// The FK must still be enforced after the round-trip: deleting a still-referenced
	// parent must be rejected.
	var aRef int
	require.NoError(t, rddb.QueryRow(`SELECT customer_id FROM orders LIMIT 1;`).Scan(&aRef))
	_, err = rddb.Exec(`DELETE FROM customers WHERE id = ?;`, aRef)
	require.Error(t, err, "FK must survive the checkpoint round-trip")
	assert.True(t, isForeignKeyViolation(err),
		"post-reopen referenced-parent delete must fail with an FK/constraint violation, got: %v", err)
}

func bulkInsert(t *testing.T, ddb *sql.DB, build func(*strings.Builder)) {
	t.Helper()
	var b strings.Builder
	build(&b)
	_, err := ddb.Exec(b.String())
	require.NoError(t, err)
}

// assertCount asserts a table's row count.
func assertCount(t *testing.T, q *sql.DB, table string, want int) {
	t.Helper()
	var got int
	require.NoError(t, q.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&got))
	assert.Equal(t, want, got, "row count for %q", table)
}

// isForeignKeyViolation reports whether err is a DuckDB foreign-key / referential
// integrity rejection.
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

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
