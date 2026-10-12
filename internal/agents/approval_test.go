package agents

import (
	"context"
	"database/sql"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
)

// --- ParseSandboxMode ---

func TestParseSandboxMode(t *testing.T) {
	for _, m := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		got, err := ParseSandboxMode(string(m))
		require.NoError(t, err)
		assert.Equal(t, m, got)
	}
	_, err := ParseSandboxMode("godmode")
	assert.Error(t, err)
	_, err = ParseSandboxMode("")
	assert.Error(t, err)
}

// --- ApprovalQueue ---

func TestApprovalQueue_Approve(t *testing.T) {
	q := NewApprovalQueue()
	var announced ApprovalRequest
	announceCount := 0
	done := make(chan error, 1)
	go func() {
		done <- q.Wait(context.Background(), ApprovalRequest{ID: "a1", Summary: "do it"}, func(r ApprovalRequest) {
			announceCount++
			announced = r
		})
	}()
	require.Eventually(t, func() bool { return q.Resolve("a1", true) }, time.Second, time.Millisecond)
	require.NoError(t, <-done)
	assert.Equal(t, 1, announceCount)
	assert.Equal(t, "do it", announced.Summary)
}

func TestApprovalQueue_Decline(t *testing.T) {
	q := NewApprovalQueue()
	done := make(chan error, 1)
	go func() { done <- q.Wait(context.Background(), ApprovalRequest{ID: "a2"}, nil) }()
	require.Eventually(t, func() bool { return q.Resolve("a2", false) }, time.Second, time.Millisecond)
	assert.ErrorIs(t, <-done, ErrApprovalDeclined)
}

func TestApprovalQueue_Deadline(t *testing.T) {
	q := NewApprovalQueue()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := q.Wait(ctx, ApprovalRequest{ID: "a3"}, nil)
	assert.ErrorIs(t, err, ErrApprovalExpired)
}

