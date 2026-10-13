package agents

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/skills"
)

func sqlOpenDuck(path string) (*sql.DB, error) { return sql.Open("duckdb", path) }

func errMsg(r *Response) string {
	if r == nil || r.Error == nil {
		return ""
	}
	return r.Error.Message
}

// TestGates_Tiers: the tier is the first write gate.
func TestGates_Tiers(t *testing.T) {
	cases := []struct {
		mode  SandboxMode
		sql   string
		allow bool
		msg   string
	}{
		{ModeReadOnly, "SELECT * FROM t", true, ""},
		{ModeReadOnly, "INSERT INTO t VALUES (2, 'x')", false, "read-only"},
		{ModeReadOnly, "CREATE TABLE zz (a INTEGER)", false, "read-only"},
		{ModeReadOnly, "COPY (SELECT 1) TO '%DATA%/o.csv'", false, "read-only"},
		{ModeReadWrite, "SELECT * FROM t", true, ""},
		{ModeReadWrite, "INSERT INTO t VALUES (2, 'x')", true, ""},
		{ModeReadWrite, "UPDATE t SET name = 'y'", true, ""},
		{ModeReadWrite, "DELETE FROM t", true, ""},
		{ModeReadWrite, "CREATE TABLE zz (a INTEGER)", false, "full tier"},
		{ModeReadWrite, "DROP TABLE t2", false, "full tier"},
		{ModeReadWrite, "COPY (SELECT 1) TO '%DATA%/o.csv'", false, "full tier"},
		{ModeFull, "INSERT INTO t VALUES (2, 'x')", true, ""},
		{ModeFull, "CREATE TABLE zz (a INTEGER)", true, ""},
		{ModeFull, "CREATE TABLE other.zz (a INTEGER)", true, ""},
		{ModeFull, "ALTER TABLE other.orders ADD COLUMN q INTEGER", true, ""},
		{ModeFull, "DROP TABLE t2", true, ""},
		{ModeFull, "COPY (SELECT 1 AS a) TO '%DATA%/o.csv' (HEADER)", true, ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode)+"/"+tc.sql, func(t *testing.T) {
			fx := newPolicyFixture(t, tc.mode) // ask-before-writes off, local connections
			resp := fx.run(fx.expand(tc.sql))
			if tc.allow {
				require.Nil(t, resp.Error, errMsg(resp))
			} else {
				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrPermission, resp.Error.Code)
				assert.Contains(t, resp.Error.Message, tc.msg)
			}
		})
	}
}

// TestGates_NeverListDeniedInEveryTier: the never-list stays denied in full,
// and no approver can lift it.
func TestGates_NeverListDeniedInEveryTier(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			fx := newPolicyFixture(t, mode, withApprover())
			fx.exec(`CREATE VIEW v_secrets AS SELECT * FROM duckdb_secrets()`)
			fx.exec(`CREATE VIEW v_text AS SELECT * FROM read_text('` + fx.outside + `/secret.txt')`)
			fx.exec(`CREATE MACRO m_file() AS (SELECT content FROM read_text('` + fx.outside + `/secret.txt'))`)
			fx.exec(`CREATE MACRO t_set() AS TABLE SELECT current_setting('home_directory') AS h`)
			home, _ := os.UserHomeDir()
			for _, q := range []string{
				// The persona-panel T1 repro: install httpfs, then read ~/.bashrc.
				"INSTALL httpfs",
				"LOAD httpfs",
				"SELECT * FROM read_text('" + filepath.Join(home, ".bashrc") + "')",
				"SELECT * FROM read_text('https://evil.example/.bashrc')",
				"SELECT * FROM v_secrets",
				"SELECT * FROM v_text",
				"SELECT m_file()",
				"SELECT * FROM t_set()",
				"INSERT INTO ro.ledger VALUES (2, 1)",
				"ATTACH 'x.db' AS x",
				"DETACH other",
				"USE other",
				"SET memory_limit='1GB'",
				"RESET memory_limit",
				"PRAGMA memory_limit='1GB'",
				"INSTALL httpfs",
				"LOAD httpfs",
				"EXPORT DATABASE '/tmp/x'",
				"CREATE SECRET s (TYPE s3)",
				"SELECT * FROM duckdb_secrets()",
				"SELECT * FROM read_text('/etc/hostname')",
				"SELECT * FROM read_text('~/.bashrc')",
				"SELECT * FROM read_csv('https://evil.example/x.csv')",
				"SELECT * FROM postgres_query('other', 'select 1')",
				"CALL postgres_execute('other', 'ROLLBACK; CREATE TABLE x(a int)')",
				"EXPLAIN ANALYZE INSERT INTO t VALUES (9, 'x')",
				"SET VARIABLE v = (SELECT content FROM read_text('/etc/hostname'))",
				"SELECT current_setting('home_directory')",
			} {
				resp := fx.run(q)
				require.NotNil(t, resp.Error, q)
				assert.Equal(t, ErrPermission, resp.Error.Code, q)
			}
			assert.Zero(t, fx.approver.count(), "a denied statement never asks for approval")
		})
	}
}

