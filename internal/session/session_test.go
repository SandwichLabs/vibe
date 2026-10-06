package session

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(t *testing.T, opts ...Option) Session {
	t.Helper()
	dir := t.TempDir()
	base := []Option{
		WithTempDirectory(filepath.Join(dir, "tmp")),
		WithExtensionDirectory(filepath.Join(dir, "ext")),
		WithSecretDirectory(filepath.Join(dir, "sec")),
	}
	s, err := New(append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func settingOf(t *testing.T, s Session, name string) string {
	t.Helper()
	var v sql.NullString
	require.NoError(t, s.DB().QueryRow("SELECT CAST(current_setting('"+name+"') AS VARCHAR)").Scan(&v))
	return v.String
}

func mkDuckFile(t *testing.T, name string, stmts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".duckdb")
	db, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	for _, s := range stmts {
		_, err := db.Exec(s)
		require.NoError(t, err, s)
	}
	require.NoError(t, db.Close())
	return path
}

func TestHardeningReadBack(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(t,
		WithTempDirectory(filepath.Join(dir, "t")),
		WithExtensionDirectory(filepath.Join(dir, "e")),
		WithSecretDirectory(filepath.Join(dir, "s")))
	assert.Equal(t, "false", settingOf(t, s, "allow_persistent_secrets"))
	assert.Equal(t, "false", settingOf(t, s, "autoinstall_known_extensions"))
	assert.Equal(t, "false", settingOf(t, s, "autoload_known_extensions"))
	assert.Equal(t, "false", settingOf(t, s, "allow_community_extensions"))
	assert.Equal(t, "true", settingOf(t, s, "lock_configuration"))
	assert.Equal(t, filepath.Join(dir, "t"), filepath.Clean(settingOf(t, s, "temp_directory")))
	assert.Equal(t, filepath.Join(dir, "e"), filepath.Clean(settingOf(t, s, "extension_directory")))
	assert.Equal(t, filepath.Join(dir, "s"), filepath.Clean(settingOf(t, s, "secret_directory")))

	_, err := s.Exec(context.Background(), "SET threads = 1")
	assert.Error(t, err, "configuration must be locked")
	_, err = s.Exec(context.Background(), "CREATE PERSISTENT SECRET p (TYPE http, BEARER_TOKEN 'x')")
	assert.Error(t, err, "persistent secrets must be disabled")
}

func TestWrapFailsClosedOnUnhardenedInstance(t *testing.T) {
	// A plain instance: autoload and persistent secrets are on by default.
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	dir := t.TempDir()
	_, err = Wrap(db, WithTempDirectory(filepath.Join(dir, "t")),
		WithExtensionDirectory(filepath.Join(dir, "e")), WithSecretDirectory(filepath.Join(dir, "s")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hardening did not take")
}

func TestWrapFailsClosedWhenOptionMismatch(t *testing.T) {
	dir := t.TempDir()
	// Opened with one temp directory, then wrapped expecting another.
	db, err := OpenDB("", WithTempDirectory(filepath.Join(dir, "a")), WithExtensionDirectory(filepath.Join(dir, "e")))
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	_, err = Wrap(db, WithTempDirectory(filepath.Join(dir, "b")), WithExtensionDirectory(filepath.Join(dir, "e")),
		WithSecretDirectory(filepath.Join(dir, "s")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "temp_directory")
}

func TestWrapKeepsCallersDB(t *testing.T) {
	dir := t.TempDir()
	opts := []Option{WithTempDirectory(filepath.Join(dir, "t")), WithExtensionDirectory(filepath.Join(dir, "e")),
		WithSecretDirectory(filepath.Join(dir, "s"))}
	path := filepath.Join(dir, "legacy.duckdb")
	db, err := OpenDB(path, opts...)
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	s, err := Wrap(db, opts...)
	require.NoError(t, err)
	assert.Equal(t, "legacy", s.DefaultCatalog())
	require.NoError(t, s.Close())
	require.NoError(t, db.Ping(), "Close must not close a wrapped instance")
}

func TestAttachDetachStatusAndJoin(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	assert.Equal(t, "memory", s.DefaultCatalog())
	a := mkDuckFile(t, "a", "CREATE TABLE people(id INTEGER, name VARCHAR)", "INSERT INTO people VALUES (1,'ann'),(2,'bob')")
	b := mkDuckFile(t, "b", "CREATE TABLE orders(person_id INTEGER, total INTEGER)", "INSERT INTO orders VALUES (1,10),(1,5),(2,7)")

	require.NoError(t, s.Register(Spec{Name: "lake", Path: a, ReadOnly: true}))
	st := s.Connections()
	require.Len(t, st, 2)
	assert.True(t, st[0].Default)
	assert.Equal(t, StatusNotConnected, st[1].Kind, "register must not dial")

	require.NoError(t, s.Connect(ctx, "lake"))
	require.NoError(t, s.Attach(ctx, Spec{Name: "sales", Path: b}))
	for _, c := range s.Connections() {
		assert.Equal(t, StatusOK, c.Kind, c.Name)
	}

	rows, err := s.Query(ctx, `SELECT p.name, sum(o.total) FROM lake.main.people p JOIN sales.main.orders o ON o.person_id = p.id GROUP BY 1 ORDER BY 1`)
	require.NoError(t, err)
	var got []string
	for rows.Next() {
		var n string
		var tot int64
		require.NoError(t, rows.Scan(&n, &tot))
		got = append(got, n+":"+string(rune('0'+tot/10))+string(rune('0'+tot%10)))
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, []string{"ann:15", "bob:07"}, got)

	// Catalog-aware introspection filters by database_name.
	cats, err := s.ListCatalogs(ctx)
	require.NoError(t, err)
	var names []string
	for _, c := range cats {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"memory", "lake", "sales"}, names)
	tabs, err := s.ListTables(ctx, "lake", "")
	require.NoError(t, err)
	require.Len(t, tabs, 1)
	assert.Equal(t, "people", tabs[0].Name)
	d, err := s.DescribeTable(ctx, "sales.main.orders")
	require.NoError(t, err)
	require.Len(t, d.Columns, 2)
	assert.Equal(t, "person_id", d.Columns[0].Name)
	// A read-only connection refuses writes.
	_, err = s.Exec(ctx, "DELETE FROM lake.main.people")
	assert.Error(t, err)

	// Name that is not a valid alias, and an alias attached twice.
	assert.Error(t, s.Attach(ctx, Spec{Name: `x"; DROP`, Path: a}))
	assert.Error(t, s.Attach(ctx, Spec{Name: "lake", Path: a}))

	require.NoError(t, s.Detach(ctx, "lake"))
	_, err = s.Query(ctx, "SELECT * FROM lake.main.people")
	assert.Error(t, err)
	assert.Error(t, s.Detach(ctx, "memory"), "default catalog cannot be detached")
	assert.Len(t, s.Connections(), 2)
}

func TestAttachQuotesPathAndName(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "it's \"odd\".duckdb")
	db, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE t(x INT)")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, s.Attach(ctx, Spec{Name: "odd", Path: path}))
	_, err = s.Exec(ctx, "INSERT INTO odd.main.t VALUES (1)")
	require.NoError(t, err)
}

func TestAttachErrorStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	err := s.Attach(ctx, Spec{Name: "ghost", Path: filepath.Join(t.TempDir(), "nope.duckdb"), ReadOnly: true})
	require.Error(t, err)
	var ghost Status
	for _, c := range s.Connections() {
		if c.Name == "ghost" {
			ghost = c
		}
	}
	assert.Equal(t, StatusError, ghost.Kind)
	assert.NotEmpty(t, ghost.Message)
	assert.Error(t, s.Register(Spec{Name: "t", Type: "carrier-pigeon", Path: "x"}))
}

func TestSchemaCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	list := func() []string {
		ts, err := s.ListTables(ctx, "", "")
		require.NoError(t, err)
		var n []string
		for _, x := range ts {
			n = append(n, x.Name)
		}
		return n
	}
	assert.Empty(t, list())
	h0, _ := CacheStats(s)
	assert.Empty(t, list())
	h1, _ := CacheStats(s)
	assert.Greater(t, h1, h0, "second listing must hit the cache")

	_, err := s.Exec(ctx, "CREATE TABLE t1(a INT)")
	require.NoError(t, err)
	assert.Equal(t, []string{"t1"}, list(), "session DDL invalidates")

	// DDL that bypasses the session is still seen via the content fingerprint.
	_, err = s.DB().Exec("CREATE TABLE t2(a INT)")
	require.NoError(t, err)
	assert.Equal(t, []string{"t1", "t2"}, list())
	d, err := s.DescribeTable(ctx, "t1")
	require.NoError(t, err)
	assert.Len(t, d.Columns, 1)
	_, err = s.DB().Exec("ALTER TABLE t1 ADD COLUMN b INT")
	require.NoError(t, err)
	d, err = s.DescribeTable(ctx, "t1")
	require.NoError(t, err)
	assert.Len(t, d.Columns, 2, "ALTER must not serve a stale description")

	// Attach / detach invalidate.
	p := mkDuckFile(t, "x", "CREATE TABLE xt(a INT)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "x", Path: p}))
	xt, err := s.ListTables(ctx, "x", "")
	require.NoError(t, err)
	assert.Len(t, xt, 1)
	require.NoError(t, s.Detach(ctx, "x"))
	_, err = s.ListTables(ctx, "x", "main")
	assert.Error(t, err, "a detached connection is unknown")
}

func TestPinnedConnUseAndRestore(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	p := mkDuckFile(t, "other", "CREATE TABLE only_there(a INT)", "INSERT INTO only_there VALUES (42)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "other", Path: p}))

	// Unqualified name resolves against the run's catalog.
	rows, err := s.QueryWith(ctx, "SELECT a FROM only_there", nil, WithCatalog("other"))
	require.NoError(t, err)
	require.True(t, rows.Next())
	var a int
	require.NoError(t, rows.Scan(&a))
	assert.Equal(t, 42, a)
	require.NoError(t, rows.Close())

	// Unset means the session default, and pooled connections were restored.
	db := s.DB()
	db.SetMaxOpenConns(1) // force reuse of the pinned connection
	_, err = s.Query(ctx, "SELECT a FROM only_there")
	assert.Error(t, err, "USE must not leak into the pool")
	var cat string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&cat))
	assert.Equal(t, "memory", cat)

	c, err := s.Conn(ctx, WithCatalog("other"))
	require.NoError(t, err)
	_, err = c.Exec(ctx, "INSERT INTO only_there VALUES (43)")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	_, err = s.Conn(ctx, WithCatalog("missing"))
	assert.Error(t, err)
}

