package notebooks

import (
	"database/sql"
	"log/slog"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reportsTestDB returns a bare _vibe_notebooks-only DB (no _vibe_reports
// table at all), matching a file created after #68.
func reportsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE _vibe_notebooks (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', cells TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
	)`)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// withReportsTable adds an old-style _vibe_reports table (pre-#68 shape,
// no migrated_to column yet — MigrateReports must add it itself).
func withReportsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE _vibe_reports (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '',
		groups TEXT NOT NULL DEFAULT '[]', sort TEXT NOT NULL DEFAULT '[]', bands TEXT NOT NULL DEFAULT '{}',
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
	)`)
	require.NoError(t, err)
}

func insertReport(t *testing.T, db *sql.DB, id, title, source, groups, sort, bands string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO _vibe_reports (id, title, source, groups, sort, bands, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, title, source, groups, sort, bands,
	)
	require.NoError(t, err)
}

func notebookByID(t *testing.T, db *sql.DB, id string) Notebook {
	t.Helper()
	nb, _, err := Get(db, id)
	require.NoError(t, err)
	return nb
}

// TestMigrateReports_NoReportsTable asserts a file with no _vibe_reports
// table at all (a new file created after #68) is a clean no-op, not an
// error.
func TestMigrateReports_NoReportsTable(t *testing.T) {
	db := reportsTestDB(t)
	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestMigrateReports_PlainReport asserts a report with no grouping migrates
// into a notebook with exactly title/sql/note cells, and the note cell is
// present even though there was nothing dropped.
func TestMigrateReports_PlainReport(t *testing.T) {
	db := reportsTestDB(t)
	withReportsTable(t, db)
	insertReport(t, db, "r1", "Orders Report", "orders", `[]`, `[]`,
		`{"detail":{"columns":[{"field":"id","caption":"Order Id"},{"field":"status"}]}}`)

	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&migratedTo))
	require.NotEmpty(t, migratedTo)

	nb := notebookByID(t, db, migratedTo)
	require.Len(t, nb.Cells, 3, "title, sql, and note cells — always exactly three")
	assert.Equal(t, CellMarkdown, nb.Cells[0].Type)
	assert.Equal(t, CellSQL, nb.Cells[1].Type)
	assert.Contains(t, nb.Cells[1].Source, `"id" AS "Order Id"`)
	assert.Contains(t, nb.Cells[1].Source, `"status"`)
	assert.Contains(t, nb.Cells[1].Source, `FROM "orders"`)
	assert.Equal(t, CellMarkdown, nb.Cells[2].Type)
	assert.Contains(t, nb.Cells[2].Source, "no grouping or aggregates to lose", "the original-spec note cell must always be present")
	assert.Contains(t, nb.Cells[2].Source, `"id": "r1"`, "the note cell must keep the original spec as reference JSON")
}

// TestMigrateReports_GroupedReport asserts a report that used banding still
// gets the "not migrated" explanation alongside the plain query.
func TestMigrateReports_GroupedReport(t *testing.T) {
	db := reportsTestDB(t)
	withReportsTable(t, db)
	insertReport(t, db, "r1", "Grouped", "orders", `[{"field":"status","sort_dir":"asc"}]`, `[]`,
		`{"detail":{"columns":[{"field":"id"}]},"group_footer_aggregates":[{"name":"count","func":"count","field":"id"}]}`)

	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&migratedTo))
	nb := notebookByID(t, db, migratedTo)
	require.Len(t, nb.Cells, 3)
	assert.Contains(t, nb.Cells[2].Source, "Not migrated: banding/grouping")
	assert.Contains(t, nb.Cells[2].Source, "1 group level(s) (status)")
	assert.Contains(t, nb.Cells[2].Source, "1 aggregate(s)")
}