// TestGates_ReadOnlyConnection: the second gate. A connection with read_only
// set refuses an agent write in every tier, naming the connection.
func TestGates_ReadOnlyConnection(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadWrite, ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			fx := newPolicyFixture(t, mode, withApprover())
			for _, q := range []string{
				"INSERT INTO ro.ledger VALUES (2, 1)",
				"INSERT INTO ro.main.ledger VALUES (2, 1)",
				"UPDATE ro.ledger SET amount = 0",
				"DELETE FROM ro.main.ledger",
				"CREATE TABLE ro.zz (a INTEGER)",
				"DROP TABLE ro.ledger",
				"ALTER TABLE ro.ledger ADD COLUMN q INTEGER",
				"SELECT 1; DELETE FROM ro.ledger",
			} {
				resp := fx.run(q)
				require.NotNil(t, resp.Error, q)
				// A write to a read-only connection is refused by the tier
				// (DDL in read-write) or by the connection gate; either way
				// it is refused and nothing is asked.
				if !strings.Contains(q, "CREATE") && !strings.Contains(q, "DROP") && !strings.Contains(q, "ALTER") || mode == ModeFull {
					assert.Contains(t, resp.Error.Message, "read-only", q)
				}
			}
			assert.Zero(t, fx.approver.count())
			assert.Equal(t, 1, fx.count("SELECT count(*) FROM ro.ledger"))
			assert.Equal(t, 100, fx.count("SELECT amount FROM ro.ledger"))
		})
	}
}

