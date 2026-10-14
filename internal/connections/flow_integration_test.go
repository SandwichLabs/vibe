//go:build integration

package connections_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func memWS(t *testing.T) (workspace.Workspace, *secrets.Redactor) {
	t.Helper()
	res, err := workspace.Init(filepath.Join(t.TempDir(), "acme"))
	require.NoError(t, err)
	_ = res.Workspace.Close()
	red := secrets.NewRedactor()
	r := secrets.NewResolver(secrets.WithKeychain(secrets.NewMemoryStore()), secrets.WithRedactor(red))
	ws, err := workspace.Open(res.Workspace.Root(), workspace.WithConfigDir(t.TempDir()),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(r))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws, red
}

func queryAll(t *testing.T, eng *engine.Manager, sqlText string) [][]any {
	t.Helper()
	rs, err := eng.Execute(context.Background(), sqlText, nil)
	require.NoError(t, err, sqlText)
	return rs.Rows
}

// The whole #115 story against a real Postgres: add, sign in, open (nothing
// dialled), query (dials on first use), join with local, read-only, no leaks.
func TestPostgresEndToEnd(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, "create table people(id int primary key, name text); insert into people values (1,'ada'),(2,'bob')")
	port, _ := strconv.Atoi(pg.port)
	ws, _ := memWS(t)
	ctx := context.Background()

	// A fresh extension directory: the connection reports extension-missing
	// and nothing is downloaded until the person consents.
	fresh := t.TempDir()
	def := workspace.ConnectionDef{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres",
		Options: map[string]string{"sslmode": "disable"}}
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, def))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "postgres", Password: pgPassword}, false))

	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.Empty(t, issues)
	st := connStatus(sess, "pg")
	assert.Equal(t, session.StatusExtensionMissing, st.Kind)
	require.Error(t, sess.Connect(ctx, "pg"))
	require.NoError(t, sess.InstallExtension(ctx, "postgres"), "install is the consent step")
	sess.Preflight(ctx, "pg")
	assert.Equal(t, session.StatusNotConnected, connStatus(sess, "pg").Kind, "installed and signed in, but nothing dialled yet")
	_ = sess.Close()

	// A shared cache keeps the rest of the run quick.
	sess, issues, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	defer sess.Close()
	require.Empty(t, issues)
	assert.Equal(t, session.StatusNotConnected, connStatus(sess, "pg").Kind)
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM duckdb_databases() WHERE database_name = 'pg'").Scan(&n))
	assert.Equal(t, 0, n, "opening a workspace dials nothing remote")

	eng := engine.NewManager(sess)
	// First use dials: a qualified name triggers the attach.
	_, err = eng.Execute(ctx, "CREATE TABLE local.main.nums AS SELECT * FROM (VALUES (1, 10), (2, 20)) t(id, n)", nil)
	require.NoError(t, err)
	rows := queryAll(t, eng, `SELECT p.name, l.n FROM pg.public.people p JOIN local.main.nums l ON l.id = p.id ORDER BY p.id`)
	require.Len(t, rows, 2)
	assert.Equal(t, "ada", rows[0][0])
	assert.Equal(t, session.StatusOK, connStatus(sess, "pg").Kind)

	// READ_ONLY refuses a write through ordinary SQL.
	_, err = eng.Execute(ctx, "INSERT INTO pg.public.people VALUES (3, 'cy')", nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "read-only")

	// No password anywhere DuckDB shows it.
	rows = queryAll(t, eng, "SELECT coalesce(path, '') FROM duckdb_databases() WHERE database_name = 'pg'")
	require.Len(t, rows, 1)
	assert.NotContains(t, fmt.Sprint(rows[0][0]), pgPassword)
	assert.NotContains(t, fmt.Sprint(rows[0][0]), "127.0.0.1")
	rows = queryAll(t, eng, "SELECT name, secret_string FROM duckdb_secrets() WHERE name = 'vibe_pg'")
	for _, r := range rows {
		assert.NotContains(t, fmt.Sprint(r...), pgPassword)
	}

	// The catalog listing sees PG's tables, and a refresh sees DDL made behind our back.
	tables, err := sess.ListTables(ctx, "pg", "public")
	require.NoError(t, err)
	assert.Len(t, tables, 1)
	pg.psql(t, "create table later(x int)")
	tables, _ = sess.ListTables(ctx, "pg", "public")
	assert.Len(t, tables, 1, "stale until refreshed")
	require.NoError(t, sess.RefreshCatalog(ctx, "pg"))
	tables, _ = sess.ListTables(ctx, "pg", "public")
	assert.Len(t, tables, 2)
}

func TestPostgresWrongPasswordIsRedactedAndNeedsCredentials(t *testing.T) {
	pg := startPG(t)
	port, _ := strconv.Atoi(pg.port)
	ws, red := memWS(t)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres"}))
	const wrong = "WRONGpw_needle_5521"
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "postgres", Password: wrong}, false))
	sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close()
	require.NoError(t, sess.InstallExtension(ctx, "postgres"))
	sess.Preflight(ctx, "pg")

	err = sess.Connect(ctx, "pg")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), wrong)
	assert.Contains(t, err.Error(), `for user "postgres"`, "the UI (the person who owns the connection) still sees the user name")
	assert.NotContains(t, agents.NewErrorResponse(1, agents.ErrInternal, err.Error()).Error.Message, `"postgres"`, "text for the agent and MCP clients does not")
	st := connStatus(sess, "pg")
	assert.Equal(t, session.StatusNeedsCredentials, st.Kind)
	assert.NotContains(t, st.Message, wrong)
	assert.NotContains(t, red.Redact(err.Error()), wrong)

	// Through the engine too.
	_, err = engine.NewManager(sess).Execute(ctx, "SELECT * FROM pg.public.x", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), wrong)
}

func TestPostgresSSLRequireAgainstPlainServerFails(t *testing.T) {
	pg := startPG(t)
	port, _ := strconv.Atoi(pg.port)
	def := connDef("pg", port, map[string]string{"sslmode": "require"})
	res := testConn(t, def, secrets.Credential{User: "postgres", Password: pgPassword})
	assert.False(t, res.OK, "sslmode is honoured: a server without TLS is refused")
	assert.NotContains(t, res.Error, pgPassword)
}

func TestMovedConnectionStillSignsIn(t *testing.T) {
	pg := startPG(t)
	port, _ := strconv.Atoi(pg.port)
	ws, _ := memWS(t)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres"}))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "postgres", Password: pgPassword}, false))
	require.NoError(t, ws.MoveConnection(ctx, "pg", workspace.ScopeLocal, workspace.ScopeShared))
	sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close()
	require.NoError(t, sess.InstallExtension(ctx, "postgres"))
	require.NoError(t, sess.Connect(ctx, "pg"))
}

func connStatus(s session.Session, name string) session.Status {
	for _, st := range s.Connections() {
		if st.Name == name {
			return st
		}
	}
	return session.Status{}
}
