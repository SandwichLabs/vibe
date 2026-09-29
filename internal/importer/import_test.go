package importer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeCSV(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestImport_CreateWithKey(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "customer_id,name\n1,Ann\n2,Bob\n")

	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "customers", Key: "customer_id",
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, ModeCreate, sum.Mode)

	var pk []any
	require.NoError(t, db.QueryRow(
		`SELECT constraint_column_names FROM duckdb_constraints() WHERE table_name='customers' AND constraint_type='PRIMARY KEY'`,
	).Scan(&pk))
	require.Equal(t, []any{"customer_id"}, pk)

	var def sql.NullString
	require.NoError(t, db.QueryRow(
		`SELECT column_default FROM information_schema.columns WHERE table_name='customers' AND column_name='customer_id'`,
	).Scan(&def))
	require.Contains(t, def.String, "_vibe_seq_customers_customer_id")

	_, err = db.Exec("INSERT INTO customers (name) VALUES ('Cara')")
	require.NoError(t, err)
	var maxID int
	require.NoError(t, db.QueryRow("SELECT MAX(customer_id) FROM customers").Scan(&maxID))
	require.Equal(t, 3, maxID)
}

func TestImport_CreateKeyAuto(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "name\nAnn\nBob\n")

	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "customers", Key: KeyAuto,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	rows, err := db.Query("SELECT id, name FROM customers ORDER BY id")
	require.NoError(t, err)
	var got []int
	for rows.Next() {
		var id int
		var name string
		require.NoError(t, rows.Scan(&id, &name))
		got = append(got, id)
	}
	rows.Close()
	require.Equal(t, []int{1, 2}, got)

	var next int
	require.NoError(t, db.QueryRow("INSERT INTO customers (name) VALUES ('Cara') RETURNING id").Scan(&next))
	require.Equal(t, 3, next)
}

func TestImport_CreateKeyAuto_ExistingIDColumn(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "id,name\n5,Ann\n5,Bob\n")

	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: KeyAuto,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_name='t' AND column_name='id_2'").Scan(&n))
	require.Equal(t, 1, n)
}

func TestImport_CreateKeyNone(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "name\nAnn\n")

	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: KeyNone,
	})
	require.NoError(t, err)
	require.True(t, sum.OK)

	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM duckdb_constraints() WHERE table_name='t' AND constraint_type='PRIMARY KEY'").Scan(&n))
	require.Equal(t, 0, n)
}

func TestImport_TypeOverrideDoesNotFit(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "id,price\n1,12.50\n2,not_a_number\n")

	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: KeyNone,
		Types: map[string]ColType{"price": {Type: TypeDecimal, Scale: 2}},
	})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.NotEmpty(t, sum.BadValues)
	require.Equal(t, 2, sum.BadValues[0].Row)
	require.Equal(t, "price", sum.BadValues[0].Column)
	require.Equal(t, 1, sum.BadCount)

	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='t'").Scan(&n))
	require.Equal(t, 0, n, "the table must not exist after a failed pre-flight")
}

func TestImport_Replace(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n2,Bob\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n9,Cara\n")

	// The wizard's dry-run summary must report Replaced too — the old row
	// count is already known before anything is written, so a dry run
	// shouldn't have to guess or leave it at 0.
	dry, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeReplace, Key: "id", DryRun: true})
	require.NoError(t, err)
	require.True(t, dry.OK, "%+v", dry)
	require.Equal(t, 2, dry.Replaced)
	var stillOld int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&stillOld))
	require.Equal(t, 2, stillOld, "a dry run must not have written anything")

	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeReplace, Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, 2, sum.Replaced)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count)

	var next int
	require.NoError(t, db.QueryRow("INSERT INTO t (name) VALUES ('D') RETURNING id").Scan(&next))
	require.Equal(t, 10, next, "the sequence must follow the NEW data, not the old")
}

func TestImport_Replace_BlockedByIncomingFK(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "p.csv", "id,name\n1,Ann\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "parent", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK)

	_, err = db.Exec("CREATE TABLE child (x BIGINT REFERENCES parent(id))")
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "p2.csv", "id,name\n2,Bob\n")
	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "parent", Mode: ModeReplace, Key: "id"})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.NotContains(t, sum.Modes, ModeReplace)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM parent").Scan(&count))
	require.Equal(t, 1, count, "the table must be untouched")
}

func TestImport_AtomicRollback(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK)

	beforeCommit = func(tx *sql.Tx) error { return fmt.Errorf("forced failure") }
	defer func() { beforeCommit = nil }()

	path2 := writeCSV(t, dir, "b.csv", "id,name\n2,Bob\n")
	_, err = Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeReplace, Key: "id"})
	require.Error(t, err)

	beforeCommit = nil
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count, "the original table must survive a rolled-back replace")
	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM t WHERE id=1").Scan(&name))
	require.Equal(t, "Ann", name)
}