// TestGates_ApprovalForcedForNonLocal: the third gate. Anything that is not a
// local file needs the person's OK even when "Ask before changes" is off.
func TestGates_ApprovalForcedForNonLocal(t *testing.T) {
	t.Run("local write with the gate off needs no approval", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeReadWrite, withAsk(false), withApprover())
		resp := fx.run("INSERT INTO other.orders VALUES (3, 30)")
		require.Nil(t, resp.Error, errMsg(resp))
		assert.Zero(t, fx.approver.count())
		assert.Equal(t, 3, fx.count("SELECT count(*) FROM other.orders"))
	})

	t.Run("local write with the gate on asks", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeReadWrite, withAsk(true), withApprover())
		resp := fx.run("INSERT INTO other.orders VALUES (3, 30)")
		require.Nil(t, resp.Error, errMsg(resp))
		assert.Equal(t, 1, fx.approver.count())
	})

	t.Run("a remote write asks even with the gate off, and executes only when approved", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeReadWrite, withAsk(false), withApprover(), withRemote("other"))
		resp := fx.run("INSERT INTO other.orders VALUES (3, 30)")
		require.Nil(t, resp.Error, errMsg(resp))
		require.Equal(t, 1, fx.approver.count())
		ask := fx.approver.asked[0]
		assert.Equal(t, []string{"other"}, ask.Connections)
		assert.Contains(t, ask.Summary, "not a local file")
		assert.Equal(t, 3, fx.count("SELECT count(*) FROM other.orders"))
	})

	t.Run("a declined remote write changes nothing", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withApprover(), withRemote("other"))
		fx.approver.decision = ErrApprovalDeclined
		for _, q := range []string{
			"INSERT INTO other.orders VALUES (3, 30)",
			"UPDATE other.orders SET total = 0",
			"DELETE FROM other.orders",
			"DROP TABLE other.orders",
			"CREATE TABLE other.zz (a INTEGER)",
		} {
			resp := fx.run(q)
			require.NotNil(t, resp.Error, q)
			assert.Equal(t, ErrApprovalNeeded, resp.Error.Code, q)
			assert.Contains(t, resp.Error.Message, "declined", q)
		}
		assert.Equal(t, 5, fx.approver.count())
		assert.Equal(t, 2, fx.count("SELECT count(*) FROM other.orders"))
		assert.Equal(t, 20, fx.count("SELECT max(total) FROM other.orders"))
	})

	t.Run("headless (no approver): a remote write is refused with the documented message, even with the gate off", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withRemote("other"))
		resp := fx.run("INSERT INTO other.orders VALUES (3, 30)")
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "not a local file")
		assert.Contains(t, resp.Error.Message, "always needs your OK in Vibe Studio")
		assert.Contains(t, resp.Error.Message, "Nothing was changed")
		assert.Equal(t, 2, fx.count("SELECT count(*) FROM other.orders"))
		// The same call to a local catalog still runs headless with the gate off.
		resp = fx.run("INSERT INTO t VALUES (2, 'x')")
		require.Nil(t, resp.Error, errMsg(resp))
	})

	t.Run("headless with the gate on: the message tells how to loosen only local writes", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(true))
		resp := fx.run("INSERT INTO t VALUES (2, 'x')")
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "Ask before changes")
	})

	t.Run("a script touching a remote catalog asks once and is refused headless", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withRemote("other"))
		resp := fx.run("INSERT INTO t VALUES (2, 'x'); INSERT INTO other.orders VALUES (3, 30)")
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
		assert.Equal(t, 1, fx.count("SELECT count(*) FROM t"), "no statement of the script ran")
	})

	t.Run("a read of a remote catalog never asks", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeReadOnly, withAsk(true), withApprover(), withRemote("other"))
		resp := fx.run("SELECT * FROM other.orders")
		require.Nil(t, resp.Error, errMsg(resp))
		assert.Zero(t, fx.approver.count())
	})

	t.Run("an approver error that is not a decision is surfaced and nothing runs", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withApprover(), withRemote("other"))
		fx.approver.decision = errors.New("approver unavailable")
		resp := fx.run("DELETE FROM other.orders")
		require.NotNil(t, resp.Error)
		assert.Equal(t, 2, fx.count("SELECT count(*) FROM other.orders"))
	})
}

