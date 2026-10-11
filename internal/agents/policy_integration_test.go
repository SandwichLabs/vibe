//go:build integration

package agents_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func memWS(t *testing.T) workspace.Workspace {
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
	return ws
}

func ptr[T any](v T) *T { return &v }

type stubApprover struct {
	mu       sync.Mutex
	decision error
	asked    []agents.ApprovalRequest
}

func (a *stubApprover) fn(_ context.Context, req agents.ApprovalRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, req)
	return a.decision
}

func (a *stubApprover) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.asked)
}

// agentOn builds the agent surface exactly as Studio and `vibe mcp serve` do:
// a workspace-backed handler over its session, at a tier, with the person's
// "Ask before changes" setting.
func agentOn(t *testing.T, ws workspace.Workspace, sess session.Session, mode agents.SandboxMode, ask bool) *agents.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := agents.NewHandler(workspace.NewAgentAccess(ws, sess), engine.NewManager(sess, engine.WithLogger(logger)),
		&agents.SandboxConfig{Mode: mode, MaxQueryRows: 1000, RequireApproval: ask}, logger)
	return h
}

func call(h *agents.Handler, method string, params map[string]any) *agents.Response {
	return h.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: "1", Method: method, Params: params})
}

func sqlCall(h *agents.Handler, q string) *agents.Response {
	return call(h, "run_query", map[string]any{"sql": q})
}

func openPG(t *testing.T, readOnly bool) (*pgServer, workspace.Workspace, session.Session) {
	t.Helper()
	pg := startPG(t)
	pg.psql(t, "create table people(id int primary key, name text); insert into people values (1,'ada'),(2,'bob')")
	port, _ := strconv.Atoi(pg.port)
	ws := memWS(t)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "pg", Type: "postgres", Host: "127.0.0.1", Port: port, Database: "postgres", ReadOnly: ptr(readOnly),
		Options: map[string]string{"sslmode": "disable"}}))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "postgres", Password: pgPassword}, false))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	require.Empty(t, issues)
	t.Cleanup(func() { _ = sess.Close() })
	return pg, ws, sess
}

func errText(r *agents.Response) string {
	if r.Error == nil {
		return ""
	}
	return r.Error.Message
}

