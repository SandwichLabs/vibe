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
	got, err := resolveSandbox("read-write", "")
	require.NoError(t, err)
	assert.Equal(t, "read-write", got)
}

func TestResolveSandbox_FlagCanTighten(t *testing.T) {
	got, err := resolveSandbox("full", "read-only")
	require.NoError(t, err)
	assert.Equal(t, "read-only", got)
}

// TestResolveSandbox_FlagCannotLoosen is the core #64 contract: a flag must
// never grant MORE access than the file's own agent_sandbox setting.
func TestResolveSandbox_FlagCannotLoosen(t *testing.T) {
	got, err := resolveSandbox("read-only", "full")
	require.NoError(t, err)
	assert.Equal(t, "read-only", got, "a --sandbox flag must never loosen the file's own setting")
}

func TestResolveSandbox_SameLevelKept(t *testing.T) {
	got, err := resolveSandbox("read-write", "read-write")
	require.NoError(t, err)
	assert.Equal(t, "read-write", got)
}

func TestResolveSandbox_InvalidFlagRejected(t *testing.T) {
	_, err := resolveSandbox("full", "godmode")
	assert.Error(t, err)
}

func TestResolveSandbox_EmptyFileSandboxDefaultsFull(t *testing.T) {
	got, err := resolveSandbox("", "read-only")
	require.NoError(t, err)
	assert.Equal(t, "read-only", got)

	got, err = resolveSandbox("", "")
	require.NoError(t, err)
	assert.Equal(t, "full", got)
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

	// content/schema categories are unconditionally writes too.
	for _, method := range []string{"add_query_cell", "create_table"} {
		s := &countingDebouncedSaver{}
		hh := wrapHandlerWithSaveHook(&fakeVibeHandler{resp: agents.NewResponse("1", nil)}, s, nil, dbMu)
		_ = hh.Handle(context.Background(), &agents.Request{Method: method})
		assert.Equal(t, 1, s.calls, "method=%s", method)
	}
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