// TestGates_RecordAndSchemaToolsFollowTheSameGates: create_record, bulk_insert,
// create_table and friends write through the same three gates.
func TestGates_RecordAndSchemaToolsFollowTheSameGates(t *testing.T) {
	t.Run("read-only connection", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withApprover())
		for method, params := range map[string]map[string]any{
			"create_record": {"table": "ro.ledger", "values": map[string]any{"id": 2, "amount": 5}},
			"update_record": {"table": "ro.ledger", "values": map[string]any{"amount": 5}, "where": "id = 1"},
			"delete_record": {"table": "ro.ledger", "where": "id = 1"},
			"bulk_insert":   {"table": "ro.ledger", "columns": []any{"id", "amount"}, "rows": []any{[]any{2, 5}}},
			"create_table":  {"name": "ro.zz", "columns": []any{map[string]any{"name": "a", "type": "INTEGER"}}},
			"drop_table":    {"name": "ro.ledger"},
			"add_column":    {"table": "ro.ledger", "name": "q", "type": "INTEGER"},
		} {
			resp := fx.call(method, params)
			require.NotNil(t, resp.Error, method)
			assert.Contains(t, resp.Error.Message, "read-only", method)
		}
		assert.Zero(t, fx.approver.count())
		assert.Equal(t, 1, fx.count("SELECT count(*) FROM ro.ledger"))
	})

	t.Run("non-local connection is always approved", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withApprover(), withRemote("other"))
		resp := fx.call("create_record", map[string]any{"table": "other.orders", "values": map[string]any{"id": 9, "total": 90}})
		require.Nil(t, resp.Error, errMsg(resp))
		assert.Equal(t, 1, fx.approver.count())
		assert.Equal(t, []string{"other"}, fx.approver.asked[0].Connections)
		assert.Equal(t, 3, fx.count("SELECT count(*) FROM other.orders"))

		fx.approver.decision = ErrApprovalDeclined
		resp = fx.call("drop_table", map[string]any{"name": "other.orders"})
		require.NotNil(t, resp.Error)
		assert.Equal(t, 3, fx.count("SELECT count(*) FROM other.orders"))
	})

	t.Run("non-local connection headless is refused", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withRemote("other"))
		resp := fx.call("create_record", map[string]any{"table": "other.orders", "values": map[string]any{"id": 9, "total": 90}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
		assert.Equal(t, 2, fx.count("SELECT count(*) FROM other.orders"))
	})

	t.Run("local connection with the gate off runs", func(t *testing.T) {
		fx := newPolicyFixture(t, ModeFull, withAsk(false), withApprover())
		resp := fx.call("create_record", map[string]any{"table": "other.orders", "values": map[string]any{"id": 9, "total": 90}})
		require.Nil(t, resp.Error, errMsg(resp))
		assert.Zero(t, fx.approver.count())
		resp = fx.call("create_table", map[string]any{"name": "other.fresh", "columns": []any{map[string]any{"name": "a", "type": "INTEGER"}}})
		require.Nil(t, resp.Error, errMsg(resp))
		assert.Equal(t, 0, fx.count("SELECT count(*) FROM other.fresh"))
	})
}

// TestGates_ChangedWhileWaiting: the policy is re-checked after the person
// approves.
func TestGates_ChangedWhileWaiting(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull, withAsk(true), withApprover())
	fx.approver.decision = nil
	// The approver tightens the tier while the card is open.
	fx.h.SetApprover(func(ctx context.Context, req ApprovalRequest) error {
		fx.h.SetFilePolicy(ModeReadOnly, true)
		return nil
	})
	resp := fx.run("INSERT INTO t VALUES (2, 'x')")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "access changed")
	assert.Equal(t, 1, fx.count("SELECT count(*) FROM t"))
}

