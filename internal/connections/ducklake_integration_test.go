//go:build integration

package connections_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// A DuckLake whose catalog is a file in the workspace and whose Parquet files
// are in a folder in the workspace: add, connect (creates it), write, read,
// join with local, reopen read-only, no path leaks.
func TestDuckLakeLocalEndToEnd(t *testing.T) {
	ws, _ := memWS(t)
	ctx := context.Background()
	def := workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.ducklake", ReadOnly: workspaceBool(false), Options: map[string]string{"data_path": "data/lake_files"}}
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, def))

	// A new lake tests as new, and creates nothing.
	cdef := connections.Def{Name: "lake", Type: "ducklake", Path: filepath.Join(ws.Root(), "data", "lake.ducklake"),
		Options: map[string]string{"data_path": filepath.Join(ws.Root(), "data", "lake_files")}}
	res := connections.Test(ctx, cdef, secrets.Credential{}, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	assert.Contains(t, res.Detail, "new lake")
	_, err := os.Stat(cdef.Path)
	assert.True(t, os.IsNotExist(err), "a test never creates the catalog")

	fresh := t.TempDir()
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.Empty(t, issues)
	assert.Equal(t, session.StatusExtensionMissing, connStatus(sess, "lake").Kind, "the consent step comes first")
	require.NoError(t, sess.InstallExtension(ctx, "ducklake"))
	sess.Preflight(ctx, "lake")
	assert.Equal(t, session.StatusNotConnected, connStatus(sess, "lake").Kind)
	_ = sess.Close()

	sess, issues, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.Empty(t, issues)
	eng := engine.NewManager(sess)
	_, err = eng.Execute(ctx, "CREATE TABLE lake.people (id INTEGER, name VARCHAR)", nil)
	require.NoError(t, err)
	_, err = eng.Execute(ctx, "INSERT INTO lake.people VALUES (1,'ada'),(2,'bob')", nil)
	require.NoError(t, err)
	assert.Equal(t, session.StatusOK, connStatus(sess, "lake").Kind)
	_, err = eng.Execute(ctx, "CREATE TABLE local.main.nums AS SELECT * FROM (VALUES (1, 10), (2, 20)) t(id, n)", nil)
	require.NoError(t, err)
	rows := queryAll(t, eng, `SELECT p.name, l.n FROM lake.main.people p JOIN local.main.nums l ON l.id = p.id ORDER BY p.id`)
	require.Len(t, rows, 2)
	assert.Equal(t, "ada", rows[0][0])

	// DuckLake 1.0 keeps small inserts in the catalog until they are flushed.
	_, err = sess.DB().Exec("CALL ducklake_flush_inlined_data('lake')")
	require.NoError(t, err)
	// The lake is real files in the workspace.
	_, err = os.Stat(filepath.Join(ws.Root(), "data", "lake.ducklake"))
	require.NoError(t, err)
	matches, _ := filepath.Glob(filepath.Join(ws.Root(), "data", "lake_files", "main", "people", "*.parquet"))
	assert.NotEmpty(t, matches)

	// The catalog list hides DuckLake's own metadata catalog.
	cats, err := sess.ListCatalogs(ctx)
	require.NoError(t, err)
	for _, c := range cats {
		assert.False(t, strings.HasPrefix(c.Name, "__ducklake_metadata_"), c.Name)
	}
	tables, err := sess.ListTables(ctx, "lake", "main")
	require.NoError(t, err)
	require.Len(t, tables, 1)
	assert.Equal(t, "people", tables[0].Name)
	_ = sess.Close()

	// A test of an existing lake works, and is read-only.
	res = connections.Test(ctx, cdef, secrets.Credential{}, connections.WithTestExtensionDir(fresh))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, 1, res.Count)

	// Reopened read-only: reads work, writes are refused by DuckDB.
	ro := def
	ro.ReadOnly = workspaceBool(true)
	require.NoError(t, ws.UpdateConnection(workspace.ScopeLocal, ro))
	sess, issues, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	defer sess.Close()
	require.Empty(t, issues)
	eng = engine.NewManager(sess)
	rows = queryAll(t, eng, "SELECT count(*) FROM lake.main.people")
	assert.EqualValues(t, 2, rows[0][0])
	_, err = eng.Execute(ctx, "INSERT INTO lake.main.people VALUES (3, 'cy')", nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "read-only")

	// duckdb_databases shows a local path and nothing else (no secret exists).
	rows = queryAll(t, eng, "SELECT coalesce(path, '') FROM duckdb_databases() WHERE database_name = 'lake'")
	require.Len(t, rows, 1)
	assert.Contains(t, fmt.Sprint(rows[0][0]), "lake.ducklake")
	rows = queryAll(t, eng, "SELECT count(*) FROM duckdb_secrets()")
	assert.EqualValues(t, 0, rows[0][0])
}

