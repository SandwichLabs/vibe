package cmd

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	vibemcp "github.com/sandwich-labs/vibe-app/internal/mcp"
)

// saveDebounce is how long a debouncedSaver waits after the LAST
// write-category tool call before actually checkpointing + saving the
// container. Chosen to absorb a burst of several tool calls in one agent
// turn (e.g. create_table then several add_column/create_record calls) into
// a single Save, while still bounding how much work a crashed/killed `vibe
// mcp serve` session (no clean stdin EOF or signal — e.g. `kill -9`, a host
// OOM) can lose to a few seconds' worth of writes rather than the whole
// session. Save also always runs once more on a clean exit (EOF/SIGINT/
// SIGTERM), so this is purely an insurance policy against the unclean case.
const saveDebounce = 5 * time.Second

// debouncedSaver periodically checkpoints + saves a container after a tool
// call that actually wrote something (see saveHookHandler.wroteData),
// without paying a Save cost on every single call — a read-heavy session,
// the common case, never marks anything dirty and never saves early at all;
// only the final save on exit applies to it.
type debouncedSaver struct {
	c        *container.Container
	logger   *slog.Logger
	readOnly bool
	// dbMu is the SAME lock saveHookHandler.Handle takes (as a read lock)
	// around every tool dispatch — review fix (finding 5): doSave takes it
	// exclusively so a periodic save can never run concurrently with an
	// in-flight tool call against the same DuckDB connection, and a new
	// tool call can't start while a save is in progress.
	dbMu *sync.RWMutex
	// debounce is saveDebounce in production; a test constructs a
	// debouncedSaver literal directly with a shorter value to keep the
	// timer-fires case fast without changing the production constant.
	debounce time.Duration

	mu      sync.Mutex // guards timer and stopped
	timer   *time.Timer
	stopped bool

	// saveMu serializes actual Save() calls and lets stop() wait for one
	// already in flight to finish — review fix (finding 5): timer.Stop()
	// alone does not stop a save that has ALREADY fired and is running; a
	// shutdown that proceeds to the final Save/Close while that periodic
	// save is still in progress is exactly the race being fixed here.
	saveMu sync.Mutex
}

func newDebouncedSaver(c *container.Container, logger *slog.Logger, readOnly bool, dbMu *sync.RWMutex) *debouncedSaver {
	return &debouncedSaver{c: c, logger: logger, readOnly: readOnly, dbMu: dbMu, debounce: saveDebounce}
}

// markDirty (re)starts the debounce timer. A read-only-file session (no
// Save is ever valid against it) is a no-op, and so — once stop() has run —
// is every session: review fix (round 2, "harden the saver"). A tool
// dispatch that was already in flight when Stop()/stop() ran (e.g. a
// slow-running tool call under saveHookHandler.Handle's dbMu.RLock) can
// still complete and call markDirty AFTER stop() has returned — cmd/mcp.go's
// finalize() has by then already done its own final Save and is about to
// (or already did) Close() the container. Arming a new timer at that point
// would fire doSave against a closed/nil'd-out DuckDB connection later,
// which nil-derefs. Once stopped, markDirty is permanently inert.
func (s *debouncedSaver) markDirty() {
	if s.readOnly {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	debounce := s.debounce
	if debounce <= 0 {
		debounce = saveDebounce
	}
	s.timer = time.AfterFunc(debounce, s.doSave)
}

// doSave runs the actual periodic Save, serialized against both any other
// doSave call (saveMu) and every tool dispatch (dbMu, exclusive).
func (s *debouncedSaver) doSave() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if err := s.c.Save(); err != nil {
		s.logger.Error("mcp serve: periodic save failed", "error", err)
	} else {
		s.logger.Debug("mcp serve: periodic save completed")
	}
}

// stop cancels any pending (not-yet-fired) debounced save, THEN waits for a
// save that had already fired and is in progress to finish — review fix
// (finding 5): calling this before the caller's own final, synchronous Save
// guarantees no periodic save is still running underneath it, and that the
// final Save (and the Close that follows) only ever start once every prior
// save has fully completed.
func (s *debouncedSaver) stop() {
	s.mu.Lock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	// If doSave is currently running, block until it releases saveMu; if
	// not, this acquires and immediately releases it — either way, no save
	// this debouncedSaver started is still in flight once stop() returns.
	s.saveMu.Lock()
	s.saveMu.Unlock() //nolint:staticcheck // deliberate lock/unlock-as-barrier, not a real critical section
}