// TestGates_CopyToWritesOnlyInsideData runs COPY ... TO for real.
func TestGates_CopyToWritesOnlyInsideData(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	resp := fx.run(fx.expand("COPY (SELECT 1 AS a, 'x' AS b) TO '%DATA%/out.csv' (HEADER)"))
	require.Nil(t, resp.Error, errMsg(resp))
	b, err := os.ReadFile(filepath.Join(fx.dataDir, "out.csv"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "a,b")

	for _, q := range []string{
		"COPY (SELECT 1 AS a) TO '%OUT%/leak.csv'",
		"COPY (SELECT 1 AS a) TO '%DATA%/../leak.csv'",
		"COPY (SELECT 1 AS a) TO '/tmp/vibe-leak-probe.csv'",
		"COPY (SELECT 1 AS a) TO 'rel.csv'",
		"COPY (SELECT 1 AS a) TO '%DATA%/link.csv'",
	} {
		resp := fx.run(fx.expand(q))
		require.NotNil(t, resp.Error, q)
	}
	_, err = os.Stat(filepath.Join(fx.outside, "leak.csv"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat("/tmp/vibe-leak-probe.csv")
	assert.True(t, os.IsNotExist(err))
	b, _ = os.ReadFile(filepath.Join(fx.outside, "secret.csv"))
	assert.Equal(t, "a,b\n99,99\n", string(b), "the symlink target was not overwritten")

	// A data file can be loaded back by a literal path.
	resp = fx.run(fx.expand("CREATE TABLE loaded AS SELECT * FROM read_csv('%DATA%/out.csv')"))
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, 1, fx.count("SELECT count(*) FROM loaded"))
}

// TestGates_FileReadsThroughRunQuery reads data/ files for real and refuses the rest.
func TestGates_FileReadsThroughRunQuery(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	resp := fx.run(fx.expand("SELECT sum(a) AS s FROM read_csv('%DATA%/x.csv')"))
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	rows, ok := res["rows"].([][]any)
	require.True(t, ok)
	assert.EqualValues(t, 4, rows[0][0])

	resp = fx.run(fx.expand("SELECT count(*) FROM '%DATA%/x.csv'"))
	require.Nil(t, resp.Error, errMsg(resp))

	for _, q := range []string{
		"SELECT * FROM read_text('%OUT%/secret.txt')",
		"SELECT * FROM read_csv('%DATA%/link.csv')",
		"SELECT * FROM '%OUT%/secret.csv'",
	} {
		resp = fx.run(fx.expand(q))
		require.NotNil(t, resp.Error, q)
		assert.NotContains(t, resp.Error.Message, "TOPSECRET")
		assert.NotContains(t, resp.Error.Message, "99,99")
	}
}

func TestListConnections(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly, withRemote("other"), withExtraConns(
		session.ConnectionInfo{Name: "lake", Type: "s3", Kind: session.StatusOK, Remote: true, Catalogless: true, Scope: "s3://lake/raw/"},
	))
	resp := fx.call("list_connections", map[string]any{})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, fx.dataDir, res["data_dir"])
	byName := map[string]map[string]any{}
	conns, ok := res["connections"].([]map[string]any)
	require.True(t, ok)
	for _, c := range conns {
		name, _ := c["name"].(string)
		byName[name] = c
	}
	require.Contains(t, byName, "other")
	assert.Equal(t, "remote", byName["other"]["location"])
	assert.Equal(t, false, byName["other"]["read_only"])
	assert.Equal(t, true, byName["other"]["catalog"])
	require.Contains(t, byName, "ro")
	assert.Equal(t, true, byName["ro"]["read_only"])
	assert.Equal(t, "local", byName["ro"]["location"])
	require.Contains(t, byName, "lake")
	assert.Equal(t, "s3://lake/raw/", byName["lake"]["scope"])
	assert.Equal(t, false, byName["lake"]["catalog"])

	// No path outside the workspace, no DSN, no credential anywhere in the output.
	raw := strings.ToLower(strings.Join(flatten(res), " "))
	assert.NotContains(t, raw, "other.duckdb")
	assert.NotContains(t, raw, "ro.duckdb")
	assert.NotContains(t, raw, "password")

	// Visible in every tier, including read-only (it is an introspect tool).
	assert.Nil(t, fx.call("list_connections", nil).Error)
}

func flatten(v any) []string {
	switch x := v.(type) {
	case map[string]any:
		var out []string
		for k, vv := range x {
			out = append(out, k)
			out = append(out, flatten(vv)...)
		}
		return out
	case []map[string]any:
		var out []string
		for _, vv := range x {
			out = append(out, flatten(vv)...)
		}
		return out
	case string:
		return []string{x}
	default:
		return nil
	}
}