func workspaceBool(b bool) *bool { return &b }

func TestDuckLakeDataPathMustBeInsideAStorageScope(t *testing.T) {
	ws, _ := memWS(t)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/l.ducklake", Options: map[string]string{"data_path": "s3://bucket/lake/"}}))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	_ = sess.Close()
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "not inside the scope of any")

	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
	sess, issues, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	_ = sess.Close()
	for _, i := range issues {
		assert.NotContains(t, i.Message, "not inside the scope", "inside the s3 scope: accepted")
	}
}

// A DuckLake whose data lives in object storage: the data path is inside the
// scope of the workspace's s3 connection, and the lake uses that connection's
// secret.
func TestDuckLakeWithObjectStorageDataPath(t *testing.T) {
	ep := startMinIO(t)
	ctx := context.Background()
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://lake/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}))
	require.NoError(t, ws.SetCredentials(ctx, "store", secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}, false))
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "wh", Type: "ducklake", Path: "data/wh.ducklake", ReadOnly: workspaceBool(false),
		Options: map[string]string{"data_path": "s3://lake/ducklake/"}}))
	fresh := t.TempDir()
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.Empty(t, issues)
	for _, e := range []string{"httpfs", "ducklake"} {
		require.NoError(t, sess.InstallExtension(ctx, e))
	}
	sess.Preflight(ctx, "store")
	sess.Preflight(ctx, "wh")
	defer sess.Close()
	eng := engine.NewManager(sess)
	_, err = eng.Execute(ctx, "CREATE TABLE wh.events AS SELECT * FROM (VALUES (1, 'a'), (2, 'b')) t(id, k)", nil)
	require.NoError(t, err)
	rows := queryAll(t, eng, "SELECT count(*) FROM wh.main.events")
	assert.EqualValues(t, 2, rows[0][0])
	// DuckLake 1.0 keeps small inserts in the catalog until they are flushed.
	_, err = sess.DB().Exec("CALL ducklake_flush_inlined_data('wh')")
	require.NoError(t, err)
	// The Parquet file is in the bucket, under the data path.
	rows = queryAll(t, eng, "SELECT count(*) FROM glob('s3://lake/ducklake/main/events/*.parquet')")
	assert.EqualValues(t, 1, rows[0][0])
	// The secret never shows in DuckDB's listing of databases.
	rows = queryAll(t, eng, "SELECT coalesce(path, '') FROM duckdb_databases() WHERE database_name = 'wh'")
	assert.NotContains(t, fmt.Sprint(rows[0][0]), minioSecret)
}

// A catalog names every file the lake reads. One whose rows point outside the
// data folder (an absolute path, "..", a URL, a different stored data path, a
// rewritten delete file) is refused and detached on attach, and what it points
// at is never read.
func TestDuckLakeRefusesACraftedCatalog(t *testing.T) {
	for _, kind := range []string{"duckdb", "sqlite"} {
		t.Run(kind, func(t *testing.T) { craftedCatalogRows(t, kind) })
	}
}