func TestApprovalQueue_CancelAll(t *testing.T) {
	q := NewApprovalQueue()
	done := make(chan error, 1)
	go func() { done <- q.Wait(context.Background(), ApprovalRequest{ID: "a4"}, nil) }()
	require.Eventually(t, func() bool {
		q.CancelAll()
		select {
		case err := <-done:
			assert.ErrorIs(t, err, ErrApprovalCancelled)
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestApprovalQueue_ResolveUnknown(t *testing.T) {
	q := NewApprovalQueue()
	assert.False(t, q.Resolve("nope", true))
}

func TestApprovalQueue_DoubleResolveDoesNotBlock(t *testing.T) {
	q := NewApprovalQueue()
	done := make(chan error, 1)
	go func() { done <- q.Wait(context.Background(), ApprovalRequest{ID: "a5"}, nil) }()
	require.Eventually(t, func() bool { return q.Resolve("a5", true) }, time.Second, time.Millisecond)
	<-done
	// A second Resolve on the same (now-finished) id must report false, not
	// block or panic.
	assert.False(t, q.Resolve("a5", true))
}

// --- Handle approval matrix ---

// newApprovalTestHandler builds a Handler on a real DuckDB connection (like
// sql_classify_test.go's newRealDuckDBHandler) with an "orders" table seeded
// with rows, so run_query/delete_record/etc. have real side effects an
// approval test can assert on.
func newApprovalTestHandler(t *testing.T, mode SandboxMode) (*Handler, *sql.DB) {
	t.Helper()
	duckDB, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = duckDB.Close() })

	_, err = duckDB.Exec(`
		CREATE TABLE orders (id INTEGER, status VARCHAR);
		INSERT INTO orders VALUES (1, 'Void'), (2, 'Paid'), (3, 'Void');
	`)
	require.NoError(t, err)

	sandbox := &SandboxConfig{Mode: mode, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	container := &mockAccess{duckDB: duckDB}
	eng := engine.NewManager(sessiontest.ForTests(duckDB), engine.WithLogger(logger))
	return NewHandler(container, eng, sandbox, logger), duckDB
}

func orderCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM orders").Scan(&n))
	return n
}

func TestHandle_ApprovalMatrix(t *testing.T) {
	t.Run("sandbox denial never calls the approver", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeReadOnly)
		h.SetFilePolicy(ModeReadOnly, true)
		calls := 0
		h.SetApprover(func(context.Context, ApprovalRequest) error { calls++; return nil })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrPermission, resp.Error.Code)
		assert.Equal(t, 0, calls)
		assert.Equal(t, 3, orderCount(t, db))
	})

	t.Run("reads never call the approver", func(t *testing.T) {
		h, _ := newApprovalTestHandler(t, ModeFull)
		h.SetFilePolicy(ModeFull, true)
		calls := 0
		h.SetApprover(func(context.Context, ApprovalRequest) error { calls++; return nil })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 2, Method: "run_query", Params: map[string]any{"sql": "SELECT * FROM orders"}})
		require.Nil(t, resp.Error)
		assert.Equal(t, 0, calls)
	})

	t.Run("nil approver with ask on refuses and names the mode/switch", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeReadWrite)
		h.SetFilePolicy(ModeReadWrite, true)
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 3, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "read-write")
		assert.Contains(t, resp.Error.Message, "Ask before changes")
		assert.Equal(t, 3, orderCount(t, db))
	})

	t.Run("approve runs the write", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeReadWrite)
		h.SetFilePolicy(ModeReadWrite, true)
		var seen ApprovalRequest
		h.SetApprover(func(ctx context.Context, req ApprovalRequest) error {
			seen = req
			return nil
		})
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 4, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
		require.Nil(t, resp.Error)
		assert.Equal(t, int64(2), seen.Rows)
		assert.Contains(t, seen.Summary, "Delete 2 rows from orders")
		assert.Equal(t, 1, orderCount(t, db))
	})

	t.Run("decline blocks the write", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeReadWrite)
		h.SetFilePolicy(ModeReadWrite, true)
		h.SetApprover(func(context.Context, ApprovalRequest) error { return ErrApprovalDeclined })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 5, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
		assert.Equal(t, 3, orderCount(t, db))
	})

	t.Run("run_query SELECT never asks even with ask on", func(t *testing.T) {
		h, _ := newApprovalTestHandler(t, ModeFull)
		h.SetFilePolicy(ModeFull, true)
		calls := 0
		h.SetApprover(func(context.Context, ApprovalRequest) error { calls++; return nil })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 6, Method: "run_query", Params: map[string]any{"sql": "SELECT * FROM orders"}})
		require.Nil(t, resp.Error)
		assert.Equal(t, 0, calls)
	})

	t.Run("run_query DELETE asks and blocks on decline", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeFull)
		h.SetFilePolicy(ModeFull, true)
		h.SetApprover(func(context.Context, ApprovalRequest) error { return ErrApprovalDeclined })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 7, Method: "run_query", Params: map[string]any{"sql": "DELETE FROM orders WHERE status = 'Void'"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, 3, orderCount(t, db))
	})

	t.Run("run_query CREATE TABLE asks under full", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeFull)
		h.SetFilePolicy(ModeFull, true)
		calls := 0
		h.SetApprover(func(context.Context, ApprovalRequest) error { calls++; return nil })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 8, Method: "run_query", Params: map[string]any{"sql": "CREATE TABLE t2 (id INTEGER)"}})
		require.Nil(t, resp.Error)
		assert.Equal(t, 1, calls)
		var n int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_name = 't2'").Scan(&n))
		assert.Equal(t, 1, n)
	})

	t.Run("create_table asks", func(t *testing.T) {
		h, _ := newApprovalTestHandler(t, ModeFull)
		h.SetFilePolicy(ModeFull, true)
		calls := 0
		h.SetApprover(func(context.Context, ApprovalRequest) error { calls++; return nil })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 9, Method: "create_table", Params: map[string]any{"name": "widgets", "columns": []any{map[string]any{"name": "id", "type": "INTEGER"}}}})
		require.Nil(t, resp.Error)
		assert.Equal(t, 1, calls)
	})

	t.Run("ask off never calls the approver", func(t *testing.T) {
		h, db := newApprovalTestHandler(t, ModeReadWrite)
		h.SetFilePolicy(ModeReadWrite, false)
		calls := 0
		h.SetApprover(func(context.Context, ApprovalRequest) error { calls++; return nil })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 10, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
		require.Nil(t, resp.Error)
		assert.Equal(t, 0, calls)
		assert.Equal(t, 1, orderCount(t, db))
	})
}

