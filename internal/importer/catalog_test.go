package importer

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

// hubWithSecondCatalog returns a hardened session with a writable second
// catalog "other" attached from a .duckdb file.
func hubWithSecondCatalog(t *testing.T) (session.Session, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	s, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	path := filepath.Join(dir, "other.duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.Attach(context.Background(), session.Spec{Name: "other", Path: path}))
	return s, s.DB()
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(q).Scan(&n))
	return n
}

func TestImport_IntoSecondCatalog(t *testing.T) {
	ctx := context.Background()
	sess, db := hubWithSecondCatalog(t)
	dir := t.TempDir()
	csv := writeCSV(t, dir, "p.csv", "person_id,name\n1,Ann\n2,Bob\n")

	// A same-named table in the default catalog must stay untouched.
	_, err := db.Exec("CREATE TABLE people (marker INTEGER)")
	require.NoError(t, err)

	sum, err := Import(ctx, db, Options{
		Source: Source{Path: csv, HasHeader: true}, Table: "other.main.people", Key: "person_id",
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, "other.main.people", sum.Table, "Summary.Table keeps the caller's qualification")

	assert.Equal(t, 2, count(t, db, "SELECT COUNT(*) FROM other.main.people"))
	assert.Equal(t, 0, count(t, db, "SELECT COUNT(*) FROM people"), "default catalog's table untouched")
	assert.Equal(t, 0, count(t, db, "SELECT COUNT(*) FROM information_schema.columns WHERE table_catalog = current_database() AND table_name='people' AND column_name='person_id'"))

	// The auto-number sequence was created in the target catalog, next to the table.
	assert.Equal(t, 1, count(t, db, "SELECT COUNT(*) FROM duckdb_sequences() WHERE database_name='other' AND sequence_name='_vibe_seq_people_person_id'"))
	assert.Equal(t, 0, count(t, db, "SELECT COUNT(*) FROM duckdb_sequences() WHERE database_name=current_database() AND sequence_name LIKE '_vibe_seq_people%'"))
	_, err = db.Exec("INSERT INTO other.main.people (name) VALUES ('Cara')")
	require.NoError(t, err)
	assert.Equal(t, 3, count(t, db, "SELECT MAX(person_id) FROM other.main.people"))

	// The pooled connection that ran the import was restored to the default catalog.
	db.SetMaxOpenConns(1)
	var cat string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&cat))
	assert.Equal(t, sess.DefaultCatalog(), cat)
	db.SetMaxOpenConns(0)

	// Append and upsert into the existing catalog table, by qualified name.
	more := writeCSV(t, dir, "p2.csv", "person_id,name\n3,Cara2\n10,Dan\n")
	dry, err := Import(ctx, db, Options{
		Source: Source{Path: more, HasHeader: true}, Table: "other.main.people", Mode: ModeUpsert, DryRun: true,
	})
	require.NoError(t, err)
	require.True(t, dry.OK, "%+v", dry)
	assert.True(t, dry.Exists)
	assert.Equal(t, 3, dry.ExistingRows)

	up, err := Import(ctx, db, Options{
		Source: Source{Path: more, HasHeader: true}, Table: "other.main.people", Mode: ModeUpsert,
	})
	require.NoError(t, err)
	require.True(t, up.OK, "%+v", up)
	assert.Equal(t, 4, count(t, db, "SELECT COUNT(*) FROM other.main.people"))
	// The sequence was bumped past the explicit key in the right catalog.
	_, err = db.Exec("INSERT INTO other.main.people (name) VALUES ('Eve')")
	require.NoError(t, err)
	// Never a collision; one spare value may be skipped (#19: last_value == start_value is ambiguous).
	assert.Equal(t, 12, count(t, db, "SELECT MAX(person_id) FROM other.main.people"))
}

func TestImport_CatalogOnlyQualification(t *testing.T) {
	ctx := context.Background()
	_, db := hubWithSecondCatalog(t)
	csv := writeCSV(t, t.TempDir(), "c.csv", "name\nAnn\n")

	// catalog.table with no schema: the catalog's default schema.
	sum, err := Import(ctx, db, Options{Source: Source{Path: csv, HasHeader: true}, Table: "other.crew", Key: KeyNone})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 1, count(t, db, "SELECT COUNT(*) FROM other.main.crew"))
}

func TestImport_RejectsBadQualifiedNames(t *testing.T) {
	ctx := context.Background()
	_, db := hubWithSecondCatalog(t)
	csv := writeCSV(t, t.TempDir(), "c.csv", "name\nAnn\n")
	for _, bad := range []string{`other.main.x"; DROP TABLE y; --`, "other..t", "a.b.c.d", "other.main._vibe_secret", "system.main.t", "temp.main.t", "system.t", "ghost.main.t"} {
		_, err := Import(ctx, db, Options{Source: Source{Path: csv, HasHeader: true}, Table: bad})
		assert.Error(t, err, bad)
	}
}

func TestImport_UnqualifiedStillTargetsDefaultCatalog(t *testing.T) {
	ctx := context.Background()
	_, db := hubWithSecondCatalog(t)
	csv := writeCSV(t, t.TempDir(), "c.csv", "name\nAnn\n")
	sum, err := Import(ctx, db, Options{Source: Source{Path: csv, HasHeader: true}, Table: "plain", Key: KeyNone})
	require.NoError(t, err)
	require.True(t, sum.OK)
	assert.Equal(t, "plain", sum.Table)
	assert.Equal(t, 1, count(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_catalog = current_database() AND table_name='plain'"))
	assert.Equal(t, 0, count(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_catalog = 'other' AND table_name='plain'"))
}

// A same-named TEMP table must not capture the import's writes.
func TestImport_TempTableDoesNotShadowTarget(t *testing.T) {
	ctx := context.Background()
	_, db := hubWithSecondCatalog(t)
	db.SetMaxOpenConns(1)
	_, err := db.Exec("CREATE TABLE people (person_id INTEGER PRIMARY KEY, name VARCHAR); INSERT INTO people VALUES (1, 'Ann')")
	require.NoError(t, err)
	_, err = db.Exec("CREATE TEMP TABLE people (person_id INTEGER PRIMARY KEY, name VARCHAR)")
	require.NoError(t, err)
	csv := writeCSV(t, t.TempDir(), "p.csv", "person_id,name\n2,Bob\n")

	sum, err := Import(ctx, db, Options{Source: Source{Path: csv, HasHeader: true}, Table: "people", Mode: ModeAppend})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 2, count(t, db, "SELECT COUNT(*) FROM memory.main.people"))
	assert.Equal(t, 0, count(t, db, "SELECT COUNT(*) FROM temp.main.people"))
}
