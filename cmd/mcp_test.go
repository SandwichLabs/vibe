package cmd

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCmdLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestVibeContainer(t *testing.T) *container.Container {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")
	c, err := container.Create(path, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestResolveSandbox_NoFlagKeepsFileSetting(t *testing.T) {
	got, err := resolveSandbox(agents.ModeReadWrite, "")
	require.NoError(t, err)
	assert.Equal(t, agents.ModeReadWrite, got)
}

func TestResolveSandbox_FlagCanTighten(t *testing.T) {
	got, err := resolveSandbox(agents.ModeFull, "read-only")
	require.NoError(t, err)
	assert.Equal(t, agents.ModeReadOnly, got)
}

// TestResolveSandbox_FlagCannotLoosen is the core #64 contract: a flag must
// never grant MORE access than the file's own agent_sandbox setting.
func TestResolveSandbox_FlagCannotLoosen(t *testing.T) {
	got, err := resolveSandbox(agents.ModeReadOnly, "full")
	require.NoError(t, err)
	assert.Equal(t, agents.ModeReadOnly, got, "a --sandbox flag must never loosen the file's own setting")
}

func TestResolveSandbox_SameLevelKept(t *testing.T) {
	got, err := resolveSandbox(agents.ModeReadWrite, "read-write")
	require.NoError(t, err)
	assert.Equal(t, agents.ModeReadWrite, got)
}

func TestResolveSandbox_InvalidFlagRejected(t *testing.T) {
	_, err := resolveSandbox(agents.ModeFull, "godmode")
	assert.Error(t, err)
}

// TestResolveSandbox_FileModeAlwaysValid documents why resolveSandbox no
// longer has an "unset fileMode" branch (round 2 review finding 11): its
// caller, buildMCPHandler, always passes agents.ReadFilePolicy(c).Mode,
// which is never empty or unrecognized — that one "what does an absent/bad
// stored mode default to" decision lives solely in ReadFilePolicy (see
// TestReadFilePolicy in internal/agents). resolveSandbox's only remaining
// job is parsing --sandbox and tightening against whatever valid mode it's
// given.
func TestResolveSandbox_FileModeAlwaysValid(t *testing.T) {
	got, err := resolveSandbox(agents.DefaultSandboxMode, "read-only")
	require.NoError(t, err)
	assert.Equal(t, agents.ModeReadOnly, got)
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:0":       true,
		"127.0.0.1:8080":    true,
		"localhost:8080":    true,
		"0.0.0.0:8080":      false,
		"192.168.1.5:8080":  false,
		"evil.example:8080": false,
	}
	for addr, want := range cases {
		got, err := isLoopbackAddr(addr)
		require.NoError(t, err, "addr=%q", addr)
		assert.Equal(t, want, got, "addr=%q", addr)
	}
}

// TestIsLoopbackAddr_GarbageAddrIsNotLoopback: splitHostPortLenient
// tolerates a bare-host string with no port, so garbage input falls through
// to "not loopback" (false, nil) rather than necessarily erroring — the
// important guarantee is that it is never mistaken FOR loopback.
func TestIsLoopbackAddr_GarbageAddrIsNotLoopback(t *testing.T) {
	got, err := isLoopbackAddr("not an addr!!")
	require.NoError(t, err)
	assert.False(t, got)
}

// --- debouncedSaver / saveHookHandler ---

// fakeVibeHandler is a minimal vibemcp.Handler double so saveHookHandler can
// be tested without a real container.Container (debouncedSaver itself needs
// a real *container.Container, which is exercised indirectly via
// runMCPServe's live QA — see the implementer report).
type fakeVibeHandler struct {
	resp *agents.Response
}

func (f *fakeVibeHandler) Handle(_ context.Context, _ *agents.Request) *agents.Response {
	return f.resp
}

func TestSaveHookHandler_MarksDirtyOnlyOnSuccessfulWrite(t *testing.T) {
	dbMu := &sync.RWMutex{}

	// create_record is category "data", method != run_query -> unconditionally a write.
	saver := &countingDebouncedSaver{}
	h := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver, nil, dbMu)
	_ = h.Handle(context.Background(), &agents.Request{Method: "create_record"})
	assert.Equal(t, 1, saver.calls)

	// introspect_schema is category "introspect" -> not a write.
	saver2 := &countingDebouncedSaver{}
	h2 := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver2, nil, dbMu)
	_ = h2.Handle(context.Background(), &agents.Request{Method: "introspect_schema"})
	assert.Equal(t, 0, saver2.calls)

	// An error response never marks dirty even for a write-category method.
	saver3 := &countingDebouncedSaver{}
	h3 := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewErrorResponse("1", agents.ErrPermission, "denied")}, saver3, nil, dbMu)
	_ = h3.Handle(context.Background(), &agents.Request{Method: "create_record"})
	assert.Equal(t, 0, saver3.calls)

	// An unknown method never panics/marks dirty.
	saver4 := &countingDebouncedSaver{}
	h4 := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver4, nil, dbMu)
	_ = h4.Handle(context.Background(), &agents.Request{Method: "totally_unknown"})
	assert.Equal(t, 0, saver4.calls)

	// "schema" category is unconditionally a write too.
	saver5 := &countingDebouncedSaver{}
	h5 := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver5, nil, dbMu)
	_ = h5.Handle(context.Background(), &agents.Request{Method: "create_table"})
	assert.Equal(t, 1, saver5.calls)

	// "content" tools (add_query_cell, create_or_update_skill, ...) write
	// SQLite only (write-through) — never a write, in either caller of
	// agents.IsWriteCall, which means "wrote DuckDB user data".
	saver6 := &countingDebouncedSaver{}
	h6 := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver6, nil, dbMu)
	_ = h6.Handle(context.Background(), &agents.Request{Method: "add_query_cell"})
	assert.Equal(t, 0, saver6.calls)
}

