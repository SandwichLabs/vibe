package agents

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// fakeConnSession wraps a real hub and adjusts what ConnectionInfos reports:
// connections the hub does not model (a storage connection with a scope, which
// needs httpfs and a bucket) and the "remote" flag of a catalog (a Postgres
// server stands in as a DuckDB file here; the integration tests use a real one).
type fakeConnSession struct {
	session.Session
	extra  []session.ConnectionInfo
	remote map[string]bool
}

func (s *fakeConnSession) ConnectionInfos() []session.ConnectionInfo {
	infos := s.Session.ConnectionInfos()
	for i := range infos {
		if s.remote[strings.ToLower(infos[i].Name)] {
			infos[i].Remote = true
		}
	}
	return append(infos, s.extra...)
}

// policyAccess is a workspace-shaped WorkspaceAccess: a real hub plus a
// data/ folder.
type policyAccess struct {
	*sessionAccess
	dataDir string
}

func (c *policyAccess) DataDir() string { return c.dataDir }

// policyFixture is a handler over a real hub:
//
//	default catalog (memory): t, t2, users
//	other  (attached, writable):  users, orders
//	ro     (attached read-only): ledger
//	data/                          x.csv, y.csv, link.csv -> outside/secret.csv, sub/z.csv
type policyFixture struct {
	t        *testing.T
	dir      string
	dataDir  string
	outside  string
	sess     *fakeConnSession
	db       *sql.DB
	h        *Handler
	approver *approverStub
}

// approverStub answers every approval request with a fixed decision and records
// what it was asked.
type approverStub struct {
	mu       sync.Mutex
	decision error
	asked    []ApprovalRequest
}

func (a *approverStub) fn(_ context.Context, req ApprovalRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, req)
	return a.decision
}

func (a *approverStub) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.asked)
}

type fixtureOpt func(*fixtureCfg)

type fixtureCfg struct {
	ask       bool
	approver  bool
	remote    []string
	extra     []session.ConnectionInfo
	noDataDir bool
	extDir    string
}

func withAsk(v bool) fixtureOpt { return func(c *fixtureCfg) { c.ask = v } }
func withApprover() fixtureOpt  { return func(c *fixtureCfg) { c.approver = true } }
func withRemote(names ...string) fixtureOpt {
	return func(c *fixtureCfg) { c.remote = append(c.remote, names...) }
}
func withExtraConns(ci ...session.ConnectionInfo) fixtureOpt {
	return func(c *fixtureCfg) { c.extra = append(c.extra, ci...) }
}
func withoutDataDir() fixtureOpt     { return func(c *fixtureCfg) { c.noDataDir = true } }
func withExtDir(d string) fixtureOpt { return func(c *fixtureCfg) { c.extDir = d } }

func newPolicyFixture(t *testing.T, mode SandboxMode, opts ...fixtureOpt) *policyFixture {
	t.Helper()
	cfg := &fixtureCfg{}
	for _, o := range opts {
		o(cfg)
	}
	dir, err := os.MkdirTemp("", "policy-fx-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// The data dir is resolved through symlinks the same way the policy does
	// (t.TempDir can sit under a symlinked /tmp).
	realDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	dir = realDir
	dataDir := filepath.Join(dir, "ws", "data")
	outside := filepath.Join(dir, "outside")
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "sub"), 0o755))
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "x.csv"), []byte("a,b\n1,2\n3,4\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "y.csv"), []byte("a,b\n5,6\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "sub", "z.csv"), []byte("a,b\n7,8\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.csv"), []byte("a,b\n99,99\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.csv"), filepath.Join(dataDir, "link.csv")))

	extDir := filepath.Join(dir, "ext")
	if cfg.extDir != "" {
		extDir = cfg.extDir
	}
	sess, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(extDir),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })

	mk := func(file, ddl string) string {
		p := filepath.Join(dir, file)
		f, err := sql.Open("duckdb", p)
		require.NoError(t, err)
		_, err = f.Exec(ddl)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		return p
	}
	otherPath := mk("other.duckdb", `CREATE TABLE users (uid INTEGER PRIMARY KEY, email VARCHAR);
		INSERT INTO users VALUES (1, 'a@x.io');
		CREATE TABLE orders (id INTEGER, total INTEGER); INSERT INTO orders VALUES (1, 10), (2, 20);`)
	roPath := mk("ro.duckdb", `CREATE TABLE ledger (id INTEGER, amount INTEGER); INSERT INTO ledger VALUES (1, 100);`)
	ctx := context.Background()
	require.NoError(t, sess.Attach(ctx, session.Spec{Name: "other", Path: otherPath}))
	require.NoError(t, sess.Attach(ctx, session.Spec{Name: "ro", Path: roPath, ReadOnly: true}))
	_, err = sess.DB().Exec(`CREATE TABLE t (id INTEGER, name VARCHAR); INSERT INTO t VALUES (1, 'Ada');
		CREATE TABLE t2 (id INTEGER); INSERT INTO t2 VALUES (1);
		CREATE TABLE users (id INTEGER, name VARCHAR, email VARCHAR); INSERT INTO users VALUES (1, 'Alice', 'alice@example.com');`)
	require.NoError(t, err)

	fs := &fakeConnSession{Session: sess, extra: cfg.extra, remote: map[string]bool{}}
	for _, n := range cfg.remote {
		fs.remote[strings.ToLower(n)] = true
	}
	dd := dataDir
	if cfg.noDataDir {
		dd = ""
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.NewManager(sess, engine.WithLogger(logger))
	h := NewHandler(&policyAccess{sessionAccess: &sessionAccess{mockAccess: &mockAccess{}, sess: fs}, dataDir: dd}, eng,
		&SandboxConfig{Mode: mode, MaxQueryRows: 100, RequireApproval: cfg.ask}, logger)
	fx := &policyFixture{t: t, dir: dir, dataDir: dataDir, outside: outside, sess: fs, db: sess.DB(), h: h}
	if cfg.approver {
		fx.approver = &approverStub{}
		h.SetApprover(fx.approver.fn)
	}
	return fx
}

// classify runs the full policy exactly as run_query does.
func (fx *policyFixture) classify(q string) *sqlVerdict {
	fx.t.Helper()
	return fx.h.classify(context.Background(), fx.db, q, nil)
}

// run dispatches run_query through the handler.
func (fx *policyFixture) run(q string) *Response {
	fx.t.Helper()
	return fx.h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: "1", Method: "run_query", Params: map[string]any{"sql": q}})
}

// call dispatches any tool.
func (fx *policyFixture) call(method string, params map[string]any) *Response {
	fx.t.Helper()
	return fx.h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: "1", Method: method, Params: params})
}

// exec runs SQL directly on the hub, bypassing the policy (to set up views and
// macros an owner could have made).
func (fx *policyFixture) exec(q string) {
	fx.t.Helper()
	_, err := fx.db.Exec(q)
	require.NoError(fx.t, err, q)
}

func (fx *policyFixture) count(q string) int {
	fx.t.Helper()
	var n int
	require.NoError(fx.t, fx.db.QueryRow(q).Scan(&n), q)
	return n
}

// p is a path inside data/.
func (fx *policyFixture) p(rel string) string { return filepath.Join(fx.dataDir, rel) }

// mapOf asserts v is a JSON object.
func mapOf(t testing.TB, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "expected an object, got %T", v)
	return m
}