func craftedCatalogRows(t *testing.T, kind string) {
	ctx := context.Background()
	for name, tamper := range map[string]string{
		"absolute data file":   "UPDATE ducklake_data_file SET path = '%OUT%/secret.parquet', path_is_relative = false",
		"dot dot data file":    "UPDATE ducklake_data_file SET path = '../../../../..%OUT%/secret.parquet'",
		"url data file":        "UPDATE ducklake_data_file SET path = 'http://127.0.0.1:1/x.parquet'",
		"leading slash":        "UPDATE ducklake_data_file SET path = '/etc/hostname'",
		"backslash":            "UPDATE ducklake_data_file SET path = 'a\\b.parquet'",
		"absolute table path":  "UPDATE ducklake_table SET path = '%OUT%/', path_is_relative = false",
		"dot dot schema path":  "UPDATE ducklake_schema SET path = '../elsewhere/'",
		"stored data path":     "UPDATE ducklake_metadata SET value = '%OUT%/' WHERE key = 'data_path'",
		"absolute delete file": "INSERT INTO ducklake_delete_file VALUES (99, 1, 2, NULL, 0, '%OUT%/del.parquet', false, 'parquet', 1, 1, 1, NULL, NULL)",
		"scheduled deletion":   "INSERT INTO ducklake_files_scheduled_for_deletion VALUES (7, '%OUT%/victim.parquet', false, now())",
	} {
		t.Run(name, func(t *testing.T) {
			ws, _ := memWS(t)
			opts := map[string]string{"data_path": "data/lake_files"}
			if kind == "sqlite" {
				opts["catalog"] = "sqlite"
			}
			def := workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.ducklake", ReadOnly: workspaceBool(false), Options: opts}
			require.NoError(t, ws.AddConnection(workspace.ScopeLocal, def))
			fresh := t.TempDir()
			sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
			require.NoError(t, err)
			require.NoError(t, sess.InstallExtension(ctx, "ducklake"))
			if kind == "sqlite" {
				require.NoError(t, sess.InstallExtension(ctx, "sqlite"))
			}
			sess.Preflight(ctx, "lake")
			require.NoError(t, sess.Connect(ctx, "lake"), "an honest lake attaches")
			_, err = sess.DB().Exec("CREATE TABLE lake.people (id INTEGER, name VARCHAR); INSERT INTO lake.people VALUES (1,'ada'); DELETE FROM lake.people WHERE id = 5; INSERT INTO lake.people VALUES (2, 'bob'); CALL ducklake_flush_inlined_data('lake')")
			require.NoError(t, err)
			require.NoError(t, sess.Close())

			outside := t.TempDir()
			tamperFile(t, filepath.Join(ws.Root(), "data", "lake.ducklake"), kind, []string{
				"COPY (SELECT 42 AS id, 'TOP-SECRET' AS name) TO '" + filepath.Join(outside, "secret.parquet") + "' (FORMAT PARQUET)",
				"USE c",
				strings.ReplaceAll(tamper, "%OUT%", outside),
				"USE memory",
			})

			sess, _, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
			require.NoError(t, err)
			defer sess.Close()
			sess.Preflight(ctx, "lake")
			err = sess.Connect(ctx, "lake")
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "TOP-SECRET")
			st := connStatus(sess, "lake")
			assert.Equal(t, session.StatusError, st.Kind)
			// DuckLake itself already refuses a stored data path that differs; the
			// session check is the second layer for everything else.
			assert.Regexp(t, "refusing|does not match existing data path", st.Message)
			var n int
			require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM duckdb_databases() WHERE database_name IN ('lake', '__ducklake_metadata_lake')").Scan(&n))
			assert.Equal(t, 0, n, "the refused lake is detached, metadata catalog included")
			_, err = sess.DB().Exec("SELECT * FROM lake.main.people")
			assert.Error(t, err)
			// Test refuses it too.
			res := connections.Test(ctx, connections.Def{Name: "lake", Type: "ducklake", Path: filepath.Join(ws.Root(), "data", "lake.ducklake"),
				Options: map[string]string{"data_path": filepath.Join(ws.Root(), "data", "lake_files"), "catalog": kindOpt(kind)}}, secrets.Credential{}, connections.WithTestExtensionDir(fresh))
			assert.False(t, res.OK)
			assert.Regexp(t, "refusing|does not match existing data path", res.Error)
		})
	}
}

// A relative catalog row ("link/secret.parquet") is clean, but if "link" is a
// symbolic link out of the data folder the lake would read what it points at.
func TestDuckLakeRefusesASymlinkUnderTheDataFolder(t *testing.T) {
	ctx := context.Background()
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.ducklake", ReadOnly: workspaceBool(false), Options: map[string]string{"data_path": "data/lake_files"}}))
	fresh := t.TempDir()
	sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.NoError(t, sess.InstallExtension(ctx, "ducklake"))
	sess.Preflight(ctx, "lake")
	require.NoError(t, sess.Connect(ctx, "lake"))
	_, err = sess.DB().Exec("CREATE TABLE lake.people (id INTEGER, name VARCHAR); INSERT INTO lake.people VALUES (1,'ada'),(2,'bob')")
	require.NoError(t, err)
	outside := t.TempDir()
	_, err = sess.DB().Exec("COPY (SELECT 42 AS id, 'TOP-SECRET' AS name) TO '" + filepath.Join(outside, "secret.parquet") + "' (FORMAT PARQUET)")
	require.NoError(t, err)
	require.NoError(t, sess.Close())

	// An honest lake attaches again with no link in it.
	sess, _, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	sess.Preflight(ctx, "lake")
	require.NoError(t, sess.Connect(ctx, "lake"))
	require.NoError(t, sess.Close())

	dir := filepath.Join(ws.Root(), "data", "lake_files", "main", "people")
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link")))
	meta, err := sql.Open("duckdb", filepath.Join(ws.Root(), "data", "lake.ducklake"))
	require.NoError(t, err)
	_, err = meta.Exec("UPDATE ducklake_data_file SET path = 'link/secret.parquet', path_is_relative = true")
	require.NoError(t, err)
	require.NoError(t, meta.Close())

	sess, _, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	defer sess.Close()
	sess.Preflight(ctx, "lake")
	err = sess.Connect(ctx, "lake")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
	assert.NotContains(t, err.Error(), "TOP-SECRET")
	assert.Equal(t, session.StatusError, connStatus(sess, "lake").Kind)
	_, err = sess.DB().Exec("SELECT * FROM lake.main.people")
	assert.Error(t, err, "detached")
	// Test refuses it too.
	res := connections.Test(ctx, connections.Def{Name: "lake", Type: "ducklake", Path: filepath.Join(ws.Root(), "data", "lake.ducklake"),
		Options: map[string]string{"data_path": filepath.Join(ws.Root(), "data", "lake_files")}}, secrets.Credential{}, connections.WithTestExtensionDir(fresh))
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "symbolic link")
}

