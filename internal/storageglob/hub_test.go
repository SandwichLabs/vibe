package storageglob_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/connections"
	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/secrets"
	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/storageglob"
)

// These tests run the whole path, offline: a real hub with a real storage
// connection, DuckDB's real httpfs, and loopback servers standing in for S3.
// They never reach AWS. httpfs must be installed (CI installs it); without it
// they skip locally.

// hostExtensionDir finds an extension directory that holds httpfs.
func hostExtensionDir(t *testing.T) string {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	var ver, platform string
	require.NoError(t, db.QueryRow("SELECT library_version, platform FROM pragma_version(), pragma_platform()").Scan(&ver, &platform))
	appDir, _ := session.DefaultDirs()
	home, _ := os.UserHomeDir()
	for _, dir := range []string{appDir, filepath.Join(home, ".duckdb", "extensions")} {
		if _, err := os.Stat(filepath.Join(dir, ver, platform, "httpfs.duckdb_extension")); err == nil {
			return dir
		}
	}
	if os.Getenv("CI") == "true" {
		t.Fatalf("httpfs %s/%s is not installed; CI installs it before the tests", ver, platform)
	}
	t.Skipf("httpfs %s/%s is not installed; skipping", ver, platform)
	return ""
}

// fakeS3 is a path-style S3 endpoint over a map of key -> body. It serves
// ListObjectsV2 (every key under the prefix, no delimiter handling) and object
// GET/HEAD, with ranges, and records each request.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]string // "bucket/key" -> body
	reqs    []string
	Host    string
}

