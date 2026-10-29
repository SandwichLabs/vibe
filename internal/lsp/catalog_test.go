package lsp

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/session"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hubWith attaches a DuckDB file with a table, a view and a second schema.
func hubWith(t *testing.T) session.Session {
	t.Helper()
	dir := t.TempDir()
	s, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	path := filepath.Join(dir, "shop.duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = f.Exec(`CREATE TABLE orders (id INTEGER, total DECIMAL(10,2));
		CREATE VIEW big_orders AS SELECT * FROM orders WHERE total > 100;
		CREATE SCHEMA staging;
		CREATE TABLE staging.raw (payload VARCHAR);`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.Attach(context.Background(), session.Spec{Name: "shop", Path: path}))
	require.NoError(t, s.SetDefaultCatalog(context.Background(), "shop"))
	return s
}

func TestBuild_SnapshotsConnectedCatalogs(t *testing.T) {
	cat, err := Build(context.Background(), hubWith(t))
	require.NoError(t, err)
	assert.Equal(t, "shop", cat.DefaultConnection)
	shop := cat.Conn("shop")
	require.NotNil(t, shop)
	assert.Equal(t, "main", shop.DefaultSchema)
	orders := shop.Schema("main").Relation("orders")
	require.NotNil(t, orders)
	assert.Equal(t, "table", orders.Kind)
	assert.Equal(t, []Column{{"id", "INTEGER"}, {"total", "DECIMAL(10,2)"}}, orders.Columns)
	assert.Equal(t, "view", shop.Schema("main").Relation("big_orders").Kind)
	assert.NotNil(t, shop.Schema("staging").Relation("raw"))
	assert.Nil(t, shop.Schema("information_schema"))
	assert.Contains(t, cat.Functions, "date_trunc")

	items := Complete("SELECT * FROM staging.", len("SELECT * FROM staging."), cat)
	assert.Equal(t, []string{"raw"}, labels(items))
}

func TestSnapshot_MergesAndSourceReloads(t *testing.T) {
	root := t.TempDir()
	path := SnapshotPath(root)
	old := fixture()
	old.Version = catalogVersion
	require.NoError(t, old.Save(path))
	src := NewSource(path)
	require.NotNil(t, src.Catalog().Conn("pg"))
	assert.Nil(t, src.Catalog().Conn("shop"))

	n, err := Snapshot(context.Background(), hubWith(t), root)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	// Same-second rewrites can keep the mtime; the size changes here.
	cat := src.Catalog()
	assert.NotNil(t, cat.Conn("shop"), "the fresh connection is in")
	assert.NotNil(t, cat.Conn("pg"), "a connection that is not connected now is kept")
	assert.Equal(t, "shop", cat.DefaultConnection)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	assert.WithinDuration(t, time.Now(), cat.Generated, time.Minute)
}

func TestSource_MissingSnapshot(t *testing.T) {
	assert.Nil(t, NewSource(filepath.Join(t.TempDir(), "nope.json")).Catalog())
	var nilSrc *Source
	assert.Nil(t, nilSrc.Catalog())
}

// The snapshot holds names and types only. A connection whose definition
// carries secret-looking text must not leak any of it into catalog.json.
func TestSnapshot_NeverCarriesCredentials(t *testing.T) {
	sess := hubWith(t)
	root := t.TempDir()
	path := SnapshotPath(root)
	_, err := Snapshot(context.Background(), sess, root)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))
	assert.ElementsMatch(t, []string{"version", "generated", "default_connection", "connections", "functions", "metrics"}, keysOf(generic))
	list, ok := generic["connections"].([]any)
	require.True(t, ok)
	for _, c := range list {
		conn, ok := c.(map[string]any)
		require.True(t, ok)
		assert.ElementsMatch(t, []string{"name", "type", "default_schema", "schemas", "updated"}, keysOf(conn))
	}
	conns, err := json.Marshal(generic["connections"]) // function names legitimately include "secret"
	require.NoError(t, err)
	for _, bad := range []string{"password", "secret", "token", "dsn", "shop.duckdb", "://"} {
		assert.NotContains(t, strings.ToLower(string(conns)), bad)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSnapshot_RefusesSymlinksOutOfTheWorkspace(t *testing.T) {
	sess := hubWith(t)
	outside := t.TempDir()

	// .vibe-local itself is a link out of the workspace.
	root := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".vibe-local")))
	n, err := Snapshot(context.Background(), sess, root)
	require.Error(t, err)
	assert.Zero(t, n)
	entries, _ := os.ReadDir(outside)
	assert.Empty(t, entries, "nothing was written through the link")

	// catalog.json is a link: refused, and the target is untouched.
	root = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".vibe-local"), 0o700))
	target := filepath.Join(outside, "victim.json")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(target, SnapshotPath(root)))
	_, err = Snapshot(context.Background(), sess, root)
	require.Error(t, err)
	b, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	assert.Equal(t, "keep", string(b), "the link target is untouched")
}

// bigFile makes a DuckDB file of 3 schemas x 5 tables x 10 columns.
func bigFile(t *testing.T) session.Session {
	t.Helper()
	dir := t.TempDir()
	s, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	path := filepath.Join(dir, "big.duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	for _, sc := range []string{"s1", "s2", "s3"} {
		_, err = f.Exec(`CREATE SCHEMA ` + sc)
		require.NoError(t, err)
		for _, tb := range []string{"t1", "t2", "t3", "t4", "t5"} {
			cols := []string{}
			for c := 1; c <= 10; c++ {
				cols = append(cols, "c"+string(rune('a'+c))+" INTEGER")
			}
			_, err = f.Exec(`CREATE TABLE ` + sc + "." + tb + "(" + strings.Join(cols, ",") + ")")
			require.NoError(t, err)
		}
	}
	_, err = f.Exec(`CREATE TABLE tm (a INTEGER, b INTEGER, c INTEGER)`) // the default schema, main
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.Attach(context.Background(), session.Spec{Name: "big", Path: path}))
	return s
}

func TestBuildConnection_Caps(t *testing.T) {
	sess := bigFile(t)
	st, ok := sess.Status("big")
	require.True(t, ok)

	full, err := buildConnection(context.Background(), sess, st, DefaultCaps())
	require.NoError(t, err)
	assert.False(t, full.Truncated)
	require.NotNil(t, full.Schema("s2"))
	assert.Len(t, full.Schema("s2").Tables, 5)
	assert.Len(t, full.Schema("s2").Tables[0].Columns, 10)

	capped, err := buildConnection(context.Background(), sess, st, Caps{RelationsPerSchema: 2, ColumnsPerRelation: 3, Rows: 1000})
	require.NoError(t, err)
	assert.True(t, capped.Truncated)
	for _, sc := range []string{"s1", "s2", "s3"} {
		s := capped.Schema(sc)
		require.NotNil(t, s)
		require.Len(t, s.Tables, 2)
		assert.Equal(t, []string{"t1", "t2"}, []string{s.Tables[0].Name, s.Tables[1].Name})
		for _, r := range s.Tables {
			assert.Len(t, r.Columns, 3)
		}
	}

	rowCapped, err := buildConnection(context.Background(), sess, st, Caps{RelationsPerSchema: 100, ColumnsPerRelation: 100, Rows: 25})
	require.NoError(t, err)
	assert.True(t, rowCapped.Truncated)
	n := 0
	for _, s := range rowCapped.Schemas {
		for _, r := range s.Tables {
			n += len(r.Columns)
		}
	}
	assert.Equal(t, 25, n)
}

func TestCompletable(t *testing.T) {
	ok := session.Status{Name: "pg", Kind: session.StatusOK}
	assert.True(t, completable(ok, false, "local"))
	assert.False(t, completable(ok, true, "local"), "a storage connection has no catalog")
	assert.False(t, completable(session.Status{Name: "pg", Kind: session.StatusNotConnected}, false, "local"))
	assert.False(t, completable(session.Status{Name: "memory", Kind: session.StatusOK}, false, "local"))
	assert.True(t, completable(session.Status{Name: "memory", Kind: session.StatusOK}, false, "memory"))
}

func TestBuildConnection_RowCapReadsDefaultSchemaFirst(t *testing.T) {
	sess := bigFile(t)
	st, ok := sess.Status("big")
	require.True(t, ok)
	conn, err := buildConnection(context.Background(), sess, st, Caps{RelationsPerSchema: 100, ColumnsPerRelation: 100, Rows: 5})
	require.NoError(t, err)
	assert.True(t, conn.Truncated)
	require.NotNil(t, conn.Schema("main"), "the default schema is not starved by schemas that sort first")
	require.NotNil(t, conn.Schema("main").Relation("tm"))
	assert.Len(t, conn.Schema("main").Relation("tm").Columns, 3)
}