func TestIntrospectionIsCatalogAware(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	resp := fx.call("introspect_schema", map[string]any{"connection": "other"})
	require.Nil(t, resp.Error, errMsg(resp))
	tables, _ := mapOf(t, resp.Result)["tables"].([]string)
	assert.Contains(t, tables, "other.main.orders")
	resp = fx.call("describe_table", map[string]any{"table": "other.main.orders"})
	require.Nil(t, resp.Error, errMsg(resp))
	cols, _ := mapOf(t, resp.Result)["columns"].([]map[string]any)
	assert.Len(t, cols, 2)
}

func TestAgentAuthoredCellsMayNameAConnection(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull, withExtraConns(
		session.ConnectionInfo{Name: "lake", Type: "s3", Kind: session.StatusOK, Remote: true, Catalogless: true, Scope: "s3://lake/raw/"},
	))
	resp := fx.call("add_query_cell", map[string]any{"sql": "SELECT * FROM orders", "connection": "other", "label": "Orders"})
	require.Nil(t, resp.Error, errMsg(resp))
	id, _ := mapOf(t, resp.Result)["notebook_id"].(string)
	nb, _, err := fx.h.access.Notebooks().Get(context.Background(), id)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, "other", nb.Cells[0].Connection)

	// The default connection resolves when no connection is named.
	resp = fx.call("add_query_cell", map[string]any{"sql": "SELECT * FROM t", "notebook_id": id})
	require.Nil(t, resp.Error, errMsg(resp))

	// Unknown, storage-only and unqualified-name-on-the-wrong-connection are refused.
	resp = fx.call("add_query_cell", map[string]any{"sql": "SELECT 1", "connection": "nope"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "no connection named")
	resp = fx.call("add_query_cell", map[string]any{"sql": "SELECT 1", "connection": "lake"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "storage connection")
	resp = fx.call("add_query_cell", map[string]any{"sql": "SELECT * FROM orders", "connection": "ro"})
	require.NotNil(t, resp.Error, "orders does not exist on ro")
	resp = fx.call("add_query_cell", map[string]any{"sql": "INSERT INTO t VALUES (1, 'x')", "connection": "other"})
	require.NotNil(t, resp.Error)
}

func TestTaskModeCellsCarryTheirConnection(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	fx.h.SetMode(ModeTask)
	prev := fx.call("preview_query", map[string]any{"sql": "SELECT * FROM orders", "connection": "other"})
	require.Nil(t, prev.Error, errMsg(prev))
	// write_cell must name the connection the query was previewed on.
	wrong := fx.call("write_cell", map[string]any{"cell_type": "sql", "sql": "SELECT * FROM orders"})
	require.NotNil(t, wrong.Error)
	ok := fx.call("write_cell", map[string]any{"cell_type": "sql", "sql": "SELECT * FROM orders", "connection": "other"})
	require.Nil(t, ok.Error, errMsg(ok))
	cell := mapOf(t, mapOf(t, ok.Result)["cell"])
	assert.Equal(t, "other", cell["connection"])
	bad := fx.call("preview_query", map[string]any{"sql": "SELECT 1", "connection": "nope"})
	require.NotNil(t, bad.Error)
}

// TestSessionConnectionInfosHaveNoSecrets guards list_connections' source.
func TestSessionConnectionInfosHaveNoSecrets(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	for _, ci := range fx.sess.ConnectionInfos() {
		assert.NotContains(t, strings.ToLower(ci.Name+ci.Type+ci.Scope), "password")
	}
}

// TestSkills_GateOnQualifiedNamesWithoutDialling: a table-bound skill names
// catalog.schema.table, is probed only against catalogs that are already
// attached, and never connects a registered one (opening a workspace and
// listing skills must not dial remotes).
func TestSkills_GateOnQualifiedNamesWithoutDialling(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	lazyPath := filepath.Join(fx.dir, "lazy.duckdb")
	f, err := sqlOpenDuck(lazyPath)
	require.NoError(t, err)
	_, err = f.Exec("CREATE TABLE tracked (a INTEGER); INSERT INTO tracked VALUES (1)")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, fx.sess.Register(session.Spec{Name: "lazy", Type: "duckdb", Path: lazyPath}))

	st := fx.h.access.Skills()
	insertSkill(t, st, "orders-skill", "d", "i", "other.main.orders", "user")
	insertSkill(t, st, "lazy-skill", "d", "i", "lazy.main.tracked", "user")
	insertSkill(t, st, "missing-skill", "d", "i", "nope.main.x", "user")

	entries, err := skills.LoadSkills(st, fx.sess, nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	assert.True(t, names["orders-skill"], "an attached catalog's table gates in")
	assert.False(t, names["lazy-skill"], "a registered but not connected catalog is not probed")
	assert.False(t, names["missing-skill"])
	for _, c := range fx.sess.Connections() {
		if c.Name == "lazy" {
			assert.Equal(t, session.StatusNotConnected, c.Kind, "listing skills never dials a connection")
		}
	}
}

// The reviewer's #116 blockers: PIVOT/UNPIVOT name a table source the token
// screen did not know. Kept as permanent regressions.
func TestUnpivotFileReadIsRefused(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull, withAsk(false))
	sec := filepath.Join(fx.outside, "secret.csv")
	for _, q := range []string{
		`CREATE TABLE stolen AS UNPIVOT '` + sec + `' ON COLUMNS(*) INTO NAME k VALUE v`,
		`CREATE VIEW vstolen AS UNPIVOT 'http://127.0.0.1:1/x.csv' ON a INTO NAME k VALUE v`,
		`CREATE MACRO mstolen() AS TABLE UNPIVOT 'http://127.0.0.1:1/x.csv' ON a INTO NAME k VALUE v`,
	} {
		assert.Equal(t, sqlClassOther, fx.classify(q).Class, q)
		resp := fx.run(q)
		require.NotNil(t, resp.Error, q)
	}
	assert.Equal(t, 0, fx.count("SELECT count(*) FROM duckdb_tables() WHERE table_name = 'stolen'"))
}

