package llm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"charm.land/fantasy"
	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// --- Test fixtures: a real *agents.Handler over an in-memory SQLite db, so
// tool dispatch (including sandbox gating) exercises the real production path
// rather than a mock. This is the same pattern internal/agents/agents_test.go
// uses for handler tests. ---

// mockContainer's duckDB field is optional: when unset, DuckDBDB() falls
// back to db (the existing SQLite-only test double every pre-#70 test in
// this package relies on); grounding_recheck_test.go's
// newRealDuckDBHandlerForLLM sets it to a real DuckDB connection, mirroring
// internal/agents' identical mockContainer split (see its sql_classify_test.go).
type mockContainer struct {
	db     *sql.DB
	duckDB *sql.DB
}

func (m *mockContainer) SQLiteDB() *sql.DB { return m.db }
func (m *mockContainer) DuckDBDB() *sql.DB {
	if m.duckDB != nil {
		return m.duckDB
	}
	return m.db
}

type mockEngine struct{}

func (m *mockEngine) Execute(_ context.Context, _ string, _ map[string]any) (*engine.ResultSet, error) {
	return &engine.ResultSet{
		Columns: []engine.Column{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}},
		Rows:    [][]any{{1, "test"}},
	}, nil
}

// newTestHandler builds a real agents.Handler over a fresh in-memory sqlite db
// seeded with the internal tables the tools touch, at the given sandbox mode.
func newTestHandler(t *testing.T, mode agents.SandboxMode) *agents.Handler {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE _vibe_pages (
			id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
			page_type TEXT NOT NULL DEFAULT 'markdown', sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, metadata TEXT NOT NULL DEFAULT '{}'
		);
		CREATE TABLE _vibe_queries (
			id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', sql TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT 'duckdb', parameters TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		CREATE TABLE _vibe_skills (
			name TEXT PRIMARY KEY, description TEXT NOT NULL DEFAULT '', instructions TEXT NOT NULL DEFAULT '',
			table_name TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT 'user',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		CREATE TABLE _vibe_notebooks (
			id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', cells TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO _vibe_pages (id, title, content, created_at, updated_at)
			VALUES ('p1', 'Sales dashboard', '# Sales', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO _vibe_skills (name, description, instructions, created_at, updated_at)
			VALUES ('sales-dashboard', 'Sales dashboard', '# Sales', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	`)
	require.NoError(t, err)

	sandbox := &agents.SandboxConfig{Mode: mode, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return agents.NewHandler(&mockContainer{db: db}, &mockEngine{}, sandbox, logger)
}

// --- Fake OpenAI-compatible LLM server driving the fantasy tool-calling loop ---

type chatRequest struct {
	Messages []struct {
		Role string `json:"role"`
	} `json:"messages"`
}

// fakeLLMServer emulates an OpenAI-compatible /chat/completions endpoint. On the
// first turn (no prior tool message) it asks the model to call the named tool;
// once it sees a tool result it returns a final text answer. This exercises the
// full fantasy tool-calling loop: prompt -> tool_call -> tool result -> answer.
func fakeLLMServer(t *testing.T, toolName, toolArgs, finalText string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		sawTool := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawTool = true
			}
		}

		w.Header().Set("Content-Type", "application/json")
		var resp map[string]any
		if !sawTool {
			resp = map[string]any{
				"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "local",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{{
							"id": "call_1", "type": "function",
							"function": map[string]any{"name": toolName, "arguments": toolArgs},
						}},
					},
					"finish_reason": "tool_calls",
				}},
				"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			}
		} else {
			resp = map[string]any{
				"id": "chatcmpl-2", "object": "chat.completion", "created": 2, "model": "local",
				"choices": []map[string]any{{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": finalText},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// --- Acceptance test (a): the runner's tool list equals the handler's
// ToolDef list by name (#63). ---

func TestBuildTools_MatchesAgentsRegistryByName(t *testing.T) {
	handler := newTestHandler(t, agents.ModeFull)
	runner := NewRunner(handler, nil)

	tools := runner.buildTools(&recorder{})
	got := make([]string, len(tools))
	for i, tool := range tools {
		got[i] = tool.Info().Name
	}

	want := make([]string, 0)
	for _, td := range agents.ListToolsForMode(agents.ModeChat) {
		want = append(want, td.Name)
	}

	assert.ElementsMatch(t, want, got, "runner tool list must be exactly the agents.Handler ToolDef list, filtered to chat-visible tools (task-only tools like write_cell/preview_query/need_more_info must never appear in chat)")
	assert.NotEmpty(t, got)
}

// --- Acceptance test (b): sandbox read-only denies a write tool identically
// via the runner path and the raw handler (MCP) path (#63). ---

func TestSandboxReadOnly_DeniesWriteTool_IdenticallyAcrossEntryPoints(t *testing.T) {
	args := `{"name":"widgets","columns":[{"name":"id","type":"INTEGER"}]}`

	// Direct handler path (what MCP does).
	directHandler := newTestHandler(t, agents.ModeReadOnly)
	directResp := directHandler.Handle(context.Background(), &agents.Request{
		JSONRPC: "2.0", ID: "1", Method: "create_table",
		Params: map[string]any{"name": "widgets", "columns": []any{map[string]any{"name": "id", "type": "INTEGER"}}},
	})
	require.NotNil(t, directResp.Error, "direct handler call must be denied under read-only")
	assert.Equal(t, agents.ErrPermission, directResp.Error.Code)

	// Runner (Studio chat) path: the model calls the same tool; the fantasy
	// tool wrapper dispatches through the identical Handler.
	srv := fakeLLMServer(t, "create_table", args, "I can't do that in read-only mode.")
	defer srv.Close()

	runnerHandler := newTestHandler(t, agents.ModeReadOnly)
	runner := NewRunner(runnerHandler, nil)
	res, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "Create a widgets table.", "")
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "create_table", res.Steps[0].Tool)
	assert.Contains(t, res.Steps[0].Output, "error:")
	assert.Contains(t, res.Steps[0].Output, directResp.Error.Message, "the runner path must surface the identical denial message as the direct handler path")
	// The sandbox check happens before dispatch (agents.Handler.Handle), so
	// under read-only the CREATE TABLE statement never reaches the database.
}

func TestChat_ToolCallingLoop_ListSkills(t *testing.T) {
	// introspect_schema/describe_table query DuckDB's information_schema,
	// which the sqlite-backed test double can't satisfy (see
	// internal/agents/agents_test.go's identical caveat); list_skills only
	// touches the SQLite-side _vibe_skills table, so it exercises the full
	// tool-calling loop end to end.
	srv := fakeLLMServer(t, "list_skills", "{}", "You have a Sales dashboard skill.")
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeReadOnly)
	runner := NewRunner(handler, nil)

	res, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1",
		Model:    "local",
		Sandbox:  SandboxReadOnly,
	}, "What skills do I have?", "")

	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "list_skills", res.Steps[0].Tool)
	assert.Contains(t, res.Steps[0].Output, "sales-dashboard")
	assert.Contains(t, res.Text, "Sales dashboard")
}

func TestChat_ReadSkillTool_ReadOnly(t *testing.T) {
	srv := fakeLLMServer(t, "read_skill", `{"name":"sales-dashboard"}`, "Here is the skill.")
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeReadOnly)
	res, err := NewRunner(handler, nil).Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "Show me the Sales dashboard skill.", "")

	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "read_skill", res.Steps[0].Tool)
	assert.Contains(t, res.Steps[0].Output, "Sales")
}

func TestChat_CreateTableTool_AllowedUnderFull(t *testing.T) {
	args := `{"name":"widgets","columns":[{"name":"id","type":"INTEGER"}]}`
	srv := fakeLLMServer(t, "create_table", args, "Created the widgets table.")
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeFull)
	runner := NewRunner(handler, nil)
	res, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxFull,
	}, "Create a widgets table.", "")
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.NotContains(t, res.Steps[0].Output, "error:")
	assert.Contains(t, res.Steps[0].Output, "widgets")
	assert.False(t, res.Steps[0].IsError)
}

// TestToolResultOutcome is the review fix for #9: a denied/failed tool call
// surfaces to OnToolResult as fantasy.ToolResultOutputContentError, not
// ...Text (fantasy's agent.go rewraps an IsError ToolResponse into the Error
// variant before OnToolResult ever sees it). Checking only the Text variant
// — the original bug — left Output empty with no error flag, so the panel
// rendered a plain success chip for a call that was actually denied.
func TestToolResultOutcome(t *testing.T) {
	out, isErr := toolResultOutcome(fantasy.ToolResultOutputContentText{Text: "4 rows"})
	assert.Equal(t, "4 rows", out)
	assert.False(t, isErr)

	out, isErr = toolResultOutcome(fantasy.ToolResultOutputContentError{Error: errors.New(`method "create_table" is not allowed in read-write mode`)})
	assert.Equal(t, `method "create_table" is not allowed in read-write mode`, out)
	assert.True(t, isErr)

	// A nil Error inside the Error variant (shouldn't happen, but don't
	// panic) still reports isError with an empty message.
	out, isErr = toolResultOutcome(fantasy.ToolResultOutputContentError{})
	assert.Equal(t, "", out)
	assert.True(t, isErr)
}

func TestChat_ProposeSchemaTool_AvailableReadOnlyAndNeverMutates(t *testing.T) {
	args := `{"tables":[{"name":"customers","columns":[{"name":"id","type":"ID","primary_key":true},{"name":"name","type":"Text"}]},{"name":"service_calls","columns":[{"name":"id","type":"ID","primary_key":true},{"name":"customer_id","type":"Link"}]}],"relationships":[{"child_table":"service_calls","child_column":"customer_id","parent_table":"customers","parent_column":"id"}],"note":"Customers and their service calls"}`

	srv := fakeLLMServer(t, "propose_schema", args, "Here's a draft — review it on the diagram.")
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeReadOnly)
	res, err := NewRunner(handler, nil).Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "I run an HVAC company, design my database", "")
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "propose_schema", res.Steps[0].Tool)
	// The step carries the full draft JSON as input — logged/inspectable,
	// though the panel renders the tool's OUTPUT (the A2UI page below), not
	// this input.
	assert.Contains(t, res.Steps[0].Input, `"service_calls"`)
	assert.Contains(t, res.Steps[0].Input, `"child_column":"customer_id"`)
	assert.NotContains(t, res.Steps[0].Output, "error:")
	// The step's output is now a rendered A2UI page (present_data-style
	// inline rendering, not a diagram): a heading/table per proposed table.
	assert.Contains(t, res.Steps[0].Output, `"customers"`)
	assert.Contains(t, res.Steps[0].Output, `"service_calls"`)
	assert.Contains(t, res.Steps[0].Output, `"type":"table"`)
}

func TestChat_ProposeSchemaTool_RejectsEmptyDraft(t *testing.T) {
	srv := fakeLLMServer(t, "propose_schema", `{"tables":[]}`, "ok")
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeReadOnly)
	res, err := NewRunner(handler, nil).Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "propose nothing", "")
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Contains(t, res.Steps[0].Output, "error:")
}

func TestChat_PinnedContextPrefixed(t *testing.T) {
	var capturedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedPrompt = string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "x", "object": "chat.completion", "created": 1, "model": "local",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeReadOnly)
	runner := NewRunner(handler, nil)
	_, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "hello", "the tables screen (/tables)")
	require.NoError(t, err)
	assert.Contains(t, capturedPrompt, "tables screen", "pinned context must be sent to the model")
}

// TestChat_SkillsBlockInSystemPrompt (#62): the live system prompt sent to
// the model includes the <skills> block built from list_skills — built-ins
// unconditionally, and NOT a task-* skill even if one existed in storage.
func TestChat_SkillsBlockInSystemPrompt(t *testing.T) {
	var capturedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedPrompt = string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "x", "object": "chat.completion", "created": 1, "model": "local",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	handler := newTestHandler(t, agents.ModeReadOnly)
	runner := NewRunner(handler, nil)
	_, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "hello", "")
	require.NoError(t, err)
	assert.Contains(t, capturedPrompt, "<skills>")
	assert.Contains(t, capturedPrompt, "data-profiling")
	assert.Contains(t, capturedPrompt, "trustworthy-numbers")
	assert.Contains(t, capturedPrompt, "duckdb-sql")
}

func TestSkillsBlockFor_DirectCall(t *testing.T) {
	handler := newTestHandler(t, agents.ModeReadOnly)
	runner := NewRunner(handler, nil)
	block := runner.skillsBlockFor(context.Background())
	assert.Contains(t, block, "data-profiling")
}

func TestChat_Errors(t *testing.T) {
	handler := newTestHandler(t, agents.ModeReadOnly)
	runner := NewRunner(handler, nil)
	_, err := runner.Chat(context.Background(), Config{Endpoint: "http://localhost:8080/v1"}, "  ", "")
	assert.Error(t, err, "empty message should error")

	_, err = runner.Chat(context.Background(), Config{Endpoint: ""}, "hi", "")
	assert.Error(t, err, "missing endpoint should error")
}

func TestBuildMessages_MultiTurn(t *testing.T) {
	history := []Turn{
		{Role: "user", Text: "hi"},
		{Role: "agent", Text: "hello, how can I help?"},
		{Role: "user", Text: ""}, // blank turns are skipped
		{Role: "agent", Text: "  "},
	}
	msgs := buildMessages(history)
	require.Len(t, msgs, 2, "blank turns should be dropped")
	assert.Equal(t, "user", string(msgs[0].Role))
	assert.Equal(t, "assistant", string(msgs[1].Role))
}

func TestLanguageModel_Providers(t *testing.T) {
	handler := newTestHandler(t, agents.ModeReadOnly)
	r := NewRunner(handler, nil)
	ctx := context.Background()

	t.Run("local builds from endpoint", func(t *testing.T) {
		m, err := r.languageModel(ctx, Config{Provider: ProviderLocal, Endpoint: "http://localhost:8080/v1"})
		require.NoError(t, err)
		assert.NotNil(t, m)
	})

	t.Run("local requires an endpoint", func(t *testing.T) {
		_, err := r.languageModel(ctx, Config{Provider: ProviderLocal, Endpoint: ""})
		assert.Error(t, err)
	})

	t.Run("anthropic builds with a key", func(t *testing.T) {
		m, err := r.languageModel(ctx, Config{Provider: ProviderAnthropic, APIKey: "sk-ant-x"})
		require.NoError(t, err)
		assert.NotNil(t, m)
	})

	t.Run("anthropic without key errors", func(t *testing.T) {
		_, err := r.languageModel(ctx, Config{Provider: ProviderAnthropic})
		assert.Error(t, err)
	})

	t.Run("gemini builds with a key", func(t *testing.T) {
		m, err := r.languageModel(ctx, Config{Provider: ProviderGemini, APIKey: "g-x"})
		require.NoError(t, err)
		assert.NotNil(t, m)
	})

	t.Run("gemini without key errors", func(t *testing.T) {
		_, err := r.languageModel(ctx, Config{Provider: ProviderGemini})
		assert.Error(t, err)
	})

	t.Run("unknown provider errors", func(t *testing.T) {
		_, err := r.languageModel(ctx, Config{Provider: "bogus"})
		assert.Error(t, err)
	})

	t.Run("empty provider defaults to local", func(t *testing.T) {
		m, err := r.languageModel(ctx, Config{Endpoint: "http://localhost:8080/v1"})
		require.NoError(t, err)
		assert.NotNil(t, m)
	})
}

func TestDefaultModelFor(t *testing.T) {
	assert.Equal(t, "local", defaultModelFor(ProviderLocal))
	assert.Contains(t, defaultModelFor(ProviderAnthropic), "claude")
	assert.Contains(t, defaultModelFor(ProviderGemini), "gemini")
}

func TestSystemPromptFor_WriteAccessAwareness(t *testing.T) {
	full := systemPromptFor(SandboxFull, false, "")
	assert.Contains(t, full, "create_table")
	assert.Contains(t, full, "FULL WRITE ACCESS")
	assert.NotContains(t, full, "READ-ONLY access:")
	assert.NotContains(t, full, "READ-WRITE access to DATA ONLY")
	assert.NotContains(t, full, "NOT PERMITTED", "every tool must be permitted under full sandbox")

	rw := systemPromptFor(SandboxReadWrite, false, "")
	// Under the unified agents.SandboxConfig, schema/page categories (e.g.
	// create_table) require full, not just read-write — a deliberate
	// tightening from the old runner-only gate so Studio chat and MCP agree.
	assert.Contains(t, rw, "create_table: Create a new table in the container [NOT PERMITTED")
	// Prompt honesty (review fix): read-write must not claim "FULL WRITE
	// ACCESS" when schema/page tools are actually denied — it should
	// describe exactly what it can do (data reads/writes) and can't
	// (schema/pages).
	assert.NotContains(t, rw, "FULL WRITE ACCESS")
	assert.Contains(t, rw, "READ-WRITE access to DATA ONLY")
	assert.Contains(t, rw, "CANNOT change the schema")
	// run_query itself is never blanket-denied — its gating is per-statement,
	// documented in the tool's own description below.
	assert.False(t, runQueryLineDenied(rw), "run_query must never be flagged NOT PERMITTED — gating is per-statement")

	ro := systemPromptFor(SandboxReadOnly, false, "")
	assert.Contains(t, ro, "READ-ONLY access:")
	assert.Contains(t, ro, "create_table: Create a new table in the container [NOT PERMITTED")
	assert.NotContains(t, ro, "FULL WRITE ACCESS")
	assert.NotContains(t, ro, "READ-WRITE access to DATA ONLY")

	// propose_schema and the friendly-type vocabulary are advertised in every
	// sandbox — drafting never mutates. run_query is likewise never flagged
	// NOT PERMITTED anywhere — read-only can still SELECT through it.
	for _, prompt := range []string{full, rw, ro} {
		assert.Contains(t, prompt, "propose_schema")
		assert.Contains(t, prompt, "SCHEMA DESIGN")
		assert.Contains(t, prompt, "Link (a reference to another table's ID")
		assert.NotContains(t, prompt, "propose_schema [NOT PERMITTED", "propose_schema must be permitted in every sandbox")
		assert.False(t, runQueryLineDenied(prompt), "run_query must never be flagged NOT PERMITTED — gating is per-statement")
	}
}

func TestSystemPromptFor_AskBeforeWrites(t *testing.T) {
	const askSentence = "Every change you make"

	askOn := systemPromptFor(SandboxReadWrite, true, "")
	assert.Contains(t, askOn, askSentence)

	askOff := systemPromptFor(SandboxReadWrite, false, "")
	assert.NotContains(t, askOff, askSentence)

	// Read-only never writes anything, so the approval sentence would be
	// noise even with ask on.
	roAskOn := systemPromptFor(SandboxReadOnly, true, "")
	assert.NotContains(t, roAskOn, askSentence)
}

// runQueryLineDenied reports whether the "- run_query: ..." line in a
// system prompt carries the "[NOT PERMITTED" suffix.
func runQueryLineDenied(prompt string) bool {
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "- run_query:") {
			return strings.Contains(line, "[NOT PERMITTED")
		}
	}
	return false
}

func TestWithPinned(t *testing.T) {
	assert.Equal(t, "hi", withPinned("hi", ""))
	assert.Equal(t, "hi", withPinned("hi", "   "))
	out := withPinned("count rows", "the tables screen")
	assert.Contains(t, out, "the tables screen")
	assert.Contains(t, out, "count rows")
	assert.Contains(t, out, "pinned")
}

// TestToolInfo_ArrayParamsCarryItems guards against the Gemini
// function-calling rejection of a bare {"type":"array"}: every array-typed
// param across the shared registry must render a concrete "items" schema.
func TestToolInfo_ArrayParamsCarryItems(t *testing.T) {
	for _, td := range agents.ListTools() {
		info := ToolInfo(td)
		for _, p := range td.Params {
			if p.Type != "array" {
				continue
			}
			prop, ok := info.Parameters[p.Name].(map[string]any)
			require.Truef(t, ok, "%s.%s: expected a schema object", td.Name, p.Name)
			assert.Containsf(t, prop, "items", "%s.%s: array param must declare items", td.Name, p.Name)
		}
	}
}