func TestImport_AppendMissingExtra(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name,region\n1,Ann,East\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK)

	path2 := writeCSV(t, dir, "b.csv", "id,name,notes\n2,Bob,hi\n")
	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeAppend})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.Equal(t, []string{"region"}, sum.Missing)
	require.Equal(t, []string{"notes"}, sum.Extra)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count)
}

func TestImport_AppendKeyCollisionBlocked(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n1,AnnAgain\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeAppend})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.Contains(t, sum.Blocked, "already in the table")
}

func TestImport_AppendExactDupesWarningOnly(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "a.csv", "name\nAnn\nBob\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: KeyNone})
	require.NoError(t, err)
	require.True(t, sum.OK)

	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Mode: ModeAppend})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, 2, sum.ExactDupes)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 4, count)
}

func TestImport_AppendAutoNumberContinues(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "name\nAnn\nBob\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: KeyAuto})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "name\nCara\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeAppend})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var next int
	require.NoError(t, db.QueryRow("INSERT INTO t (name) VALUES ('D') RETURNING id").Scan(&next))
	require.Equal(t, 4, next)
}

func TestImport_UpsertIdempotent(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n2,Bob\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK)

	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, 0, sum.Inserted)
	require.Equal(t, 0, sum.Updated)
	require.Equal(t, 2, sum.Unchanged)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 2, count)

	// Second run of the SAME file: 0/0/N.
	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, 0, sum.Inserted)
	require.Equal(t, 0, sum.Updated)
	require.Equal(t, 2, sum.Unchanged)
}

func TestImport_UpsertChangedAndNew(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n2,Bob\n3,Cara\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n1,AnnX\n2,Bob\n3,CaraX\n4,Dee\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, 1, sum.Inserted)
	require.Equal(t, 2, sum.Updated)
	require.Equal(t, 1, sum.Unchanged)

	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM t WHERE id=1").Scan(&name))
	require.Equal(t, "AnnX", name)
}

func TestImport_UpsertDuplicateKeysBlocked(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n5,X\n5,Y\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.NotEmpty(t, sum.BadValues)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count, "nothing written when the file's duplicate keys would silently collapse")
}

func TestImport_UpsertNoPK_NotOffered(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "a.csv", "name\nAnn\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: KeyNone})
	require.NoError(t, err)

	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t"})
	require.NoError(t, err)
	require.NotContains(t, sum.Modes, ModeUpsert)
}

func TestImport_UpsertBumpsAutoNumber(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "name\nAnn\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: KeyAuto})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n99,New\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var next int
	require.NoError(t, db.QueryRow("INSERT INTO t (name) VALUES ('D') RETURNING id").Scan(&next))
	require.Greater(t, next, 99)
}

func TestImport_UpsertFKOrphan(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	ppath := writeCSV(t, dir, "p.csv", "id,name\n1,Ann\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: ppath, HasHeader: true}, Table: "parent", Key: "id"})
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE child (id BIGINT PRIMARY KEY, parent_id BIGINT REFERENCES parent(id))")
	require.NoError(t, err)

	cpath := writeCSV(t, dir, "c.csv", "id,parent_id\n1,99\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: cpath, HasHeader: true}, Table: "child", Mode: ModeAppend})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.NotEmpty(t, sum.BadValues)
	require.Equal(t, "missing_parent", sum.BadValues[0].Reason)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM child").Scan(&count))
	require.Equal(t, 0, count)
}

func TestImport_UpsertOnFKReferencedParent(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	ppath := writeCSV(t, dir, "p.csv", "id,name\n1,Ann\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: ppath, HasHeader: true}, Table: "parent", Key: "id"})
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE child (x BIGINT REFERENCES parent(id))")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO child VALUES (1)")
	require.NoError(t, err)

	upath := writeCSV(t, dir, "u.csv", "id,name\n1,AnnUpdated\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: upath, HasHeader: true}, Table: "parent", Mode: ModeUpsert})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM parent WHERE id=1").Scan(&name))
	require.Equal(t, "AnnUpdated", name)
}

func TestImport_DryRunWritesNothing(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id", DryRun: true})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='t'").Scan(&n))
	require.Equal(t, 0, n)
}

func TestImport_DryRunUpsertCounts(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n1,AnnX\n2,Bob\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeUpsert, DryRun: true})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, 1, sum.Inserted)
	require.Equal(t, 1, sum.Updated)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count, "dry run must not write")
}

func TestImport_NoModeOnExistingTable(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "a.csv", "id,name\n1,Ann\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)

	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t"})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.NotEmpty(t, sum.Blocked)
	require.NotEmpty(t, sum.Modes)
}