// #111: introspect_schema and describe_table connect a registered connection
// on first use, as run_query does, instead of calling it unknown.
func TestIntrospectionConnectsOnFirstUse(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		params       map[string]any
	}{
		{"introspect_schema", "introspect_schema", map[string]any{"connection": "lazy"}},
		{"describe_table", "describe_table", map[string]any{"table": "lazy.main.tracked"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newPolicyFixture(t, ModeReadOnly)
			lazyPath := filepath.Join(fx.dir, "lazy.duckdb")
			f, err := sqlOpenDuck(lazyPath)
			require.NoError(t, err)
			_, err = f.Exec("CREATE TABLE tracked (a INTEGER, b VARCHAR)")
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.NoError(t, fx.sess.Register(session.Spec{Name: "lazy", Type: "duckdb", Path: lazyPath}))

			resp := fx.call(tc.method, tc.params)
			require.Nil(t, resp.Error, errMsg(resp))
			if tc.method == "introspect_schema" {
				tables, _ := mapOf(t, resp.Result)["tables"].([]string)
				assert.Equal(t, []string{"lazy.main.tracked"}, tables)
			} else {
				cols, _ := mapOf(t, resp.Result)["columns"].([]map[string]any)
				assert.Len(t, cols, 2)
			}
		})
	}
}

// #111: a connection that fails to connect reports its own error, not
// "unknown connection" or an empty column list.
func TestIntrospectionReportsConnectFailure(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	missing := filepath.Join(fx.dir, "nope", "missing.duckdb")
	require.NoError(t, fx.sess.Register(session.Spec{Name: "gone", Type: "duckdb", Path: missing, ReadOnly: true}))
	resp := fx.call("describe_table", map[string]any{"table": "gone.main.t"})
	require.NotNil(t, resp.Error, "a failed connect is an error, not an empty table")
	assert.NotContains(t, resp.Error.Message, "unknown connection")
}