func TestTimeoutCancelsStatement(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	start := time.Now()
	rows, err := s.QueryWith(ctx, "SELECT count(*) FROM range(100000000000) a, range(1000) b", nil, WithTimeout(300*time.Millisecond))
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		_ = rows.Close()
	}
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "statement must be interrupted")

	// Cancelling the caller's context also stops it.
	cctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_, err = s.ExecWith(cctx, "CREATE TABLE big AS SELECT count(*) FROM range(100000000000) a, range(1000) b", nil)
	assert.Error(t, err)

	// The session still works afterwards.
	var one int
	require.NoError(t, s.DB().QueryRow("SELECT 1").Scan(&one))
}

func TestRowCap(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	count := func(rows *Rows) (int, bool) {
		n := 0
		for rows.Next() {
			n++
		}
		require.NoError(t, rows.Err())
		tr := rows.Truncated()
		require.NoError(t, rows.Close())
		return n, tr
	}
	rows, err := s.QueryWith(ctx, "SELECT * FROM range(100)", nil, WithRowCap(10))
	require.NoError(t, err)
	n, tr := count(rows)
	assert.Equal(t, 10, n)
	assert.True(t, tr)

	rows, err = s.QueryWith(ctx, "SELECT * FROM range(10)", nil, WithRowCap(10))
	require.NoError(t, err)
	n, tr = count(rows)
	assert.Equal(t, 10, n)
	assert.False(t, tr, "exactly at the cap is not truncation")

	rows, err = s.Query(ctx, "SELECT * FROM range(100)")
	require.NoError(t, err)
	n, _ = count(rows)
	assert.Equal(t, 100, n)

	capped := newTestSession(t, WithDefaultRowCap(5))
	rows, err = capped.Query(ctx, "SELECT * FROM range(100)")
	require.NoError(t, err)
	n, tr = count(rows)
	assert.Equal(t, 5, n)
	assert.True(t, tr)
}

func TestPerConnectionDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	p := mkDuckFile(t, "capd", "CREATE TABLE t(a INT)", "INSERT INTO t SELECT * FROM range(50)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "capd", Path: p, RowCap: 7}))
	rows, err := s.QueryWith(ctx, "SELECT * FROM t", nil, WithCatalog("capd"))
	require.NoError(t, err)
	n := 0
	for rows.Next() {
		n++
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, 7, n)
}

func TestNewCleansUpTempDir(t *testing.T) {
	dir := t.TempDir()
	s, err := New(WithExtensionDirectory(filepath.Join(dir, "e")), WithSecretDirectory(filepath.Join(dir, "s")))
	require.NoError(t, err)
	h, ok := s.(*hub)
	require.True(t, ok)
	require.DirExists(t, h.tempDir)
	require.NoError(t, s.Close())
	assert.NoDirExists(t, h.tempDir)
}

type failingExecer struct{ failOn string }

func (f failingExecer) ExecContext(_ context.Context, q string, _ ...any) (sql.Result, error) {
	if strings.HasPrefix(q, f.failOn) {
		return nil, errors.New("network unreachable")
	}
	return nil, nil
}

func TestEnsureExtension_FailureIsVisible(t *testing.T) {
	err := EnsureExtension(context.Background(), failingExecer{failOn: "INSTALL"}, "excel", "Exporting to Excel")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one-time download")
	assert.Contains(t, err.Error(), "about 2 MB")
	assert.Contains(t, err.Error(), "network unreachable")

	err = EnsureExtension(context.Background(), failingExecer{failOn: "LOAD"}, "excel", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not load")

	assert.Error(t, EnsureExtension(context.Background(), failingExecer{failOn: "NEVER"}, `excel; DROP`, "x"), "extension names are validated")
	assert.NoError(t, EnsureExtension(context.Background(), failingExecer{failOn: "NEVER"}, "excel", "x"))
}

// A USE issued through the pool must not leak: unqualified writes keep landing
// in the default catalog afterwards, and a TEMP table cannot shadow a
// fully-qualified reference.
func TestPoolUSEDoesNotPoisonDefaultCatalog(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.DB().SetMaxOpenConns(1) // every statement reuses the one connection
	p := mkDuckFile(t, "b", "CREATE TABLE people (id INT)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "b", Path: p}))
	_, err := s.Exec(ctx, "CREATE TABLE people (id INT)")
	require.NoError(t, err)

	_, err = s.Exec(ctx, "USE b")
	require.NoError(t, err)
	_, err = s.Exec(ctx, "INSERT INTO people VALUES (1)")
	require.NoError(t, err)
	var inDefault, inB int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM memory.main.people").Scan(&inDefault))
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM b.main.people").Scan(&inB))
	assert.Equal(t, 1, inDefault, "the write landed in the default catalog")
	assert.Equal(t, 0, inB)

	// Same through Query (a USE statement run as a query), and a schema switch.
	rows, err := s.Query(ctx, "USE b")
	require.NoError(t, err)
	require.NoError(t, rows.Close())
	_, err = s.Exec(ctx, "INSERT INTO people VALUES (2)")
	require.NoError(t, err)
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM memory.main.people").Scan(&inDefault))
	assert.Equal(t, 2, inDefault)

	// A temp table shadows the bare name but not the resolved one.
	_, err = s.Exec(ctx, "CREATE TEMP TABLE people (id INT)")
	require.NoError(t, err)
	ref, err := s.Resolve(ctx, TableRef{Name: "people"})
	require.NoError(t, err)
	assert.Equal(t, TableRef{Catalog: "memory", Schema: "main", Name: "people"}, ref)
	_, err = s.Exec(ctx, "INSERT INTO "+ref.SQL()+" VALUES (3)")
	require.NoError(t, err)
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM memory.main.people").Scan(&inDefault))
	assert.Equal(t, 3, inDefault)
}

func TestResolveRejectsInternalCatalogs(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	for _, c := range []string{"system", "temp", "ghost"} {
		_, err := s.Resolve(ctx, TableRef{Catalog: c, Schema: "main", Name: "t"})
		assert.Error(t, err, c)
		_, err = s.ListTables(ctx, c, "")
		assert.Error(t, err, c)
	}
}

func TestQueryWithDDLInvalidatesSchemaCache(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	tabs, err := s.ListTables(ctx, "", "")
	require.NoError(t, err)
	assert.Empty(t, tabs)
	rows, err := s.Query(ctx, "CREATE TABLE viaquery (a INT)")
	require.NoError(t, err)
	require.NoError(t, rows.Close())
	tabs, err = s.ListTables(ctx, "", "")
	require.NoError(t, err)
	assert.Len(t, tabs, 1)
}

// UnhardenedForTests skips the hardening read-back; only the sessiontest
// package (used from _test.go files) may call it.
func TestUnhardenedOnlyUsedByTests(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		src := string(b)
		slash := filepath.ToSlash(path)
		if strings.Contains(src, "UnhardenedForTests") && !strings.HasSuffix(slash, "session/testing.go") && !strings.Contains(slash, "session/sessiontest/") {
			offenders = append(offenders, path)
		}
		if strings.Contains(src, "session/sessiontest\"") && !strings.Contains(slash, "session/sessiontest/") {
			offenders = append(offenders, path)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "production code must not use the unhardened session")
}

// A BEGIN with no COMMIT must not stay open on a pooled connection.
func TestOpenTransactionIsRolledBackOnRelease(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	_, err := s.Exec(ctx, "CREATE TABLE t (a INT)")
	require.NoError(t, err)

	// One run, one transaction: BEGIN with no COMMIT.
	_, err = s.Exec(ctx, "BEGIN; INSERT INTO t VALUES (1); CREATE TABLE ghost (a INT)")
	require.NoError(t, err)

	var n int
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 0, n, "no uncommitted write survives the release")
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_name = 'ghost'").Scan(&n))
	assert.Equal(t, 0, n, "no uncommitted DDL survives either")
	_, err = s.DB().Exec("CHECKPOINT")
	assert.NoError(t, err, "CHECKPOINT is not blocked by a leaked transaction")
	_, err = s.Exec(ctx, "INSERT INTO t VALUES (2)")
	require.NoError(t, err, "later writes work")
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 1, n)

	// An aborted transaction does not poison the next statement either.
	_, err = s.Exec(ctx, "BEGIN; SELECT * FROM missing_table")
	assert.Error(t, err)
	_, err = s.Exec(ctx, "INSERT INTO t VALUES (3)")
	require.NoError(t, err)

	// BEGIN; ...; COMMIT in one run still commits, and a streamed read is
	// not cut short by the release.
	_, err = s.Exec(ctx, "BEGIN; INSERT INTO t VALUES (4); COMMIT")
	require.NoError(t, err)
	rows, err := s.Query(ctx, "SELECT * FROM range(5000)")
	require.NoError(t, err)
	c := 0
	for rows.Next() {
		c++
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, 5000, c)
	require.NoError(t, s.DB().QueryRow("SELECT count(*) FROM t WHERE a = 4").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestSetDefaultCatalog(t *testing.T) {
	s := newTestSession(t)
	ctx := context.Background()
	path := mkDuckFile(t, "wsdata", "CREATE TABLE t(x INT)", "INSERT INTO t VALUES (7)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "wsdata", Type: "duckdb", Path: path}))
	require.NoError(t, s.SetDefaultCatalog(ctx, "wsdata"))
	assert.Equal(t, "wsdata", s.DefaultCatalog())

	// Every pooled connection, new or recycled, starts in the new default.
	for i := 0; i < 4; i++ {
		var cat string
		require.NoError(t, s.DB().QueryRow("SELECT current_database()").Scan(&cat))
		assert.Equal(t, "wsdata", cat)
	}
	rows, err := s.Query(ctx, "SELECT x FROM t")
	require.NoError(t, err)
	assert.True(t, rows.Next())
	require.NoError(t, rows.Close())

	// A pinned connection returns to the new default.
	otherPath := mkDuckFile(t, "pinned", "CREATE TABLE p(x INT)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "pinned", Type: "duckdb", Path: otherPath}))
	c, err := s.Conn(ctx, WithCatalog("pinned"))
	require.NoError(t, err)
	require.NoError(t, c.Close())
	var cat string
	require.NoError(t, s.DB().QueryRow("SELECT current_database()").Scan(&cat))
	assert.Equal(t, "wsdata", cat)

	var defaults int
	for _, st := range s.Connections() {
		if st.Default {
			defaults++
			assert.Equal(t, "wsdata", st.Name)
		}
	}
	assert.Equal(t, 1, defaults)

	assert.Error(t, s.SetDefaultCatalog(ctx, "nope"))
}

// TestListSchemasIncludesMainAndExcludesSystemSchemas: DuckDB 1.4 marks "main"
// internal, so it must be listed by name rules, not by the internal flag.
func TestListSchemasIncludesMainAndExcludesSystemSchemas(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	a := mkDuckFile(t, "a", "CREATE TABLE people(id INTEGER)", "CREATE SCHEMA archive", "CREATE TABLE archive.old(id INTEGER)")
	require.NoError(t, s.Attach(ctx, Spec{Name: "sales", Path: a}))

	schemas, err := s.ListSchemas(ctx, "sales")
	require.NoError(t, err)
	assert.Equal(t, []string{"archive", "main"}, schemas)

	all, err := AllTables(ctx, s, "sales")
	require.NoError(t, err)
	var names []string
	for _, ti := range all {
		names = append(names, ti.Schema+"."+ti.Name)
	}
	assert.ElementsMatch(t, []string{"main.people", "archive.old"}, names)
}