// A lake with a SQLite catalog on this computer: consent for both extensions,
// created on first write, read-write, then reopened read-only, and tested.
func TestDuckLakeSQLiteCatalogLocalEndToEnd(t *testing.T) {
	ws, _ := memWS(t)
	ctx := context.Background()
	opts := map[string]string{"data_path": "data/lake_files", "catalog": "sqlite"}
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.db", ReadOnly: workspaceBool(false), Options: opts}))
	open := func(fresh string) session.Session {
		sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
		require.NoError(t, err)
		require.Empty(t, issues)
		return sess
	}

	// Consent: the lake first, then the extension its catalog is read with.
	fresh := t.TempDir()
	sess := open(fresh)
	st := connStatus(sess, "lake")
	assert.Equal(t, session.StatusExtensionMissing, st.Kind)
	assert.Equal(t, "ducklake", st.Extension)
	require.NoError(t, sess.InstallExtension(ctx, "ducklake"))
	sess.Preflight(ctx, "lake")
	st = connStatus(sess, "lake")
	assert.Equal(t, session.StatusExtensionMissing, st.Kind, "sqlite is still missing")
	assert.Equal(t, "sqlite", st.Extension)
	require.NoError(t, sess.InstallExtension(ctx, "sqlite"))
	sess.Preflight(ctx, "lake")
	assert.Equal(t, session.StatusNotConnected, connStatus(sess, "lake").Kind)
	_ = sess.Close()

	cdef := connections.Def{Name: "lake", Type: "ducklake", Path: filepath.Join(ws.Root(), "data", "lake.db"),
		Options: map[string]string{"data_path": filepath.Join(ws.Root(), "data", "lake_files"), "catalog": "sqlite"}}
	res := connections.Test(ctx, cdef, secrets.Credential{}, connections.WithTestExtensionDir(fresh))
	require.True(t, res.OK, res.Error)
	assert.Contains(t, res.Detail, "new lake")

	sess = open(fresh)
	eng := engine.NewManager(sess)
	_, err := eng.Execute(ctx, "CREATE TABLE lake.people (id INTEGER, name VARCHAR)", nil)
	require.NoError(t, err)
	_, err = eng.Execute(ctx, "INSERT INTO lake.people VALUES (1,'ada'),(2,'bob')", nil)
	require.NoError(t, err)
	_, err = sess.DB().Exec("CALL ducklake_flush_inlined_data('lake')")
	require.NoError(t, err)
	_, err = eng.Execute(ctx, "INSERT INTO lake.people VALUES (3,'cy')", nil)
	require.NoError(t, err)
	require.NoError(t, sess.Close())
	head := make([]byte, 16)
	f, err := os.Open(filepath.Join(ws.Root(), "data", "lake.db"))
	require.NoError(t, err)
	_, _ = f.Read(head)
	_ = f.Close()
	assert.Equal(t, "SQLite format 3\x00", string(head), "the catalog is a SQLite file")

	// Tested now that it exists: attached, vetted, probed.
	res = connections.Test(ctx, cdef, secrets.Credential{}, connections.WithTestExtensionDir(fresh))
	require.True(t, res.OK, res.Error)
	assert.NotContains(t, res.Detail, "new lake")

	// Reopened read-only.
	require.NoError(t, ws.RemoveConnection(ctx, workspace.ScopeLocal, "lake"))
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.db", ReadOnly: workspaceBool(true), Options: opts}))
	sess = open(fresh)
	defer sess.Close() //nolint:errcheck
	eng = engine.NewManager(sess)
	rows := queryAll(t, eng, "SELECT count(*) FROM lake.people")
	assert.EqualValues(t, 3, rows[0][0])
	_, err = eng.Execute(ctx, "INSERT INTO lake.people VALUES (4,'di')", nil)
	assert.Error(t, err, "read-only")
}