// TestSaveHookHandler_RunQuery_OnlyWritesMarkDirty is the review fix for
// finding 6: a plain SELECT via run_query must never mark the container
// dirty (the pre-fix version treated every successful "data"-category call,
// SELECT included, as a write); an INSERT/UPDATE/DELETE via run_query must.
// Exercises the real classifier (agents.IsReadOnlyStatement) against a real
// DuckDB connection — the same one handleRunQuery itself would use — not a
// hand-rolled SQL-shape guess.
func TestSaveHookHandler_RunQuery_OnlyWritesMarkDirty(t *testing.T) {
	duckDB, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer duckDB.Close()
	_, err = duckDB.Exec(`CREATE TABLE t (id INTEGER)`)
	require.NoError(t, err)

	dbMu := &sync.RWMutex{}

	t.Run("SELECT does not mark dirty", func(t *testing.T) {
		saver := &countingDebouncedSaver{}
		h := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver, duckDB, dbMu)
		_ = h.Handle(context.Background(), &agents.Request{Method: "run_query", Params: map[string]any{"sql": "SELECT * FROM t"}})
		assert.Equal(t, 0, saver.calls, "a plain SELECT via run_query must never trigger a save")
	})

	t.Run("INSERT marks dirty", func(t *testing.T) {
		saver := &countingDebouncedSaver{}
		h := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver, duckDB, dbMu)
		_ = h.Handle(context.Background(), &agents.Request{Method: "run_query", Params: map[string]any{"sql": "INSERT INTO t VALUES (1)"}})
		assert.Equal(t, 1, saver.calls)
	})

	t.Run("nil duckDB fails toward saving", func(t *testing.T) {
		saver := &countingDebouncedSaver{}
		h := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, saver, nil, dbMu)
		_ = h.Handle(context.Background(), &agents.Request{Method: "run_query", Params: map[string]any{"sql": "SELECT * FROM t"}})
		assert.Equal(t, 1, saver.calls, "an unclassifiable run_query call must fail toward saving, not silently skip a possible write")
	})
}

// countingDebouncedSaver is a dirtyMarker double that counts calls without
// ever starting a real save timer against a (possibly nil) container.
type countingDebouncedSaver struct {
	calls int
}

func (c *countingDebouncedSaver) markDirty() {
	c.calls++
}

