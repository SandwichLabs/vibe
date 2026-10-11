package llm

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRealDuckDBHandlerForLLM builds a real agents.Handler wired to a REAL
// DuckDB connection, optionally with a table "t" (id INTEGER) holding 3
// rows when withTable is true — grounding recheck tests need actual SQL
// execution (run_query/introspect_schema), which the plain
// mockEngine/mockAccess test doubles elsewhere in this package can't
// satisfy (see internal/agents' identical caveat). withTable=false gives a
// real, CONFIRMED-empty DuckDB — hasAnyTables must see a genuine, successful
// "zero tables" result, not an error from querying information_schema
// against the wrong engine (which newTestHandler's plain double
// would produce, and which maybeGroundText treats as "uncertain, so check
// anyway" — the wrong fixture for testing the "no tables" skip specifically).
func newRealDuckDBHandlerForLLM(t *testing.T, withTable bool) *agents.Handler {
	t.Helper()
	duckDB, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = duckDB.Close() })

	if withTable {
		_, err = duckDB.Exec(`CREATE TABLE t (id INTEGER); INSERT INTO t VALUES (1), (2), (3);`)
		require.NoError(t, err)
	}

	sandbox := &agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	container := &mockAccess{duckDB: duckDB}
	eng := engine.NewManager(sessiontest.ForTests(duckDB), engine.WithLogger(logger))
	return agents.NewHandler(container, eng, sandbox, logger)
}

// --- maybeGroundText / grounding-recheck integration tests (#70 review). ---

// fakeSSEServer_UngroundedThenRunsQuery: turn 1 answers with an ungrounded
// number and no tool call; the grounding recheck (turn 2, continuing the
// SAME conversation) calls run_query and gives a grounded answer. Also
// captures every request body it sees, so a test can assert the recheck
// request actually carried the history/prior turn.
func fakeSSEServer_UngroundedThenRunsQuery(t *testing.T, ungroundedText, groundedText string) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))

		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		sawGroundingNudge := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}
		if len(body) > 0 && containsStr(string(body), "Do not repeat any action") {
			sawGroundingNudge = true
		}

		switch {
		case sawTool:
			// Recheck's follow-up after run_query ran: give the grounded answer.
			sseFinalTextResponse(t, w, groundedText)
		case sawGroundingNudge:
			// Recheck's first request: call run_query.
			sseToolCallResponse(t, w, "run_query", `{"sql":"SELECT COUNT(*) AS n FROM t"}`)
		default:
			// The turn's own first (and only) request: an ungrounded answer,
			// no tool call.
			sseFinalTextResponse(t, w, ungroundedText)
		}
	}))
	return srv, &bodies
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// newTestHandlerWithTable is a real DuckDB handler with a "t" table, so a
// grounding recheck's run_query call has something to actually query.
func newTestHandlerWithTable(t *testing.T) *agents.Handler {
	t.Helper()
	return newRealDuckDBHandlerForLLM(t, true)
}

// newTestHandlerNoTables is a real, CONFIRMED-empty DuckDB handler — see
// newRealDuckDBHandlerForLLM's doc comment for why this must be a real
// DuckDB with zero tables, not the plain newTestHandler.
func newTestHandlerNoTables(t *testing.T) *agents.Handler {
	t.Helper()
	return newRealDuckDBHandlerForLLM(t, false)
}

func TestMaybeGroundText_FiresOnceAndContinuesConversation(t *testing.T) {
	srv, bodies := fakeSSEServer_UngroundedThenRunsQuery(t, "There are roughly 42 rows in the table.", "There are exactly 3 rows in the table, confirmed by query.")
	defer srv.Close()

	handler := newTestHandlerWithTable(t)
	runner := NewRunner(handler, nil)

	var events []StreamEvent
	res, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "hi, previously you told me the row count", "", []Turn{
		{Role: "user", Text: "how many rows roughly"},
		{Role: "agent", Text: "about 40ish, I think"},
	}, func(ev StreamEvent) { events = append(events, ev) })
	require.NoError(t, err)

	assert.Equal(t, "There are exactly 3 rows in the table, confirmed by query.", res.Text)

	// Exactly one "verifying" event (fired once).
	verifyingCount := 0
	for _, ev := range events {
		if ev.Kind == "verifying" {
			verifyingCount++
		}
	}
	assert.Equal(t, 1, verifyingCount, "the grounding check must fire exactly once")

	// The recheck's tool call reached the UI as a tool chip (review fix #4).
	sawToolChip := false
	for _, ev := range events {
		if ev.Kind == "tool" && ev.Tool == "run_query" {
			sawToolChip = true
		}
	}
	assert.True(t, sawToolChip, "the recheck's run_query call must produce a live tool chip")

	// The recheck's own tool step is folded into the turn's Steps (review fix #4).
	foundRunQuery := false
	for _, s := range res.Steps {
		if s.Tool == "run_query" {
			foundRunQuery = true
		}
	}
	assert.True(t, foundRunQuery, "the recheck's tool steps must be returned in ChatResult.Steps")

	// The recheck request actually carried the prior conversation (review fix #2).
	require.GreaterOrEqual(t, len(*bodies), 2)
	last := (*bodies)[len(*bodies)-1]
	firstRecheckReq := (*bodies)[1] // request 0 = turn's own; request 1 = recheck's first request
	assert.Contains(t, firstRecheckReq, "about 40ish", "the recheck must include the turn's own prior history")
	assert.Contains(t, firstRecheckReq, "roughly 42 rows", "the recheck must include this turn's own (ungrounded) answer")
	_ = last
}