func startFakeS3(t *testing.T, objects map[string]string) *fakeS3 {
	t.Helper()
	f := &fakeS3{objects: objects}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.Host = strings.TrimPrefix(srv.URL, "http://")
	return f
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.reqs = append(f.reqs, fmt.Sprintf("%s %s auth=%s", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")))
	f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/")
	if r.URL.Query().Get("list-type") == "2" {
		bucket := strings.TrimSuffix(path, "/")
		prefix := r.URL.Query().Get("prefix")
		type content struct {
			Key  string
			Size int
		}
		var res struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			IsTruncated bool
			Contents    []content
		}
		var keys []string
		for k := range f.objects {
			if b, key, _ := strings.Cut(k, "/"); b == bucket && strings.HasPrefix(key, prefix) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			res.Contents = append(res.Contents, content{k, len(f.objects[bucket+"/"+k])})
		}
		w.Header().Set("Content-Type", "application/xml")
		_ = xml.NewEncoder(w).Encode(res)
		return
	}
	body, ok := f.objects[path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	http.ServeContent(w, r, "", time.Unix(0, 0), bytes.NewReader([]byte(body)))
}

func (f *fakeS3) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fakeS3) listings() int {
	n := 0
	for _, r := range f.requests() {
		if strings.Contains(r, "list-type=2") {
			n++
		}
	}
	return n
}

// lakeSession opens a hub with one storage connection "lake" on the fake S3.
func lakeSession(t *testing.T, scope string, s3 *fakeS3) session.Session {
	t.Helper()
	sess, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(hostExtensionDir(t)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	addLake(t, sess, "lake", scope, s3)
	return sess
}

func addLake(t *testing.T, sess session.Session, name, scope string, s3 *fakeS3) {
	t.Helper()
	spec := connections.Def{Name: name, Type: "s3", Scope: scope, Region: "us-east-1",
		Options: map[string]string{"endpoint": s3.Host, "url_style": "path", "use_ssl": "false"}}.Spec()
	spec.Credential = func(context.Context) (secrets.Credential, error) {
		return secrets.Credential{KeyID: "OWNKEYID", Secret: "OWNSECRET"}, nil
	}
	require.NoError(t, sess.Attach(context.Background(), spec))
}

func count(t *testing.T, eng *engine.Manager, q string) (int64, error) {
	t.Helper()
	rs, err := eng.Execute(context.Background(), q, nil)
	if err != nil {
		return 0, err
	}
	require.Len(t, rs.Rows, 1)
	n, ok := rs.Rows[0][0].(int64)
	require.True(t, ok, "%T", rs.Rows[0][0])
	return n, nil
}

func TestWildcardsRunThroughTheSealedHub(t *testing.T) {
	evil := startFakeS3(t, nil)
	s3 := startFakeS3(t, map[string]string{
		"b/dir/a.csv": "id\n1\n",
		"b/dir/b.csv": "id\n2\n",
		// A hostile name: S3 allows "?" in a key. It is only a name.
		"b/hostile/x.csv?s3_endpoint=" + evil.Host: "id\n3\n",
		"b/dir/sub/c.csv":                          "id\n4\n",
		"b/top.csv":                                "id\n5\n",
	})
	sess := lakeSession(t, "s3://b/", s3)
	eng := engine.NewManager(sess)

	var on string
	require.NoError(t, sess.DB().QueryRow("SELECT CAST(current_setting('s3_url_compatibility_mode') AS VARCHAR)").Scan(&on))
	require.Equal(t, "true", on, "the hub reads every URL as a literal key")

	t.Run("reader with a star", func(t *testing.T) {
		n, err := count(t, eng, `SELECT count(*) FROM read_csv('s3://b/dir/*')`)
		require.NoError(t, err)
		assert.EqualValues(t, 2, n, "a and b; not sub/c.csv")
	})
	t.Run("recursive", func(t *testing.T) {
		n, err := count(t, eng, `SELECT count(*) FROM read_csv('s3://b/dir/**')`)
		require.NoError(t, err)
		assert.EqualValues(t, 3, n)
	})
	t.Run("FROM pattern", func(t *testing.T) {
		n, err := count(t, eng, `SELECT count(*) FROM 's3://b/dir/*.csv'`)
		require.NoError(t, err)
		assert.EqualValues(t, 2, n)
	})
	t.Run("glob() returns the keys, column file", func(t *testing.T) {
		rs, err := eng.Execute(context.Background(), `SELECT file FROM glob('s3://b/dir/*.csv') ORDER BY file`, nil)
		require.NoError(t, err)
		require.Len(t, rs.Rows, 2)
		assert.Equal(t, "s3://b/dir/a.csv", rs.Rows[0][0])
		assert.Equal(t, "s3://b/dir/b.csv", rs.Rows[1][0])
		n, err := count(t, eng, `SELECT count(*) FROM glob('s3://b/dir/**')`)
		require.NoError(t, err)
		assert.EqualValues(t, 3, n)
	})
	t.Run("a list of patterns, a named option, CTAS", func(t *testing.T) {
		_, err := eng.Execute(context.Background(), `CREATE TABLE got AS SELECT * FROM read_csv(['s3://b/dir/*.csv', 's3://b/*.csv'], header = true)`, nil)
		require.NoError(t, err)
		n, err := count(t, eng, `SELECT count(*) FROM got`)
		require.NoError(t, err)
		assert.EqualValues(t, 3, n)
	})
	t.Run("an object named like a redirect is refused, and read only by its exact name", func(t *testing.T) {
		_, err := eng.Execute(context.Background(), `SELECT count(*) FROM read_csv('s3://b/hostile/*')`, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1 of the objects")
		assert.Contains(t, err.Error(), "? or # in the name")
		n, err := count(t, eng, `SELECT count(*) FROM read_csv('s3://b/hostile/x.csv?s3_endpoint=`+evil.Host+`')`)
		require.NoError(t, err, "the exact name is a literal key on the connection's own endpoint")
		assert.EqualValues(t, 1, n)
	})
	t.Run("COPY FROM a pattern is not expanded", func(t *testing.T) {
		_, err := eng.Execute(context.Background(), `COPY got FROM 's3://b/dir/*.csv'`, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "INSERT INTO t SELECT")
	})
	t.Run("a pattern in a value is a value", func(t *testing.T) {
		rs, err := eng.Execute(context.Background(), `SELECT 's3://b/dir/*' AS p`, nil)
		require.NoError(t, err)
		assert.Equal(t, "s3://b/dir/*", rs.Rows[0][0])
	})
	t.Run("nothing matched", func(t *testing.T) {
		_, err := eng.Execute(context.Background(), `SELECT * FROM read_csv('s3://b/nothing/*')`, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no files found that match the pattern "s3://b/nothing/*"`)
		n, err := count(t, eng, `SELECT count(*) FROM glob('s3://b/nothing/*')`)
		require.NoError(t, err)
		assert.EqualValues(t, 0, n)
	})
	t.Run("an error quotes the pattern, not the keys", func(t *testing.T) {
		_, err := eng.Execute(context.Background(), `SELECT nosuchcolumn FROM read_csv('s3://b/dir/*.csv')`, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nosuchcolumn")
		assert.NotContains(t, err.Error(), "s3://b/dir/a.csv")
		assert.Contains(t, err.Error(), "LINE 1: (the statement, with each wildcard expanded")
	})
	t.Run("no covering connection is never listed", func(t *testing.T) {
		before := s3.listings()
		_, err := eng.Execute(context.Background(), `SELECT * FROM read_csv('s3://other-bucket/*')`, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no storage connection")
		assert.Equal(t, before, s3.listings())
	})
	t.Run("url parameters in a pattern", func(t *testing.T) {
		_, err := eng.Execute(context.Background(), `SELECT * FROM read_csv('s3://b/dir/*?s3_endpoint=`+evil.Host+`&s3_use_ssl=false')`, nil)
		require.Error(t, err)
	})

	assert.Empty(t, evil.requests(), "the name's ?s3_endpoint= reached nobody: the host it named saw nothing")
	hostile := 0
	for _, r := range s3.requests() {
		if strings.Contains(r, "x.csv%3Fs3_endpoint") {
			hostile++
		}
		assert.NotContains(t, r, "auth=AWS4-HMAC-SHA256 Credential=Q/", "no foreign key id")
	}
	assert.Positive(t, hostile, "the hostile object was read by its exact name from the connection's own endpoint")
}

func TestListMatchingPicksTheCoveringConnection(t *testing.T) {
	s3 := startFakeS3(t, map[string]string{"b/dir/a.csv": "id\n1\n", "b/dir/deep/z.csv": "id\n2\n", "c/x.csv": "id\n3\n"})
	other := startFakeS3(t, map[string]string{"b/dir/deep/other.csv": "id\n9\n"})
	sess := lakeSession(t, "s3://b/", s3)
	addLake(t, sess, "deep", "s3://b/dir/deep/", other)
	lister, ok := sess.(storageglob.Lister)
	require.True(t, ok)
	ctx := context.Background()

	keys, err := lister.ListMatching(ctx, "s3://b/dir/*", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3://b/dir/a.csv"}, keys)

	// The longest scope wins, as it does when the hub reads: "deep" holds the
	// secret that matches this pattern, and its endpoint is the one asked.
	keys, err = lister.ListMatching(ctx, "s3://b/dir/deep/*", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3://b/dir/deep/other.csv"}, keys)
	assert.Positive(t, other.listings())

	// A pattern's "*" cannot reach outside a scope's text.
	_, err = lister.ListMatching(ctx, "s3://*/x.csv", 100)
	require.Error(t, err)
	_, err = lister.ListMatching(ctx, "s3://c/*", 100)
	require.Error(t, err, "no connection's scope starts it")
	assert.Contains(t, err.Error(), "no storage connection")

	// The pattern is checked before any session sees it.
	for _, p := range []string{
		"s3://b/dir/*?s3_endpoint=evil.example", "s3://b/dir/[ab]*", "s3://b/dir/a?.csv*", "s3://b/dir/../*", "s3://b/dir/*#x", "az://c/*", "/tmp/*",
	} {
		_, err = lister.ListMatching(ctx, p, 100)
		assert.Error(t, err, p)
	}

	// limit+1 keys come back when there are more.
	keys, err = lister.ListMatching(ctx, "s3://b/**", 1)
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	// Cancellation is honoured.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = lister.ListMatching(cctx, "s3://b/dir/*", 100)
	require.Error(t, err)
}

func TestListMatchingNeedsAReadyConnection(t *testing.T) {
	s3 := startFakeS3(t, map[string]string{"b/a.csv": "id\n1\n"})
	sess, err := session.New(session.WithConnectionTypes(connections.Types()...), session.WithExtensionDirectory(hostExtensionDir(t)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	spec := connections.Def{Name: "lake", Type: "s3", Scope: "s3://b/", Region: "us-east-1",
		Options: map[string]string{"endpoint": s3.Host, "url_style": "path", "use_ssl": "false"}}.Spec()
	spec.Credential = func(context.Context) (secrets.Credential, error) { return secrets.Credential{}, secrets.ErrNotFound }
	require.NoError(t, sess.Register(spec))
	sess.Preflight(context.Background(), "lake")
	lister, ok := sess.(storageglob.Lister)
	require.True(t, ok)
	_, err = lister.ListMatching(context.Background(), "s3://b/*", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
	assert.Zero(t, s3.listings(), "a connection without credentials lists nothing, and never with the host's")
}