// TestMigrateReports_Idempotent asserts a second run does not re-migrate an
// already-migrated report, and does not touch reports migrated by a prior
// run.
func TestMigrateReports_Idempotent(t *testing.T) {
	db := reportsTestDB(t)
	withReportsTable(t, db)
	insertReport(t, db, "r1", "Orders Report", "orders", `[]`, `[]`, `{"detail":{"columns":[]}}`)

	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var firstMigratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&firstMigratedTo))

	// A second call must not create a second notebook or change migrated_to.
	n, err = MigrateReports(db, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "an already-migrated report must be skipped")

	var secondMigratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&secondMigratedTo))
	assert.Equal(t, firstMigratedTo, secondMigratedTo)

	var notebookCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM _vibe_notebooks`).Scan(&notebookCount))
	assert.Equal(t, 1, notebookCount, "re-running migration must not create a duplicate notebook")
}

// TestMigrateReports_PerRowRollback asserts one report's migration failure
// does not abort the whole batch, and leaves no partial state behind for
// the report that failed: migrateOneReport wraps "insert the notebook" +
// "stamp migrated_to" in one transaction, so a failure on the second
// statement must roll back the first (no orphaned notebook row) rather than
// leaving the report half-migrated. We force that failure with a SQLite
// trigger that rejects the migrated_to UPDATE for one specific report id,
// simulating a real write failure (e.g. a full disk) at exactly that point.
func TestMigrateReports_PerRowRollback(t *testing.T) {
	db := reportsTestDB(t)
	withReportsTable(t, db)
	insertReport(t, db, "r1", "First", "orders", `[]`, `[]`, `{"detail":{"columns":[]}}`)
	insertReport(t, db, "r2", "Second", "customers", `[]`, `[]`, `{"detail":{"columns":[]}}`)
	insertReport(t, db, "r3", "Third", "products", `[]`, `[]`, `{"detail":{"columns":[]}}`)

	// Reject the migrated_to stamp for r2 only, forcing migrateOneReport's
	// transaction to roll back for that one report.
	_, err := db.Exec(`
		CREATE TRIGGER reject_r2_migration
		BEFORE UPDATE ON _vibe_reports
		WHEN NEW.id = 'r2' AND NEW.migrated_to != ''
		BEGIN SELECT RAISE(ABORT, 'forced failure for r2'); END;
	`)
	require.NoError(t, err)

	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err, "one report's failure must not fail the whole batch")
	assert.Equal(t, 2, n, "r1 and r3 migrate; r2 does not")

	var m1, m2, m3 string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&m1))
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r2'`).Scan(&m2))
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r3'`).Scan(&m3))
	assert.NotEmpty(t, m1, "r1 (before the failing row) must still migrate")
	assert.Empty(t, m2, "r2 must be left unmigrated after its forced failure")
	assert.NotEmpty(t, m3, "r3 (after the failing row) must still migrate")

	// No orphaned notebook row from r2's rolled-back attempt: exactly one
	// notebook per successfully migrated report (r1, r3), never a third.
	assert.Equal(t, 2, notebookCount(t, db), "r2's rolled-back insert must not leave an orphaned notebook row")
}

func notebookCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM _vibe_notebooks`).Scan(&n))
	return n
}

// TestMigrateReports_QuotesIdentifiers covers the exact reviewer scenario:
// a column named "Order Date" (space), a reserved word ("select"), and a
// mixed-case column ("CustomerName") must all round-trip as valid, safe SQL
// via engine.QuoteIdent rather than being spliced in raw.
func TestMigrateReports_QuotesIdentifiers(t *testing.T) {
	db := reportsTestDB(t)
	withReportsTable(t, db)
	insertReport(t, db, "r1", "Quoting", "orders", `[]`, `[{"field":"Order Date","dir":"desc"}]`,
		`{"detail":{"columns":[{"field":"Order Date"},{"field":"select"},{"field":"CustomerName","caption":"Customer Name"}]}}`)

	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&migratedTo))
	nb := notebookByID(t, db, migratedTo)
	sqlText := nb.Cells[1].Source

	assert.Contains(t, sqlText, `"Order Date"`)
	assert.Contains(t, sqlText, `"select"`)
	assert.Contains(t, sqlText, `"CustomerName" AS "Customer Name"`)
	assert.Contains(t, sqlText, `ORDER BY "Order Date" DESC`)
}

// TestMigrateReports_SourceNeverExecuted proves migration only ever stores
// the built SELECT as notebook-cell text — it never runs it. A crafted,
// hostile "source" (e.g. one that would run arbitrary SQL if ever executed)
// must migrate without error and without touching the database beyond the
// notebook write, since a report's source was historically an arbitrary
// saved-query SQL string, not always a bare table name.
func TestMigrateReports_SourceNeverExecuted(t *testing.T) {
	db := reportsTestDB(t)
	withReportsTable(t, db)
	// Not a plain identifier -> wrapped as a subquery, never executed here.
	hostile := "SELECT 1; DROP TABLE _vibe_notebooks; --"
	insertReport(t, db, "r1", "Hostile", hostile, `[]`, `[]`, `{"detail":{"columns":[]}}`)

	n, err := MigrateReports(db, slog.Default())
	require.NoError(t, err, "migration must not execute the source SQL, only store it as cell text")
	assert.Equal(t, 1, n)

	// _vibe_notebooks must still exist and be queryable — proving the
	// "DROP TABLE" embedded in source was never run.
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM _vibe_notebooks`).Scan(&count))
	assert.Equal(t, 1, count)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_reports WHERE id = 'r1'`).Scan(&migratedTo))
	nb := notebookByID(t, db, migratedTo)
	assert.Contains(t, nb.Cells[1].Source, hostile, "the hostile source is stored verbatim as an unexecuted subquery")
}
