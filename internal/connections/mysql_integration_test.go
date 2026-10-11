//go:build integration

package connections_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func myConnDef(port int, readOnly bool) workspace.ConnectionDef {
	return workspace.ConnectionDef{Name: "shop", Type: "mysql", Host: "127.0.0.1", Port: port, Database: "app",
		ReadOnly: &readOnly, Options: map[string]string{"ssl_mode": "required"}}
}

// The whole #119 MySQL story against a real server: add, sign in, Test, open
// (nothing dialled), query (dials on first use), join with local, READ_ONLY, no
// leaks.
func TestMySQLEndToEnd(t *testing.T) {
	my := startMySQL(t)
	my.mysql(t, "create table people(id int primary key, name varchar(40)); insert into people values (1,'ada'),(2,'bob')")
	port, _ := strconv.Atoi(my.port)
	ctx := context.Background()

	// Test: the Studio button.
	def := connections.Def{Name: "shop", Type: "mysql", Host: "127.0.0.1", Port: port, Database: "app", ReadOnly: true, Options: map[string]string{"ssl_mode": "required"}}
	res := connections.Test(ctx, def, secrets.Credential{User: "root", Password: myPassword},
		connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, 1, res.Count)
	assert.Equal(t, session.StatusOK, res.Status)
	assert.NotContains(t, res.Error+res.Detail, myPassword)

	// A wrong password: needs-credentials, and the needle is nowhere.
	const wrong = "WRONGpw_needle_9921"
	bad := connections.Test(ctx, def, secrets.Credential{User: "root", Password: wrong}, connections.WithTestExtensionDir(extDir(t)))
	require.False(t, bad.OK)
	assert.NotContains(t, bad.Error, wrong)
	assert.Equal(t, session.StatusNeedsCredentials, bad.Status, bad.Error)
	t.Logf("wrong password: %+v", bad)

	// An unreachable server is an error, not a hang.
	closed := def
	closed.Port = 1
	res = connections.Test(ctx, closed, secrets.Credential{User: "root", Password: myPassword}, connections.WithTestExtensionDir(extDir(t)))
	assert.False(t, res.OK)
	assert.NotContains(t, res.Error, myPassword)

	// In a workspace.
	ws, red := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, myConnDef(port, true)))
	require.NoError(t, ws.SetCredentials(ctx, "shop", secrets.Credential{User: "root", Password: myPassword}, false))
	fresh := t.TempDir()
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	require.Empty(t, issues)
	assert.Equal(t, session.StatusExtensionMissing, connStatus(sess, "shop").Kind, "nothing downloads until the person consents")
	require.NoError(t, sess.InstallExtension(ctx, "mysql"))
	sess.Preflight(ctx, "shop")
	assert.Equal(t, session.StatusNotConnected, connStatus(sess, "shop").Kind)
	_ = sess.Close()

	sess, issues, err = workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
	require.NoError(t, err)
	defer sess.Close()
	require.Empty(t, issues)
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM duckdb_databases() WHERE database_name = 'shop'").Scan(&n))
	assert.Equal(t, 0, n, "opening a workspace dials nothing")

	eng := engine.NewManager(sess)
	_, err = eng.Execute(ctx, "CREATE TABLE local.main.nums AS SELECT * FROM (VALUES (1, 10), (2, 20)) t(id, n)", nil)
	require.NoError(t, err)
	rows := queryAll(t, eng, `SELECT p.name, l.n FROM shop.app.people p JOIN local.main.nums l ON l.id = p.id ORDER BY p.id`)
	require.Len(t, rows, 2)
	assert.Equal(t, "ada", rows[0][0])
	assert.Equal(t, session.StatusOK, connStatus(sess, "shop").Kind)

	// READ_ONLY refuses a write.
	_, err = eng.Execute(ctx, "INSERT INTO shop.app.people VALUES (3, 'cy')", nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "read-only")
	assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))

	// No password anywhere DuckDB shows it, and no host either.
	rows = queryAll(t, eng, "SELECT coalesce(path, '') FROM duckdb_databases() WHERE database_name = 'shop'")
	require.Len(t, rows, 1)
	assert.NotContains(t, fmt.Sprint(rows[0][0]), myPassword)
	assert.NotContains(t, fmt.Sprint(rows[0][0]), "127.0.0.1")
	rows = queryAll(t, eng, "SELECT name, secret_string FROM duckdb_secrets() WHERE name = 'vibe_shop'")
	for _, r := range rows {
		assert.NotContains(t, fmt.Sprint(r...), myPassword)
	}
	assert.NotContains(t, red.Redact("password="+myPassword), myPassword)

	// The catalog listing, and a refresh sees DDL made behind our back.
	tables, err := sess.ListTables(ctx, "shop", "app")
	require.NoError(t, err)
	assert.Len(t, tables, 1)
	my.mysql(t, "create table later(x int)")
	require.NoError(t, sess.RefreshCatalog(ctx, "shop"))
	tables, _ = sess.ListTables(ctx, "shop", "app")
	assert.Len(t, tables, 2)

	// The person's own SQL (not the agent) may still use a passthrough: it is a
	// documented DuckDB feature and not covered by the agent policy.
	rows = queryAll(t, eng, "SELECT * FROM mysql_query('shop', 'select 1 as one')")
	require.Len(t, rows, 1)
}

func TestMySQLWrongPasswordInAWorkspaceIsRedacted(t *testing.T) {
	my := startMySQL(t)
	port, _ := strconv.Atoi(my.port)
	ws, red := memWS(t)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, myConnDef(port, true)))
	const wrong = "WRONGpw_needle_5521"
	require.NoError(t, ws.SetCredentials(ctx, "shop", secrets.Credential{User: "root", Password: wrong}, false))
	sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close()
	require.NoError(t, sess.InstallExtension(ctx, "mysql"))
	sess.Preflight(ctx, "shop")

	err = sess.Connect(ctx, "shop")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), wrong)
	assert.NotContains(t, red.Redact(err.Error()), wrong)
	st := connStatus(sess, "shop")
	assert.Equal(t, session.StatusNeedsCredentials, st.Kind, st.Message)
	assert.NotContains(t, st.Message, wrong)

	_, err = engine.NewManager(sess).Execute(ctx, "SELECT * FROM shop.app.x", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), wrong)
}