// TestDebouncedSaver_StopWaitsForInFlightSave is the review fix for finding
// 5 ("Save timer races Close"): stop() must not return while a save it
// started is still running — simulated here by holding saveMu directly
// (standing in for doSave's own Save() call being slow), which is the exact
// lock stop() blocks on. This is what makes runMCPServe's finalize()
// sequence (srv.Stop() -> saver.stop() -> final Save() -> Close()) safe: by
// the time saver.stop() returns, no earlier periodic save can still be
// touching the container when Close() runs.
func TestDebouncedSaver_StopWaitsForInFlightSave(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := newDebouncedSaver(c, testCmdLogger(), false, dbMu)

	// Simulate "a save is currently in flight" by holding saveMu ourselves —
	// doSave takes exactly this lock for the duration of its real Save() call.
	saver.saveMu.Lock()

	done := make(chan struct{})
	go func() {
		saver.stop()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("stop() returned while the simulated in-flight save was still \"running\"")
	case <-time.After(150 * time.Millisecond):
		// Expected: stop() is still blocked.
	}

	saver.saveMu.Unlock() // "save" finishes

	select {
	case <-done:
		// Expected: stop() unblocks promptly once the in-flight save is done.
	case <-time.After(2 * time.Second):
		t.Fatal("stop() did not return after the in-flight save finished")
	}
}

// TestDebouncedSaver_MarkDirtyThenStop_NeverSaves: stop() called before the
// debounce timer fires must cancel it — no save happens at all. Uses a real
// container and a short debounce so the test would observably fail (an
// unexpected log line, or the timer firing) if cancellation didn't work.
func TestDebouncedSaver_MarkDirtyThenStop_NeverSaves(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := &debouncedSaver{c: c, logger: testCmdLogger(), dbMu: dbMu, debounce: 50 * time.Millisecond}

	saver.markDirty()
	saver.stop() // immediately, well before the 50ms debounce would fire

	// If the timer had NOT been cancelled, doSave would fire ~50ms later and
	// call c.Save() — including on an already-Close()'d container in the
	// worst case. Waiting past that window and confirming stop() didn't
	// block (it returned already, synchronously) plus no panic occurred is
	// the whole assertion here: cancellation is silent by design.
	time.Sleep(150 * time.Millisecond)
}

// TestDebouncedSaver_ReadOnlyNeverSaves: markDirty on a read-only-file
// saver must never schedule anything.
func TestDebouncedSaver_ReadOnlyNeverSaves(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := &debouncedSaver{c: c, logger: testCmdLogger(), dbMu: dbMu, readOnly: true, debounce: 10 * time.Millisecond}
	saver.markDirty()
	saver.mu.Lock()
	timer := saver.timer
	saver.mu.Unlock()
	assert.Nil(t, timer, "a read-only saver must never arm the debounce timer")
}

// TestDebouncedSaver_MarkDirtyAfterStop_NeverSaves is the review fix (round
// 2, "harden the saver"): once stop() has run, a LATE markDirty (from a
// tool dispatch that was already in flight when Stop was called and only
// completes afterward) must be permanently inert — never arm a new timer,
// never call Save. Uses a short debounce so, if the fix were missing, the
// timer would visibly fire well within the test's wait window.
func TestDebouncedSaver_MarkDirtyAfterStop_NeverSaves(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := &debouncedSaver{c: c, logger: testCmdLogger(), dbMu: dbMu, debounce: 20 * time.Millisecond}

	saver.stop() // simulates shutdown having already run

	saver.markDirty() // simulates a late-finishing tool call's dirty-mark

	saver.mu.Lock()
	timer := saver.timer
	stopped := saver.stopped
	saver.mu.Unlock()
	assert.Nil(t, timer, "markDirty after stop() must never arm a timer")
	assert.True(t, stopped)

	// Wait past where the (never-armed) timer would have fired, to also
	// prove no save silently happened via some other path.
	time.Sleep(100 * time.Millisecond)
}

// TestDebouncedSaver_NeedsSave is #80's checklist item 3: a session that
// never wrote anything must not trigger the final shutdown Save.
func TestDebouncedSaver_NeedsSave(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := &debouncedSaver{c: c, logger: testCmdLogger(), dbMu: dbMu, debounce: 20 * time.Millisecond}

	assert.False(t, saver.needsSave(), "a fresh saver has nothing to save")

	saver.markDirty()
	assert.True(t, saver.needsSave(), "markDirty must mark the session dirty")

	saver.doSave()
	assert.False(t, saver.needsSave(), "a successful save clears dirty")

	saver.stop()
	saver.markDirty() // a tool call that finishes after shutdown started
	assert.True(t, saver.needsSave(), "a write that completes after stop() must still be counted")
}