// writeToolCategories are the ToolDef categories that are ALWAYS a write
// when the tool call succeeds — every category isWriteOperation
// (internal/agents) treats as requiring approval, EXCEPT "data": run_query
// is also category "data" but is very often a plain SELECT, so it needs its
// own classification (wroteData) rather than a blanket category match. The
// non-run_query "data" tools (create_record/update_record/delete_record/
// bulk_insert) are unconditionally writes — handled as a special case in
// wroteData, not lumped in here.
var writeToolCategories = map[string]bool{
	"content": true, "schema": true,
}

// dirtyMarker is the narrow interface saveHookHandler needs from a
// debouncedSaver — factored out so tests can substitute a counting double
// without letting a real save timer fire.
type dirtyMarker interface {
	markDirty()
}

// saveHookHandler wraps a vibemcp.Handler so every successful tool call
// that actually wrote something marks a debouncedSaver dirty — see
// wroteData. Every dispatch is also serialized against a concurrent Save
// via dbMu (review fix, finding 5).
type saveHookHandler struct {
	inner  vibemcp.Handler
	saver  dirtyMarker
	duckDB *sql.DB
	dbMu   *sync.RWMutex
}

func wrapHandlerWithSaveHook(inner vibemcp.Handler, saver dirtyMarker, duckDB *sql.DB, dbMu *sync.RWMutex) vibemcp.Handler {
	return &saveHookHandler{inner: inner, saver: saver, duckDB: duckDB, dbMu: dbMu}
}

func (h *saveHookHandler) Handle(ctx context.Context, req *agents.Request) *agents.Response {
	h.dbMu.RLock()
	defer h.dbMu.RUnlock()
	resp := h.inner.Handle(ctx, req)
	if resp != nil && resp.Error == nil && h.wroteData(ctx, req) {
		h.saver.markDirty()
	}
	return resp
}

// wroteData reports whether req (already dispatched successfully) actually
// wrote something — review fix (finding 6): the ORIGINAL version marked
// dirty for ANY successful "data"-category call, including a plain SELECT
// via run_query, triggering a full checkpoint+Save on every read. run_query
// is reclassified per call using agents.IsReadOnlyStatement — the SAME
// classifier handleRunQuery itself uses — so this can never drift from what
// the handler actually decided was safe to execute. Every other
// "data"-category tool (create_record/update_record/delete_record/
// bulk_insert) has no read form at all, so it is unconditionally a write;
// page/schema tools are likewise unconditionally writes
// (writeToolCategories). Anything else (introspect, list_skills, etc.) is
// never a write.
func (h *saveHookHandler) wroteData(ctx context.Context, req *agents.Request) bool {
	tool := agents.GetTool(req.Method)
	if tool == nil {
		return false
	}
	if tool.Category == "data" {
		if req.Method != "run_query" {
			return true // create_record/update_record/delete_record/bulk_insert
		}
		sqlText, _ := req.Params["sql"].(string)
		var params map[string]any
		if p, ok := req.Params["params"].(map[string]any); ok {
			params = p
		}
		if h.duckDB == nil {
			// Can't classify — fail toward saving rather than silently
			// losing a write we couldn't verify was safe to skip.
			return true
		}
		return !agents.IsReadOnlyStatement(ctx, h.duckDB, sqlText, params)
	}
	return writeToolCategories[tool.Category]
}

// splitHostPortLenient splits addr into host/port, tolerating a bare host
// with no port (net.SplitHostPort errors on that) by treating the whole
// string as the host in that case.
func splitHostPortLenient(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err == nil {
		return host, port, nil
	}
	// No port present, or some other split failure — validate addr looks
	// like a bare host/IP by round-tripping it through net.JoinHostPort with
	// a throwaway port; if that still fails to split, it's not a valid host.
	if _, _, err2 := net.SplitHostPort(net.JoinHostPort(addr, "0")); err2 == nil {
		return addr, "", nil
	}
	return "", "", err
}