func TestApproval_RowEstimate(t *testing.T) {
	h, db := newApprovalTestHandler(t, ModeFull)
	h.SetFilePolicy(ModeFull, true)

	t.Run("known where clause", func(t *testing.T) {
		var seen ApprovalRequest
		h.SetApprover(func(ctx context.Context, req ApprovalRequest) error { seen = req; return ErrApprovalDeclined })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, int64(2), seen.Rows)
		assert.Contains(t, seen.Summary, "status='Void'")
	})

	t.Run("unsafe where still asks with an unknown estimate, and the table survives decline", func(t *testing.T) {
		var seen ApprovalRequest
		h.SetApprover(func(ctx context.Context, req ApprovalRequest) error { seen = req; return ErrApprovalDeclined })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 2, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "1=0; DROP TABLE orders"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, int64(-1), seen.Rows)
		assert.Equal(t, 3, orderCount(t, db))
	})

	t.Run("drop_table counts rows", func(t *testing.T) {
		var seen ApprovalRequest
		h.SetApprover(func(ctx context.Context, req ApprovalRequest) error { seen = req; return ErrApprovalDeclined })
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 3, Method: "drop_table", Params: map[string]any{"name": "orders"}})
		require.NotNil(t, resp.Error)
		assert.Equal(t, int64(3), seen.Rows)
	})
}

// TestAwaitApproval_Cancelled proves the ErrApprovalCancelled path (file
// closed while waiting) surfaces as ErrApprovalNeeded with no side effect —
// exercised directly against the Handler rather than through
// studio/app.go's lock-releasing wrapper (that invariant is covered by
// studio's own TestAgentApproval_Chat, which needs the real App/file lock).
func TestAwaitApproval_Cancelled(t *testing.T) {
	h, db := newApprovalTestHandler(t, ModeFull)
	h.SetFilePolicy(ModeFull, true)
	h.SetApprover(func(context.Context, ApprovalRequest) error { return ErrApprovalCancelled })
	resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "delete_record", Params: map[string]any{"table": "orders", "where": "status='Void'"}})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Equal(t, 3, orderCount(t, db))
}

// TestApprovalQueue_ConcurrentWaitAndResolve is a -race guard: many
// concurrent Wait/Resolve pairs, plus stray Resolve calls on ids nobody is
// waiting on, must never race or block.
func TestApprovalQueue_ConcurrentWaitAndResolve(t *testing.T) {
	q := NewApprovalQueue()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		id := "id"
		wg.Add(2)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_ = q.Wait(ctx, ApprovalRequest{ID: id}, nil)
		}()
		go func() {
			defer wg.Done()
			q.Resolve(id, true)
		}()
	}
	wg.Wait()
}

func TestDescribeWrite_SchemaToolsShowColumnTypes(t *testing.T) {
	ar := describeWrite(context.Background(), &Request{Method: "create_table", Params: map[string]any{
		"name": "orders",
		"columns": []any{
			map[string]any{"name": "id", "type": "INTEGER PRIMARY KEY"},
			map[string]any{"name": "cust", "type": "INTEGER REFERENCES customers(id)"},
		},
	}}, nil, sqlClassWrite)
	assert.Equal(t, "Create table orders with id INTEGER PRIMARY KEY, cust INTEGER REFERENCES customers(id)", ar.Summary)

	ar = describeWrite(context.Background(), &Request{Method: "add_column", Params: map[string]any{
		"table": "orders", "name": "note", "type": "VARCHAR(20)",
	}}, nil, sqlClassWrite)
	assert.Equal(t, "Add column note VARCHAR(20) to orders", ar.Summary)
}