// TestPostgresThroughTheAgent: remote reads pass the plan allow-list, every
// passthrough is refused and changes nothing on the server, and a write needs
// all three gates.
func TestPostgresThroughTheAgent(t *testing.T) {
	pg, ws, sess := openPG(t, false)

	t.Run("remote reads are allowed even in read-only, and connect on first use", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeReadOnly, true)
		resp := sqlCall(h, "SELECT * FROM pg.public.people ORDER BY id")
		require.Nil(t, resp.Error, errText(resp))
		rows := rowsOf(t, resp)
		assert.Len(t, rows, 2)

		for _, q := range []string{
			"SELECT count(*) FROM pg.public.people",
			"SELECT p.name, l.k FROM pg.public.people p JOIN (SELECT 1 AS id, 'x' AS k) l USING (id)",
			"DESCRIBE pg.public.people",
			"SHOW TABLES",
			"EXPLAIN SELECT * FROM pg.public.people",
			"WITH x AS (SELECT * FROM pg.public.people) SELECT count(*) FROM x",
		} {
			resp := sqlCall(h, q)
			require.Nil(t, resp.Error, q+": "+errText(resp))
		}
		// A view over a remote table.
		_, err := sess.DB().Exec("CREATE VIEW people_v AS SELECT * FROM pg.public.people")
		require.NoError(t, err)
		resp = sqlCall(h, "SELECT count(*) FROM people_v")
		require.Nil(t, resp.Error, errText(resp))

		// SUMMARIZE would copy the whole table out of the server.
		resp = sqlCall(h, "SUMMARIZE pg.public.people")
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "SUMMARIZE")
	})

	t.Run("the passthroughs are refused and change nothing on the server", func(t *testing.T) {
		for _, mode := range []agents.SandboxMode{agents.ModeReadOnly, agents.ModeReadWrite, agents.ModeFull} {
			h := agentOn(t, ws, sess, mode, false)
			for _, q := range []string{
				"SELECT * FROM postgres_query('pg', 'select 1')",
				"CALL postgres_execute('pg', 'ROLLBACK; CREATE TABLE evil(a int)')",
				"SELECT * FROM postgres_execute('pg', 'COMMIT; CREATE TABLE evil(a int)')",
				"SELECT * FROM postgres_scan('host=127.0.0.1 password=x', 'public', 'people')",
				"ATTACH 'host=127.0.0.1' AS pg2 (TYPE postgres)",
				"SELECT * FROM duckdb_secrets()",
				"SELECT * FROM which_secret('x', 'postgres')",
			} {
				resp := sqlCall(h, q)
				require.NotNil(t, resp.Error, q)
				assert.Equal(t, agents.ErrPermission, resp.Error.Code, q)
				assert.NotContains(t, resp.Error.Message, pgPassword)
			}
		}
		assert.Equal(t, "0", pg.psql(t, "select count(*) from pg_tables where tablename = 'evil'"))
	})

	t.Run("a write over MCP (no approver) is refused with the documented message", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeFull, false) // the person turned "Ask before changes" off
		resp := sqlCall(h, "INSERT INTO pg.public.people VALUES (3, 'cy')")
		require.NotNil(t, resp.Error)
		assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "not a local file")
		assert.Contains(t, resp.Error.Message, "Nothing was changed")
		assert.Equal(t, "2", pg.psql(t, "select count(*) from people"))
		for _, q := range []string{"UPDATE pg.public.people SET name = 'z'", "DELETE FROM pg.public.people", "CREATE TABLE pg.public.made (a int)", "DROP TABLE pg.public.people"} {
			resp := sqlCall(h, q)
			require.NotNil(t, resp.Error, q)
			assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code, q)
		}
		assert.Equal(t, "2", pg.psql(t, "select count(*) from people"))
		resp = call(h, "create_record", map[string]any{"table": "pg.public.people", "values": map[string]any{"id": 9, "name": "q"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, agents.ErrApprovalNeeded, resp.Error.Code)
	})

	t.Run("with an approver, a write runs only when approved", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeFull, false)
		ap := &stubApprover{decision: agents.ErrApprovalDeclined}
		h.SetApprover(ap.fn)
		resp := sqlCall(h, "INSERT INTO pg.public.people VALUES (3, 'cy')")
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "declined")
		assert.Equal(t, "2", pg.psql(t, "select count(*) from people"))
		require.Equal(t, 1, ap.count())
		assert.Equal(t, []string{"pg"}, ap.asked[0].Connections)

		ap.decision = nil
		resp = sqlCall(h, "INSERT INTO pg.public.people VALUES (3, 'cy')")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "3", pg.psql(t, "select count(*) from people"))

		resp = sqlCall(h, "UPDATE pg.public.people SET name = 'cyrus' WHERE id = 3")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "cyrus", pg.psql(t, "select name from people where id = 3"))
		resp = sqlCall(h, "DELETE FROM pg.public.people WHERE id = 3")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "2", pg.psql(t, "select count(*) from people"))

		// The full tier may run DDL on a writable remote catalog (approved).
		resp = sqlCall(h, "CREATE TABLE pg.public.made AS SELECT 1 AS a")
		require.Nil(t, resp.Error, errText(resp))
		assert.Equal(t, "1", pg.psql(t, "select count(*) from made"))
		resp = sqlCall(h, "DROP TABLE pg.public.made")
		require.Nil(t, resp.Error, errText(resp))

		// The read-write tier cannot.
		h2 := agentOn(t, ws, sess, agents.ModeReadWrite, false)
		h2.SetApprover(ap.fn)
		resp = sqlCall(h2, "CREATE TABLE pg.public.made2 (a int)")
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "full tier")
		// Record tools follow the same gates.
		resp = call(h2, "create_record", map[string]any{"table": "pg.public.people", "values": map[string]any{"id": 5, "name": "e"}})
		require.Nil(t, resp.Error, errText(resp))
		resp = call(h2, "delete_record", map[string]any{"table": "pg.public.people", "where": "id = 5"})
		require.Nil(t, resp.Error, errText(resp))
	})

	t.Run("list_connections hides the DSN, the host and the password", func(t *testing.T) {
		h := agentOn(t, ws, sess, agents.ModeReadOnly, true)
		resp := call(h, "list_connections", nil)
		require.Nil(t, resp.Error, errText(resp))
		b, _ := json.Marshal(resp.Result)
		out := string(b)
		assert.Contains(t, out, `"name":"pg"`)
		assert.Contains(t, out, `"location":"remote"`)
		assert.Contains(t, out, `"read_only":false`)
		assert.NotContains(t, out, "127.0.0.1")
		assert.NotContains(t, out, pgPassword)
		assert.NotContains(t, strings.ToLower(out), "password")
		assert.Contains(t, out, filepath.Join(ws.Root(), "data"))
	})

	assert.False(t, strings.Contains(strings.ToLower(pg.logs()), "create table evil"), "the passthrough payload never reached the server")
}

