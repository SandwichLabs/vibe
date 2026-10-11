//go:build integration

package agents_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func openMySQL(t *testing.T, readOnly bool) (*mysqlServer, workspace.Workspace, session.Session) {
	t.Helper()
	my := startMySQL(t)
	my.mysql(t, "create table people(id int primary key, name varchar(40)); insert into people values (1,'ada'),(2,'bob')")
	port, _ := strconv.Atoi(my.port)
	ws := memWS(t)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "shop", Type: "mysql", Host: "127.0.0.1", Port: port, Database: "app", ReadOnly: ptr(readOnly),
		Options: map[string]string{"ssl_mode": "required"}}))
	require.NoError(t, ws.SetCredentials(ctx, "shop", secrets.Credential{User: "root", Password: myPassword}, false))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	require.Empty(t, issues)
	require.NoError(t, sess.InstallExtension(ctx, "mysql"))
	sess.Preflight(ctx, "shop")
	t.Cleanup(func() { _ = sess.Close() })
	return my, ws, sess
}

// TestMySQLThroughTheAgent: remote reads pass the plan allow-list, every
// passthrough is refused (checked against the functions the extension really
// adds) and changes nothing on the server, and a write needs all three gates.
func TestMySQLThroughTheAgent(t *testing.T) {
	my, ws, sess := openMySQL(t, false)

	t.Run("reads are allowed in every tier, and connect on first use", func(t *testing.T) {
		for _, mode := range []agents.SandboxMode{agents.ModeReadOnly, agents.ModeReadWrite, agents.ModeFull} {
			h := agentOn(t, ws, sess, mode, true)
			resp := sqlCall(h, "SELECT * FROM shop.app.people ORDER BY id")
			require.Nil(t, resp.Error, errText(resp))
			assert.Len(t, rowsOf(t, resp), 2)
			for _, q := range []string{
				"SELECT count(*) FROM shop.app.people",
				"SELECT p.name, l.k FROM shop.app.people p JOIN (SELECT 1 AS id, 'x' AS k) l USING (id)",
				"SELECT name FROM shop.app.people WHERE id = 2",
				"DESCRIBE shop.app.people",
				"SHOW TABLES",
				"EXPLAIN SELECT * FROM shop.app.people",
				"WITH x AS (SELECT * FROM shop.app.people) SELECT count(*) FROM x",
			} {
				resp := sqlCall(h, q)
				require.Nil(t, resp.Error, q+": "+errText(resp))
			}
		}
		_, err := sess.DB().Exec("CREATE VIEW people_v AS SELECT * FROM shop.app.people")
		require.NoError(t, err)
		resp := sqlCall(agentOn(t, ws, sess, agents.ModeReadOnly, true), "SELECT count(*) FROM people_v")
		require.Nil(t, resp.Error, errText(resp))
		resp = sqlCall(agentOn(t, ws, sess, agents.ModeReadOnly, true), "SUMMARIZE shop.app.people")
		require.NotNil(t, resp.Error, "SUMMARIZE would copy the whole table out of the server")
	})

	t.Run("every function the mysql extension adds is refused, in every tier", func(t *testing.T) {
		rows, err := sess.DB().Query(`SELECT DISTINCT lower(function_name), function_type FROM duckdb_functions()
			WHERE function_name ILIKE '%mysql%' AND NOT internal OR function_name ILIKE 'mysql%'`)
		require.NoError(t, err)
		type fn struct{ name, typ string }
		var fns []fn
		for rows.Next() {
			var f fn
			require.NoError(t, rows.Scan(&f.name, &f.typ))
			fns = append(fns, f)
		}
		require.NoError(t, rows.Close())
		var names []string
		for _, f := range fns {
			names = append(names, f.name)
		}
		for _, want := range []string{"mysql_query", "mysql_execute", "mysql_clear_cache"} {
			require.Contains(t, names, want, "the extension adds %s (if this fails, a DuckDB bump changed the extension: re-check the denied list)", want)
		}
		for _, mode := range []agents.SandboxMode{agents.ModeReadOnly, agents.ModeReadWrite, agents.ModeFull} {
			h := agentOn(t, ws, sess, mode, false)
			for _, f := range fns {
				var qs []string
				switch f.typ {
				case "scalar":
					qs = []string{"SELECT " + f.name + "('shop')"}
				default:
					qs = []string{"SELECT * FROM " + f.name + "('shop', 'select 1')", "CALL " + f.name + "('shop')", "SELECT * FROM " + f.name + "()"}
				}
				for _, q := range qs {
					resp := sqlCall(h, q)
					require.NotNil(t, resp.Error, q)
					assert.Equal(t, agents.ErrPermission, resp.Error.Code, q+": "+resp.Error.Message)
					assert.NotContains(t, resp.Error.Message, myPassword)
				}
			}
			for _, q := range []string{
				"SELECT * FROM mysql_query('shop', 'DROP TABLE people')",
				"CALL mysql_execute('shop', 'DROP TABLE people')",
				"SELECT * FROM mysql_execute('shop', 'DROP TABLE people')",
				"ATTACH 'host=127.0.0.1 user=root' AS m2 (TYPE mysql)",
				"SELECT * FROM duckdb_secrets()",
			} {
				resp := sqlCall(h, q)
				require.NotNil(t, resp.Error, q)
				assert.Equal(t, agents.ErrPermission, resp.Error.Code, q)
			}
		}
		assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
		log := strings.ToLower(my.generalLog())
		assert.NotContains(t, log, "drop table people", "the passthrough payload never reached the server")
	})

	t.Run("the MySQL server's own schemas are off limits (mysql.user holds password hashes)", func(t *testing.T) {
		for _, mode := range []agents.SandboxMode{agents.ModeReadOnly, agents.ModeFull} {
			h := agentOn(t, ws, sess, mode, true)
			for _, q := range []string{
				"SELECT * FROM shop.mysql.user",
				"SELECT user, authentication_string FROM shop.mysql.user",
				`SELECT * FROM "shop"."MYSQL"."user"`,
				"SELECT * FROM shop.performance_schema.threads",
				"SELECT * FROM shop.sys.version",
				"SELECT * FROM shop.information_schema.tables",
				"SELECT * FROM shop.information_schema.columns",
				"WITH x AS (SELECT * FROM shop.mysql.user) SELECT count(*) FROM x",
				"SELECT (SELECT count(*) FROM shop.mysql.user)",
				"DESCRIBE shop.mysql.user",
			} {
				resp := sqlCall(h, q)
				require.NotNil(t, resp.Error, q)
				assert.Contains(t, resp.Error.Message, "MySQL server's own schemas", q)
			}
		}
		// DuckDB's own information_schema, and the connection's database, stay.
		h := agentOn(t, ws, sess, agents.ModeReadOnly, true)
		for _, q := range []string{"SELECT count(*) FROM information_schema.tables", "SELECT count(*) FROM shop.app.people", "SELECT count(*) FROM duckdb_tables() WHERE database_name = 'shop'"} {
			resp := sqlCall(h, q)
			require.Nil(t, resp.Error, q+": "+errText(resp))
		}
	})

	t.Run("a write over MCP (no approver) is refused with the documented message", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeFull, false)
		resp := sqlCall(h, "INSERT INTO shop.app.people VALUES (3, 'cy')")
		require.NotNil(t, resp.Error)
		assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "not a local file")
		assert.Contains(t, resp.Error.Message, "Nothing was changed")
		assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
		for _, q := range []string{"UPDATE shop.app.people SET name = 'z'", "DELETE FROM shop.app.people", "CREATE TABLE shop.app.made (a int)", "DROP TABLE shop.app.people"} {
			resp := sqlCall(h, q)
			require.NotNil(t, resp.Error, q)
			assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code, q)
		}
		assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
	})

	t.Run("with an approver, a write runs only when approved (three gates)", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeFull, false)
		ap := &stubApprover{decision: agents.ErrApprovalDeclined}
		h.SetApprover(ap.fn)
		resp := sqlCall(h, "INSERT INTO shop.app.people VALUES (3, 'cy')")
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "declined")
		assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
		require.Equal(t, 1, ap.count())
		assert.Equal(t, []string{"shop"}, ap.asked[0].Connections)

		ap.decision = nil
		resp = sqlCall(h, "INSERT INTO shop.app.people VALUES (3, 'cy')")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "3", my.mysql(t, "select count(*) from people"))
		resp = sqlCall(h, "UPDATE shop.app.people SET name = 'cyrus' WHERE id = 3")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "cyrus", my.mysql(t, "select name from people where id = 3"))
		resp = sqlCall(h, "DELETE FROM shop.app.people WHERE id = 3")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
		resp = sqlCall(h, "CREATE TABLE shop.app.made AS SELECT 1 AS a")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "1", my.mysql(t, "select count(*) from made"))
		resp = sqlCall(h, "DROP TABLE shop.app.made")
		require.Nil(t, resp.Error, errText(resp))

		// The read-write tier cannot run DDL; the read-only tier no DML.
		h2 := agentOn(t, ws, sess, agents.ModeReadWrite, false)
		h2.SetApprover(ap.fn)
		resp = sqlCall(h2, "CREATE TABLE shop.app.made2 (a int)")
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "full tier")
		h3 := agentOn(t, ws, sess, agents.ModeReadOnly, false)
		h3.SetApprover(ap.fn)
		resp = sqlCall(h3, "INSERT INTO shop.app.people VALUES (7, 'q')")
		require.NotNil(t, resp.Error)
		assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
	})

	t.Run("list_connections hides the host, the user and the password", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeReadOnly, true)
		resp := call(h, "list_connections", nil)
		require.Nil(t, resp.Error, errText(resp))
		b, _ := json.Marshal(resp.Result)
		out := string(b)
		assert.Contains(t, out, `"name":"shop"`)
		assert.Contains(t, out, `"location":"remote"`)
		assert.NotContains(t, out, "127.0.0.1")
		assert.NotContains(t, out, myPassword)
		assert.NotContains(t, strings.ToLower(out), "password")
	})

	t.Run("an error the agent sees never carries the user name or the password", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeReadOnly, true)
		resp := sqlCall(h, "SELECT * FROM shop.app.no_such_table")
		require.NotNil(t, resp.Error)
		assert.NotContains(t, resp.Error.Message, myPassword)
	})
}

// TestMySQLReadOnlyConnectionRefusesAgentWrites: read_only is checked before
// anything is asked, and a read-only attach is enforced by DuckDB itself.
func TestMySQLReadOnlyConnectionRefusesAgentWrites(t *testing.T) {
	my, ws, sess := openMySQL(t, true)
	ap := &stubApprover{}
	h := agentOn(t, ws, sess, agents.ModeFull, false)
	h.SetApprover(ap.fn)
	resp := sqlCall(h, "SELECT count(*) FROM shop.app.people")
	require.Nil(t, resp.Error, errText(resp))
	for _, q := range []string{
		"INSERT INTO shop.app.people VALUES (3, 'cy')",
		"UPDATE shop.app.people SET name = 'z'",
		"DELETE FROM shop.app.people",
		"CREATE TABLE shop.app.made (a int)",
		"DROP TABLE shop.app.people",
	} {
		resp := sqlCall(h, q)
		require.NotNil(t, resp.Error, q)
		assert.Contains(t, resp.Error.Message, "read-only", q)
	}
	assert.Equal(t, 0, ap.count(), "nothing is asked of the person for a write that cannot happen")
	assert.Equal(t, "2", my.mysql(t, "select count(*) from people"))
}