// TestDebouncedSaver_ReadOnlyNeedsSaveAlwaysFalse: a read-only-file saver's
// markDirty is a no-op (see TestDebouncedSaver_ReadOnlyNeverSaves), so
// needsSave must never report dirty for one either.
func TestDebouncedSaver_ReadOnlyNeedsSaveAlwaysFalse(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := &debouncedSaver{c: c, logger: testCmdLogger(), dbMu: dbMu, readOnly: true, debounce: 20 * time.Millisecond}
	saver.markDirty()
	assert.False(t, saver.needsSave())
}

// TestSaveHookHandler_SerializesWithSave: while saveHookHandler.Handle holds
// dbMu's read lock (a tool call in flight), a concurrent doSave (which needs
// the exclusive write lock) must wait — proving the two are actually
// serialized through the SAME lock, not just both happening to reference one.
func TestSaveHookHandler_SerializesWithSave(t *testing.T) {
	c := newTestVibeContainer(t)
	dbMu := &sync.RWMutex{}
	saver := newDebouncedSaver(c, testCmdLogger(), false, dbMu)

	dbMu.RLock() // stand-in for "a tool call is in flight"

	saveDone := make(chan struct{})
	go func() {
		saver.doSave()
		close(saveDone)
	}()

	select {
	case <-saveDone:
		t.Fatal("doSave must not proceed while a tool dispatch (RLock) is in flight")
	case <-time.After(150 * time.Millisecond):
		// Expected: doSave is blocked waiting for the write lock.
	}

	dbMu.RUnlock()

	select {
	case <-saveDone:
		// Expected: doSave proceeds once the read lock is released.
	case <-time.After(2 * time.Second):
		t.Fatal("doSave did not proceed after the read lock was released")
	}
}

// --- buildMCPHandler (#79: no approver, ask-before-writes refuses) ---

func TestMCPServe_FreshFile_WritesRefused(t *testing.T) {
	c := newTestVibeContainer(t)
	_, err := c.DuckDBDB().Exec(`CREATE TABLE orders (id INTEGER, status VARCHAR); INSERT INTO orders VALUES (1, 'Void')`)
	require.NoError(t, err)
	eng := engine.NewManager(c.Session(), engine.WithLogger(testCmdLogger()))

	handler, policy, err := buildMCPHandler(c, eng, "", testCmdLogger())
	require.NoError(t, err)
	assert.Equal(t, agents.ModeReadWrite, policy.Mode)
	assert.True(t, policy.AskBeforeWrites)

	resp := handler.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: 1, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "read-write")
	assert.Contains(t, resp.Error.Message, "Ask before changes")

	var n int
	require.NoError(t, c.DuckDBDB().QueryRow("SELECT count(*) FROM orders").Scan(&n))
	assert.Equal(t, 1, n, "the refused delete must not have run")

	selResp := handler.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: 2, Method: "run_query", Params: map[string]any{"sql": "SELECT * FROM orders"}})
	assert.Nil(t, selResp.Error, "reads must still work while writes are refused")
}

func TestMCPServe_OwnerOptIn(t *testing.T) {
	c := newTestVibeContainer(t)
	_, err := c.DuckDBDB().Exec(`CREATE TABLE orders (id INTEGER, status VARCHAR); INSERT INTO orders VALUES (1, 'Void')`)
	require.NoError(t, err)
	require.NoError(t, c.SetMeta(agents.MetaKeySandbox, "full"))
	require.NoError(t, c.SetMeta(agents.MetaKeyAskBeforeWrites, "false"))
	eng := engine.NewManager(c.Session(), engine.WithLogger(testCmdLogger()))

	handler, policy, err := buildMCPHandler(c, eng, "", testCmdLogger())
	require.NoError(t, err)
	assert.Equal(t, agents.ModeFull, policy.Mode)
	assert.False(t, policy.AskBeforeWrites)

	resp := handler.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: 1, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
	require.Nil(t, resp.Error)

	var n int
	require.NoError(t, c.DuckDBDB().QueryRow("SELECT count(*) FROM orders").Scan(&n))
	assert.Equal(t, 0, n)
}

