package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
)

// Every tool an MCP client sees carries annotations that match its category.
func TestToolAnnotations_MatchTheCategory(t *testing.T) {
	tools := agents.ListToolsForMode(agents.ModeMCP)
	require.NotEmpty(t, tools)
	for _, td := range tools {
		a := toolDefToMCPTool(td).Annotations
		require.NotNil(t, a.ReadOnlyHint, td.Name)
		require.NotNil(t, a.DestructiveHint, td.Name)
		require.NotNil(t, a.IdempotentHint, td.Name)
		require.NotNil(t, a.OpenWorldHint, td.Name)
		switch td.Category {
		case "introspect":
			assert.True(t, *a.ReadOnlyHint, td.Name)
			assert.False(t, *a.DestructiveHint, td.Name)
			assert.True(t, *a.IdempotentHint, td.Name)
		case "content":
			assert.False(t, *a.ReadOnlyHint, td.Name)
			assert.False(t, *a.DestructiveHint, td.Name)
			assert.False(t, *a.OpenWorldHint, "content stays in the workspace folder: "+td.Name)
		case "data", "schema":
			assert.False(t, *a.ReadOnlyHint, td.Name)
			assert.True(t, *a.DestructiveHint, td.Name)
			assert.True(t, *a.OpenWorldHint, "a connection may be remote: "+td.Name)
		default:
			t.Fatalf("tool %s has category %q: decide its annotations in annotationsFor", td.Name, td.Category)
		}
	}
}

func TestToolAnnotations_WorkspaceLocalIntrospectIsNotOpenWorld(t *testing.T) {
	for _, name := range []string{"list_connections", "list_skills", "read_skill", "help_search", "list_notebooks", "get_artifact"} {
		td := agents.GetTool(name)
		require.NotNil(t, td, name)
		assert.False(t, *toolDefToMCPTool(*td).Annotations.OpenWorldHint, name)
	}
	for _, name := range []string{"introspect_schema", "describe_table"} {
		assert.True(t, *toolDefToMCPTool(*agents.GetTool(name)).Annotations.OpenWorldHint, name)
	}
}

func TestToolAnnotations_IdempotentOnlyWhereTrue(t *testing.T) {
	for name := range idempotentTools {
		td := agents.GetTool(name)
		require.NotNil(t, td, "idempotentTools names an unregistered tool: "+name)
		assert.NotEqual(t, "introspect", td.Category, name)
	}
	for _, name := range []string{"add_query_cell", "create_dashboard", "run_query", "bulk_insert", "create_record"} {
		assert.False(t, *toolDefToMCPTool(*agents.GetTool(name)).Annotations.IdempotentHint, name)
	}
	for name := range connectionReaders {
		require.NotNil(t, agents.GetTool(name), name)
	}
}

// refusingHandler answers every call with an approval-needed refusal that
// carries structured data.
type refusingHandler struct{}

func (refusingHandler) Handle(_ context.Context, req *agents.Request) *agents.Response {
	r := agents.NewErrorResponse(req.ID, agents.ErrApprovalNeeded, "valid and ready, not saved")
	r.Error.Data = map[string]any{"valid": true, "needs_approval": true}
	return r
}

// stdioRoundTrip sends the given JSON-RPC lines and returns the first n
// responses, decoded.
func stdioRoundTrip(t *testing.T, s *Server, lines []string, n int) []map[string]any {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.ServeStdio(ctx, inR, outW)
	}()
	go func() {
		for _, l := range lines {
			_, _ = io.WriteString(inW, l+"\n")
		}
	}()
	var out []map[string]any
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	got := make(chan struct{})
	go func() {
		defer close(got)
		for len(out) < n && sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				out = append(out, m)
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for MCP responses")
	}
	cancel()
	_ = inW.Close()
	_ = outW.Close()
	<-done
	return out
}

const (
	initLine  = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	notifLine = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
)

func TestInitialize_ReportsIdentityAndInstructions(t *testing.T) {
	calls := 0
	s := New(refusingHandler{}, slog.New(slog.DiscardHandler),
		WithServerInfo("vibe mcp serve", "v1.2.3"),
		WithInstructions(func(context.Context) string { calls++; return "orientation text" }))
	res := stdioRoundTrip(t, s, []string{initLine}, 1)
	result := asMap(t, res[0]["result"])
	info := asMap(t, result["serverInfo"])
	assert.Equal(t, "vibe mcp serve", info["name"])
	assert.Equal(t, "v1.2.3", info["version"])
	assert.Equal(t, "orientation text", result["instructions"])
	assert.Equal(t, 1, calls)
}

// A client on protocol 2026-07-28 never calls initialize; it calls
// server/discover, which must carry the same instructions.
func TestServerDiscover_CarriesInstructions(t *testing.T) {
	s := New(refusingHandler{}, slog.New(slog.DiscardHandler),
		WithServerInfo("vibe mcp serve", "v1.2.3"),
		WithInstructions(func(context.Context) string { return "orientation text" }))
	line := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	res := stdioRoundTrip(t, s, []string{line}, 1)
	result := asMap(t, res[0]["result"])
	assert.Equal(t, "orientation text", result["instructions"])
	info := asMap(t, asMap(t, result["_meta"])["io.modelcontextprotocol/serverInfo"])
	assert.Equal(t, "vibe mcp serve", info["name"])
}

func TestInitialize_DefaultsWithoutOptions(t *testing.T) {
	s := New(refusingHandler{}, slog.New(slog.DiscardHandler))
	res := stdioRoundTrip(t, s, []string{initLine}, 1)
	result := asMap(t, res[0]["result"])
	info := asMap(t, result["serverInfo"])
	assert.Equal(t, DefaultServerName, info["name"])
	assert.NotEqual(t, "1.0.0", info["version"])
	_, has := result["instructions"]
	assert.False(t, has)
}

func TestToolsList_CarriesAnnotationsAndRefusalCarriesStructuredData(t *testing.T) {
	s := New(refusingHandler{}, slog.New(slog.DiscardHandler))
	res := stdioRoundTrip(t, s, []string{
		initLine, notifLine,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_connections","arguments":{}}}`,
	}, 3)
	var list, call map[string]any
	for _, m := range res {
		switch m["id"] {
		case float64(2):
			list = asMap(t, m["result"])
		case float64(3):
			call = asMap(t, m["result"])
		}
	}
	require.NotNil(t, list)
	var sawReadOnly, sawDestructive bool
	for _, tl := range asList(t, list["tools"]) {
		tm := asMap(t, tl)
		ann := asMap(t, tm["annotations"])
		if tm["name"] == "list_connections" {
			assert.Equal(t, true, ann["readOnlyHint"])
			assert.Equal(t, false, ann["destructiveHint"])
			assert.Equal(t, false, ann["openWorldHint"])
			sawReadOnly = true
		}
		if tm["name"] == "run_query" {
			assert.Equal(t, true, ann["destructiveHint"])
			sawDestructive = true
		}
	}
	assert.True(t, sawReadOnly && sawDestructive)

	require.NotNil(t, call)
	assert.Equal(t, true, call["isError"])
	sc := asMap(t, call["structuredContent"])
	assert.Equal(t, true, sc["valid"])
	text, _ := asMap(t, asList(t, call["content"])[0])["text"].(string)
	assert.Contains(t, text, "valid and ready")
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "expected an object, got %T", v)
	return m
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	require.True(t, ok, "expected a list, got %T", v)
	return l
}
