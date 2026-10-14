package engine

import (
	"context"
	"database/sql"
	"testing"

	"github.com/SandwichLabs/vibe/internal/session"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"
)

func TestIsIntegerType(t *testing.T) {
	for _, typ := range []string{"BIGINT", "integer", "Int", "HUGEINT", "ubigint", "SMALLINT"} {
		require.True(t, IsIntegerType(typ), typ)
	}
	for _, typ := range []string{"VARCHAR", "DOUBLE", "DECIMAL(18,2)", "DATE", "BOOLEAN", ""} {
		require.False(t, IsIntegerType(typ), typ)
	}
}

func TestAutoNumberSequence(t *testing.T) {
	require.Equal(t, "_vibe_seq_customers_id", AutoNumberSequence("customers", "id"))
}

func openTestDuckDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestBumpAutoNumber(t *testing.T) {
	ctx := context.Background()
	db := openTestDuckDB(t)

	_, err := db.ExecContext(ctx, "CREATE SEQUENCE _vibe_seq_t_id START 1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE t (id BIGINT PRIMARY KEY DEFAULT nextval('_vibe_seq_t_id'), name VARCHAR)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO t (id, name) VALUES (5, 'x'), (99, 'y')")
	require.NoError(t, err)

	require.NoError(t, BumpAutoNumber(ctx, db, session.TableRef{Name: "t"}, "id"))

	var next int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO t (name) VALUES ('z') RETURNING id").Scan(&next))
	require.Greater(t, next, int64(99))
}

func TestBumpAutoNumber_NoDefault(t *testing.T) {
	ctx := context.Background()
	db := openTestDuckDB(t)
	_, err := db.ExecContext(ctx, "CREATE TABLE t (id BIGINT PRIMARY KEY, name VARCHAR)")
	require.NoError(t, err)
	require.NoError(t, BumpAutoNumber(ctx, db, session.TableRef{Name: "t"}, "id"))
}

func TestBumpAutoNumber_FKReferenced(t *testing.T) {
	// The case ALTER COLUMN SET DEFAULT / CREATE OR REPLACE SEQUENCE can't
	// handle: a table referenced by a foreign key. BumpAutoNumber must still
	// work because it never touches DDL.
	ctx := context.Background()
	db := openTestDuckDB(t)

	_, err := db.ExecContext(ctx, "CREATE SEQUENCE _vibe_seq_parent_id START 1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE parent (id BIGINT PRIMARY KEY DEFAULT nextval('_vibe_seq_parent_id'), name VARCHAR)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO parent (id, name) VALUES (1,'a'),(50,'b')")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE child (x BIGINT REFERENCES parent(id))")
	require.NoError(t, err)

	require.NoError(t, BumpAutoNumber(ctx, db, session.TableRef{Name: "parent"}, "id"))

	var next int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO parent (name) VALUES ('c') RETURNING id").Scan(&next))
	require.Greater(t, next, int64(50))
}

func TestBumpAutoNumber_CapsHugeGap(t *testing.T) {
	ctx := context.Background()
	db := openTestDuckDB(t)

	_, err := db.ExecContext(ctx, "CREATE SEQUENCE _vibe_seq_t_id START 1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE t (id BIGINT PRIMARY KEY DEFAULT nextval('_vibe_seq_t_id'), name VARCHAR)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO t (id, name) VALUES (50000000000, 'x')")
	require.NoError(t, err)

	err = BumpAutoNumber(ctx, db, session.TableRef{Name: "t"}, "id")
	require.Error(t, err)
	require.Contains(t, err.Error(), "too far behind")

	// The refusal must have zero side effects: no nextval() was ever
	// consumed to reach this decision (AutoNumberGap peeks via
	// duckdb_sequences(), it never calls nextval itself), so the sequence
	// is still completely untouched.
	var lastValue sql.NullInt64
	require.NoError(t, db.QueryRow(
		"SELECT last_value FROM duckdb_sequences() WHERE lower(sequence_name) = lower('_vibe_seq_t_id')").Scan(&lastValue))
	require.False(t, lastValue.Valid, "a refused bump must not advance the sequence at all")
}

// TestAutoNumberGap_CaseInsensitive proves AutoNumberGap (and so
// BumpAutoNumber) finds a sequence-backed default and the sequence itself
// when called with a table name that differs only in case from how the
// table was actually created — the exact mismatch a caller resolving an
// existing table case-insensitively (see the importer's own canonical-name
// comment) must not reintroduce.
func TestAutoNumberGap_CaseInsensitive(t *testing.T) {
	ctx := context.Background()
	db := openTestDuckDB(t)

	_, err := db.ExecContext(ctx, "CREATE SEQUENCE _vibe_seq_Customers_id START 1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE "Customers" (id BIGINT PRIMARY KEY DEFAULT nextval('_vibe_seq_Customers_id'), name VARCHAR)`)
	require.NoError(t, err)

	gap, ok, err := AutoNumberGap(ctx, db, session.TableRef{Name: "customers"}, "id", 50)
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, gap, int64(0))
}

// TestAutoNumberGap_AfterReopen (#19): duckdb_sequences().last_value must be
// read as the last dispensed value both in the session that created the
// sequence and after the file is closed and reopened, so a fully caught-up
// sequence reports gap 0 and a lagging one reports the exact distance.
func TestAutoNumberGap_AfterReopen(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/seq.duckdb"
	open := func() *sql.DB {
		db, err := sql.Open("duckdb", path)
		require.NoError(t, err)
		return db
	}

	db := open()
	_, err := db.ExecContext(ctx, "CREATE SEQUENCE _vibe_seq_t_id START 1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE t (id BIGINT PRIMARY KEY DEFAULT nextval('_vibe_seq_t_id'), name VARCHAR)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO t (name) VALUES ('a'), ('b'), ('c')") // ids 1..3
	require.NoError(t, err)

	ref := session.TableRef{Name: "t"}
	gap, ok, err := AutoNumberGap(ctx, db, ref, "id", 3)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(0), gap, "same session")
	require.NoError(t, db.Close())

	db = open()
	defer db.Close()
	gap, ok, err = AutoNumberGap(ctx, db, ref, "id", 3)
	require.NoError(t, err)
	require.True(t, ok)
	require.LessOrEqual(t, gap, int64(1), "after reopen: sequence already at max, at most one spare value")
	gap, _, err = AutoNumberGap(ctx, db, ref, "id", 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, gap, int64(7), "after reopen: never under-reports the distance to 10")

	// Bump past an external max, reopen again, and the default insert must not collide.
	_, err = db.ExecContext(ctx, "INSERT INTO t (id, name) VALUES (10, 'x')")
	require.NoError(t, err)
	require.NoError(t, BumpAutoNumber(ctx, db, ref, "id"))
	require.NoError(t, db.Close())
	db = open()
	defer db.Close()
	var next int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO t (name) VALUES ('z') RETURNING id").Scan(&next))
	require.Equal(t, int64(11), next)
}

// The ambiguous-position adjustment uses the sequence's own step, not 1.
func TestSequencePosition_UsesIncrement(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/inc.duckdb"
	db, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE SEQUENCE s START 10 INCREMENT 5")
	require.NoError(t, err)
	var v int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT nextval('s')").Scan(&v)) // 10
	require.NoError(t, db.Close())
	db, err = sql.Open("duckdb", path)
	require.NoError(t, err)
	defer db.Close()
	pos, found, err := sequencePosition(ctx, db, session.TableRef{}, "s")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(10), pos) // next is 15; last dispensed 10
}