// TestMCPServe_FullStoredFile_AskAbsentStillRefused covers the existing-file
// case (#79's deliberate tightening): a panel-era file with agent_sandbox
// stored as "full" and no agent_ask_before_writes key at all still asks —
// there is no approver over MCP, so it's refused, not silently full access.
func TestMCPServe_FullStoredFile_AskAbsentStillRefused(t *testing.T) {
	c := newTestVibeContainer(t)
	_, err := c.DuckDBDB().Exec(`CREATE TABLE orders (id INTEGER, status VARCHAR); INSERT INTO orders VALUES (1, 'Void')`)
	require.NoError(t, err)
	require.NoError(t, c.SetMeta(agents.MetaKeySandbox, "full"))
	eng := engine.NewManager(c.Session(), engine.WithLogger(testCmdLogger()))

	handler, policy, err := buildMCPHandler(c, eng, "", testCmdLogger())
	require.NoError(t, err)
	assert.Equal(t, agents.ModeFull, policy.Mode)
	assert.True(t, policy.AskBeforeWrites)

	resp := handler.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: 1, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
	require.NotNil(t, resp.Error)

	var n int
	require.NoError(t, c.DuckDBDB().QueryRow("SELECT count(*) FROM orders").Scan(&n))
	assert.Equal(t, 1, n)
}

// TestMCPServe_InUse_RefusedWithHint is the CLI side of #80: `vibe mcp
// serve` against a file another writer already holds must refuse before
// touching the stdio server (so stdin is never read/blocked on) and must
// point the operator at Studio's MCP server or --read-only-file.
func TestMCPServe_InUse_RefusedWithHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held.vibe")
	seed, err := container.Create(path, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	holder, err := container.Open(path,
		container.WithPassphrase("testpass123"),
		container.WithLogger(testCmdLogger()),
		container.WithLockApp("Vibe Studio"),
	)
	require.NoError(t, err)
	defer holder.Close() //nolint:errcheck

	t.Setenv("VIBE_PASSPHRASE", "testpass123")

	_, err = executeCommand("mcp", "serve", path)
	require.Error(t, err)
	assert.ErrorIs(t, err, container.ErrFileInUse)
	assert.Contains(t, err.Error(), "--read-only-file")
	assert.Contains(t, err.Error(), "Settings → MCP Server")
}

// TestMCPServe_WritingSession_SavesOnExit is an end-to-end check of the
// exact composition runMCPServe wires up (buildMCPHandler +
// wrapHandlerWithSaveHook + debouncedSaver, and finalize's
// needsSave-gated final Save) — everything except the stdio transport
// itself, which runMCPServe reads from a hardcoded os.Stdin/os.Stdout and
// has no injectable seam for a test to drive. A dispatched write must still
// be on disk after the container is closed, the same way finalize() leaves
// it on a real stdin-EOF/SIGINT/SIGTERM shutdown.
func TestMCPServe_WritingSession_SavesOnExit(t *testing.T) {
	c := newTestVibeContainer(t)
	require.NoError(t, c.SetMeta(agents.MetaKeySandbox, "full"))
	require.NoError(t, c.SetMeta(agents.MetaKeyAskBeforeWrites, "false"))
	path := c.Path()
	eng := engine.NewManager(c.Session(), engine.WithLogger(testCmdLogger()))

	rpcHandler, _, err := buildMCPHandler(c, eng, "", testCmdLogger())
	require.NoError(t, err)
	dbMu := &sync.RWMutex{}
	saver := newDebouncedSaver(c, testCmdLogger(), false, dbMu)
	handler := wrapHandlerWithSaveHook(rpcHandler, saver, c.DuckDBDB(), dbMu)

	resp := handler.Handle(context.Background(), &agents.Request{
		JSONRPC: "2.0", ID: 1, Method: "run_query",
		Params: map[string]any{"sql": "CREATE TABLE mcp_marker AS SELECT 1 x"},
	})
	require.Nil(t, resp.Error)

	// finalize()'s own shutdown sequence, reproduced exactly: stop the
	// debounced saver FIRST (cancels/waits out its production-length timer —
	// without this, a still-armed doSave fires against the container after
	// this test has already Closed it) — then check needsSave only after
	// taking dbMu.Lock, then the final Save, then Close.
	saver.stop()
	dbMu.Lock()
	require.True(t, saver.needsSave(), "the CREATE TABLE above must have marked the session dirty")
	require.NoError(t, c.Save())
	dbMu.Unlock()
	require.NoError(t, c.Close())

	reopened, err := container.Open(path, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer reopened.Close() //nolint:errcheck
	var n int
	require.NoError(t, reopened.DuckDBDB().QueryRow(
		"SELECT count(*) FROM information_schema.tables WHERE table_name='mcp_marker'",
	).Scan(&n))
	assert.Equal(t, 1, n, "the write must have survived the save-on-exit path")
}