// TestMaybeGroundText_SkipsWhenGrounded: a turn that already ran run_query
// this turn must never trigger the recheck at all (single request).
func TestMaybeGroundText_SkipsWhenGrounded(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}
		if sawTool {
			sseFinalTextResponse(t, w, "There are 3 rows, verified by query.")
			return
		}
		sseToolCallResponse(t, w, "run_query", `{"sql":"SELECT COUNT(*) AS n FROM t"}`)
	}))
	defer srv.Close()

	handler := newTestHandlerWithTable(t)
	runner := NewRunner(handler, nil)
	var verifyingSeen bool
	res, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "how many rows", "", nil, func(ev StreamEvent) {
		if ev.Kind == "verifying" {
			verifyingSeen = true
		}
	})
	require.NoError(t, err)
	assert.False(t, verifyingSeen, "a turn that already ran a grounding tool must never trigger the recheck")
	assert.Equal(t, 2, requests, "exactly the turn's own 2 requests (tool call + follow-up), no recheck")
	assert.Contains(t, res.Text, "3 rows")
}

// TestMaybeGroundText_SkipsWhenNoTables: an empty workspace (no tables at
// all) must never loop on the recheck.
func TestMaybeGroundText_SkipsWhenNoTables(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		sseFinalTextResponse(t, w, "There are about 42 rows in some table.")
	}))
	defer srv.Close()

	handler := newTestHandlerNoTables(t)
	runner := NewRunner(handler, nil)
	var verifyingSeen bool
	_, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "how many rows roughly", "", nil, func(ev StreamEvent) {
		if ev.Kind == "verifying" {
			verifyingSeen = true
		}
	})
	require.NoError(t, err)
	assert.False(t, verifyingSeen, "no tables at all must skip the grounding recheck")
	assert.Equal(t, 1, requests, "only the turn's own single request, no recheck")
}

// TestMaybeGroundText_SkipsAfterWrite: a turn that ran a write tool
// (create_record) — even with no grounding tool — must never trigger the
// recheck, to avoid ever giving the model a second chance to re-invoke a
// write.
func TestMaybeGroundText_SkipsAfterWrite(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}
		if sawTool {
			sseFinalTextResponse(t, w, "Added the row; there are now 4 rows total.")
			return
		}
		sseToolCallResponse(t, w, "create_record", `{"table":"t","values":{"id":4}}`)
	}))
	defer srv.Close()

	handler := newTestHandlerWithTable(t)
	runner := NewRunner(handler, nil)
	var verifyingSeen bool
	res, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxFull,
	}, "add a row", "", nil, func(ev StreamEvent) {
		if ev.Kind == "verifying" {
			verifyingSeen = true
		}
	})
	require.NoError(t, err)
	assert.False(t, verifyingSeen, "a turn that already wrote data must never trigger the recheck")
	assert.Equal(t, 2, requests, "exactly the turn's own 2 requests, no recheck")
	assert.Contains(t, res.Text, "4 rows")
}

// TestMaybeGroundText_ChatStreamEmitsVerifyingEvent is a focused check that
// the "verifying" StreamEvent actually reaches the emitter (separate from
// the fires-once test above, which also asserts this, kept here as the
// single, minimal, explicitly-named acceptance test for the review's list).
func TestMaybeGroundText_ChatStreamEmitsVerifyingEvent(t *testing.T) {
	srv, _ := fakeSSEServer_UngroundedThenRunsQuery(t, "There are about 42 rows.", "There are 3 rows, verified.")
	defer srv.Close()

	handler := newTestHandlerWithTable(t)
	runner := NewRunner(handler, nil)
	var sawVerifying bool
	_, err := runner.ChatStream(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "how many rows roughly", "", nil, func(ev StreamEvent) {
		if ev.Kind == "verifying" {
			sawVerifying = true
		}
	})
	require.NoError(t, err)
	assert.True(t, sawVerifying)
}