// TestPostgresReadOnlyConnectionRefusesAgentWrites: read_only is checked before
// anything is asked.
func TestPostgresReadOnlyConnectionRefusesAgentWrites(t *testing.T) {
	pg, ws, sess := openPG(t, true)
	ap := &stubApprover{}
	h := agentOn(t, ws, sess, agents.ModeFull, false)
	h.SetApprover(ap.fn)
	resp := sqlCall(h, "SELECT count(*) FROM pg.public.people")
	require.Nil(t, resp.Error, errText(resp))
	for _, q := range []string{
		"INSERT INTO pg.public.people VALUES (3, 'cy')",
		"UPDATE pg.public.people SET name = 'z'",
		"DELETE FROM pg.public.people",
		"CREATE TABLE pg.public.made (a int)",
		"DROP TABLE pg.public.people",
	} {
		resp := sqlCall(h, q)
		require.NotNil(t, resp.Error, q)
		assert.Contains(t, resp.Error.Message, "read-only", q)
	}
	assert.Zero(t, ap.count(), "nothing is asked about a connection that cannot be written")
	assert.Equal(t, "2", pg.psql(t, "select count(*) from people"))
}

const minioSecret = "minioSecret_agent1"

func startMinIO(t *testing.T) string {
	t.Helper()
	bin := containerBin()
	out, err := exec.Command(bin, "run", "--rm", "-d", "--entrypoint", "sh",
		"-e", "MINIO_ROOT_USER=minioadmin", "-e", "MINIO_ROOT_PASSWORD="+minioSecret,
		"-p", "127.0.0.1::9000", "docker.io/minio/minio",
		"-c", "mkdir -p /data/lake && exec minio server /data").CombinedOutput()
	if err != nil {
		t.Skipf("minio is not available here: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	if i := strings.LastIndex(id, "\n"); i >= 0 {
		id = id[i+1:]
	}
	t.Cleanup(func() { _ = exec.Command(bin, "rm", "-f", id).Run() })
	pout, err := exec.Command(bin, "port", id, "9000/tcp").CombinedOutput()
	require.NoError(t, err, string(pout))
	line := strings.TrimSpace(strings.Split(string(pout), "\n")[0])
	ep := "127.0.0.1:" + line[strings.LastIndex(line, ":")+1:]
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("curl", "-s", "-o", "/dev/null", "-m", "2", "http://"+ep+"/minio/health/ready").Run() == nil {
			return ep
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("minio never became ready")
	return ""
}

// TestStorageScopeThroughTheAgent: URL reads work inside a storage
// connection's scope and nowhere else, even though the keys can read the whole bucket.
func TestStorageScopeThroughTheAgent(t *testing.T) {
	ep := startMinIO(t)
	ctx := context.Background()
	ws := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "lake", Type: "s3", Scope: "s3://lake/raw/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}))
	require.NoError(t, ws.SetCredentials(ctx, "lake", secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}, false))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	require.Empty(t, issues)
	defer sess.Close() //nolint:errcheck

	// Seed objects as the person (the engine is the human path, not the agent's).
	// A second, bucket-wide secret stays in place so the keys CAN read
	// everything: it is the SQL policy, not the credentials, that confines the agent.
	require.NoError(t, sess.CreateTemporarySecret(ctx, "seed_all", "s3", []session.SecretParam{
		{Key: "SCOPE", Value: "s3://lake/"}, {Key: "KEY_ID", Value: "minioadmin"},
		{Key: "SECRET", Value: minioSecret, Secret: true}, {Key: "REGION", Value: "us-east-1"},
		{Key: "ENDPOINT", Value: ep}, {Key: "URL_STYLE", Value: "path"}, {Key: "USE_SSL", Value: false},
	}))
	eng := engine.NewManager(sess)
	_, err = eng.Execute(ctx, "COPY (SELECT 1 AS x UNION ALL SELECT 2) TO 's3://lake/raw/a.parquet'", nil)
	require.NoError(t, err)
	_, err = eng.Execute(ctx, "COPY (SELECT 'secret' AS x) TO 's3://lake/private/p.parquet'", nil)
	require.NoError(t, err)

	h := agentOn(t, ws, sess, agents.ModeFull, true)
	ap := &stubApprover{}
	h.SetApprover(ap.fn)

	resp := sqlCall(h, "SELECT count(*) FROM read_parquet('s3://lake/raw/a.parquet')")
	require.Nil(t, resp.Error, errText(resp))
	assert.EqualValues(t, 2, rowsOf(t, resp)[0][0])
	resp = sqlCall(h, "SELECT count(*) FROM 's3://lake/raw/a.parquet'")
	require.Nil(t, resp.Error, errText(resp))
	resp = sqlCall(h, "SELECT count(*) FROM glob('s3://lake/raw/*')")
	require.Nil(t, resp.Error, errText(resp))
	resp = sqlCall(h, "DESCRIBE SELECT * FROM read_parquet('s3://lake/raw/*.parquet')")
	require.Nil(t, resp.Error, errText(resp))

	for _, q := range []string{
		"SELECT * FROM read_parquet('s3://lake/private/p.parquet')",
		"SELECT * FROM read_parquet('s3://lake/raw/../private/p.parquet')",
		"SELECT * FROM read_parquet('s3://lake/rawx/a.parquet')",
		"SELECT * FROM read_parquet('s3://other-bucket/raw/a.parquet')",
		"SELECT * FROM read_text('http://" + ep + "/lake/private/p.parquet')",
		"SELECT * FROM read_parquet('s3://lake/' || 'private/p.parquet')",
		"COPY (SELECT 1 AS x) TO 's3://lake/raw/evil.parquet'",
		"CREATE TABLE copied AS SELECT * FROM read_parquet('s3://lake/private/p.parquet')",
	} {
		resp := sqlCall(h, q)
		require.NotNil(t, resp.Error, q)
		assert.NotContains(t, resp.Error.Message, "secret", q)
	}
	assert.Zero(t, ap.count())

	// The connection shows up with its scope and no key.
	resp = call(h, "list_connections", nil)
	b, _ := json.Marshal(resp.Result)
	assert.Contains(t, string(b), `"scope":"s3://lake/raw/"`)
	assert.NotContains(t, string(b), minioSecret)
	assert.NotContains(t, string(b), "minioadmin")

	// Once its credentials are cleared the connection no longer widens what is readable.
	require.NoError(t, ws.ClearCredentials(ctx, "lake"))
	sess.Preflight(ctx, "lake")
	resp = sqlCall(h, "SELECT count(*) FROM read_parquet('s3://lake/raw/a.parquet')")
	require.NotNil(t, resp.Error)
}

func rowsOf(t *testing.T, r *agents.Response) [][]any {
	t.Helper()
	m, ok := r.Result.(map[string]any)
	require.True(t, ok)
	rows, ok := m["rows"].([][]any)
	require.True(t, ok)
	return rows
}
