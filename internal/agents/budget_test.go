package agents

import (
	"database/sql"
	"encoding/json"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	"log/slog"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRealDuckDBHandlerMaxRows is newRealDuckDBHandler (sql_classify_test.go)
// with a caller-chosen MaxQueryRows, so the row-budget truncation test can
// exercise budgetRows itself rather than being pre-cut by the (unrelated)
// MaxQueryRows cap first.
func newRealDuckDBHandlerMaxRows(t *testing.T, maxRows int) *Handler {
	t.Helper()
	duckDB, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = duckDB.Close() })

	sandbox := &SandboxConfig{Mode: ModeReadOnly, MaxQueryRows: maxRows}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	container := &mockAccess{duckDB: duckDB}
	eng := engine.NewManager(sessiontest.ForTests(duckDB), engine.WithLogger(logger))
	return NewHandler(container, eng, sandbox, logger)
}

func TestBudgetRows_UnderBudgetUnchanged(t *testing.T) {
	rows := [][]any{{float64(1), "Ada"}, {float64(2), "Bob"}}
	out, truncated := budgetRows(rows)
	assert.Equal(t, rows, out)
	assert.Nil(t, truncated)
}

func TestBudgetRows_EmptyUnchanged(t *testing.T) {
	out, truncated := budgetRows(nil)
	assert.Nil(t, out)
	assert.Nil(t, truncated)
}

func TestBudgetRows_CutsOnWholeRowBoundary(t *testing.T) {
	var rows [][]any
	for i := 0; i < 2000; i++ {
		rows = append(rows, []any{float64(i), strings.Repeat("x", 50)})
	}
	out, truncated := budgetRows(rows)
	require.NotNil(t, truncated)
	assert.Less(t, len(out), len(rows))
	assert.Equal(t, len(out), truncated["shown"])
	assert.Equal(t, len(rows), truncated["total"])
	assert.NotEmpty(t, truncated["note"])

	// The result must still be valid, budget-respecting JSON once wrapped
	// the way handleRunQuery wraps it.
	env := map[string]any{"columns": []string{"id", "val"}, "rows": out, "truncated": truncated}
	b, err := json.Marshal(env)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(b), RowResultBudgetChars+wrapperOverhead+1024)

	var roundtrip map[string]any
	require.NoError(t, json.Unmarshal(b, &roundtrip))
}

func TestBudgetRows_SingleOversizedRowIsClippedNotDropped(t *testing.T) {
	huge := strings.Repeat("y", RowResultBudgetChars*2)
	rows := [][]any{{float64(1), huge}}
	out, truncated := budgetRows(rows)
	require.NotNil(t, truncated)
	require.Len(t, out, 1, "an oversized single row must still be emitted, not dropped")
	assert.Equal(t, 1, truncated["shown"])
	assert.Equal(t, 1, truncated["total"])

	clippedVal, ok := out[0][1].(string)
	require.True(t, ok)
	assert.Less(t, len(clippedVal), len(huge))
	assert.Contains(t, clippedVal, rowBudgetClipSuffix)

	b, err := json.Marshal(map[string]any{"rows": out, "truncated": truncated})
	require.NoError(t, err)
	var roundtrip map[string]any
	require.NoError(t, json.Unmarshal(b, &roundtrip))
}

// TestHandleRunQuery_LargeResultIsTruncated is the handler-level truncation
// test with a real DuckDB connection (#70): range(100000) is exactly the
// live-QA reproduction case from the issue, run through the real
// Handler -> engine.Manager -> DuckDB path so the wiring in handleRunQuery
// (not just budgetRows in isolation) is proven.
func TestHandleRunQuery_LargeResultIsTruncated(t *testing.T) {
	handler := newRealDuckDBHandlerMaxRows(t, 0) // 0 = unlimited MaxQueryRows, so only the row budget cuts it
	resp := runQuery(handler, "SELECT * FROM range(100000) AS t(i)")
	require.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	truncated, ok := result["truncated"].(map[string]any)
	require.True(t, ok, "a 100000-row result must be truncated")
	assert.Equal(t, 100000, truncated["total"])
	rows, ok := result["rows"].([][]any)
	require.True(t, ok)
	assert.Less(t, len(rows), 100000)

	// Response must stay valid JSON end to end (MCP/chat both marshal
	// resp.Result directly).
	b, err := json.Marshal(result)
	require.NoError(t, err)
	var roundtrip map[string]any
	require.NoError(t, json.Unmarshal(b, &roundtrip))
}

func TestHandleRunQuery_SmallResultNotTruncated(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQuery(handler, "SELECT * FROM t")
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	_, hasTruncated := result["truncated"]
	assert.False(t, hasTruncated)
}

// TestSafeTruncate_NeverSplitsARune (#70 review round 2): clipOversizedRow's
// binary search lands on an arbitrary byte length — this must never split a
// multi-byte UTF-8 rune, which would otherwise corrupt the string (and, via
// json.Marshal, potentially emit an invalid escape or replacement char).
func TestSafeTruncate_NeverSplitsARune(t *testing.T) {
	s := "héllo wörld 🎉 done"
	for n := 0; n <= len(s)+2; n++ {
		out := SafeTruncate(s, n)
		assert.True(t, utf8.ValidString(out), "SafeTruncate(%q, %d) = %q is not valid UTF-8", s, n, out)
		assert.LessOrEqual(t, len(out), n, "SafeTruncate must never exceed n bytes")
	}
	assert.Equal(t, "", SafeTruncate(s, 0))
	assert.Equal(t, s, SafeTruncate(s, len(s)+10))
}

// TestClipOversizedRow_MultiByteStringsStayValidUTF8 exercises the real
// clipping path (not just SafeTruncate in isolation) with a multi-byte
// string long enough to force clipping, confirming the clipped JSON value
// is still valid UTF-8/JSON end to end.
func TestClipOversizedRow_MultiByteStringsStayValidUTF8(t *testing.T) {
	huge := strings.Repeat("héllo wörld 🎉 ", 2000) // multi-byte runes throughout
	rows := [][]any{{float64(1), huge}}
	out, truncated := budgetRows(rows)
	require.NotNil(t, truncated)
	require.Len(t, out, 1)

	b, err := json.Marshal(map[string]any{"rows": out, "truncated": truncated})
	require.NoError(t, err)
	assert.True(t, utf8.Valid(b), "clipped row JSON must be valid UTF-8")

	var roundtrip map[string]any
	require.NoError(t, json.Unmarshal(b, &roundtrip))
}
