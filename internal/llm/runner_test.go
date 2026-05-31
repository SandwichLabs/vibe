package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDeps is an in-memory implementation of Deps for testing the agent loop
// without a real .vibe/DuckDB.
type fakeDeps struct {
	tables       []string
	created      []TableSpec
	forms        []FormDef
	queries      []QueryDef
	reports      []ReportDef
	rels         []RelationshipDef
	lastQuery    string
	queryResult  *QueryResult
	describeText string
}

func (f *fakeDeps) ListTables(context.Context) ([]string, error) { return f.tables, nil }
func (f *fakeDeps) DescribeTable(_ context.Context, _ string) (string, error) {
	return f.describeText, nil
}
func (f *fakeDeps) Query(_ context.Context, sql string) (*QueryResult, error) {
	f.lastQuery = sql
	return f.queryResult, nil
}
func (f *fakeDeps) CreateTable(_ context.Context, spec TableSpec) error {
	f.created = append(f.created, spec)
	return nil
}
func (f *fakeDeps) CreateForm(_ context.Context, def FormDef) error {
	f.forms = append(f.forms, def)
	return nil
}
func (f *fakeDeps) SaveQuery(_ context.Context, def QueryDef) error {
	f.queries = append(f.queries, def)
	return nil
}
func (f *fakeDeps) CreateReport(_ context.Context, def ReportDef) error {
	f.reports = append(f.reports, def)
	return nil
}
func (f *fakeDeps) AddRelationship(_ context.Context, def RelationshipDef) error {
	f.rels = append(f.rels, def)
	return nil
}

// chatRequest is the subset of the OpenAI chat-completions request we inspect.
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
			// First turn: instruct a tool call.
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
			// Second turn: final answer.
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

func TestChat_ToolCallingLoop_ListTables(t *testing.T) {
	srv := fakeLLMServer(t, "list_tables", "{}", "You have these tables: customers, orders.")
	defer srv.Close()

	deps := &fakeDeps{tables: []string{"customers", "orders"}}
	runner := NewRunner(deps, nil)

	res, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1",
		Model:    "local",
		Sandbox:  SandboxReadOnly,
	}, "What tables do I have?", "")

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Contains(t, res.Text, "customers")
	// The agent actually invoked the list_tables tool.
	require.Len(t, res.Steps, 1)
	assert.Equal(t, "list_tables", res.Steps[0].Tool)
	assert.Contains(t, res.Steps[0].Output, "customers")
}

func TestChat_CreateTableTool_GatedBySandbox(t *testing.T) {
	args := `{"name":"widgets","columns":[{"name":"id","type":"INTEGER","primary_key":true,"not_null":true}]}`

	t.Run("read-write allows create_table", func(t *testing.T) {
		srv := fakeLLMServer(t, "create_table", args, "Created the widgets table.")
		defer srv.Close()
		deps := &fakeDeps{}
		runner := NewRunner(deps, nil)
		res, err := runner.Chat(context.Background(), Config{
			Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadWrite,
		}, "Create a widgets table.", "")
		require.NoError(t, err)
		require.Len(t, deps.created, 1)
		assert.Equal(t, "widgets", deps.created[0].Name)
		assert.Contains(t, res.Text, "widgets")
	})

	t.Run("read-only hides create_table", func(t *testing.T) {
		// In read-only mode the create_table tool isn't registered. The model
		// "calls" it anyway; fantasy can't dispatch an unknown tool, so the
		// table is never created.
		srv := fakeLLMServer(t, "create_table", args, "I cannot create tables in read-only mode.")
		defer srv.Close()
		deps := &fakeDeps{}
		runner := NewRunner(deps, nil)
		_, _ = runner.Chat(context.Background(), Config{
			Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
		}, "Create a widgets table.", "")
		assert.Empty(t, deps.created, "create_table must not run under read-only sandbox")
	})
}

func TestChat_BuilderTools(t *testing.T) {
	cases := []struct {
		name, tool, args string
		check            func(t *testing.T, d *fakeDeps)
	}{
		{
			name: "create_form", tool: "create_form",
			args: `{"title":"Customer entry","table":"customers","archetype":"columnar","fields":[{"name":"name","caption":"Name","control_type":"text"}]}`,
			check: func(t *testing.T, d *fakeDeps) {
				require.Len(t, d.forms, 1)
				assert.Equal(t, "customers", d.forms[0].Table)
				assert.Equal(t, "columnar", d.forms[0].Archetype)
				require.Len(t, d.forms[0].Fields, 1)
			},
		},
		{
			name: "save_query", tool: "save_query",
			args: `{"title":"Top cities","sql":"SELECT city, count(*) FROM customers GROUP BY city","description":"counts"}`,
			check: func(t *testing.T, d *fakeDeps) {
				require.Len(t, d.queries, 1)
				assert.Equal(t, "Top cities", d.queries[0].Title)
			},
		},
		{
			name: "create_report", tool: "create_report",
			args: `{"title":"Customers by city","source":"SELECT city, count(*) c FROM customers GROUP BY city","group_by":["city"],"columns":[{"field":"city","caption":"City"}]}`,
			check: func(t *testing.T, d *fakeDeps) {
				require.Len(t, d.reports, 1)
				assert.Equal(t, []string{"city"}, d.reports[0].GroupBy)
				require.Len(t, d.reports[0].Columns, 1)
			},
		},
		{
			name: "add_relationship", tool: "add_relationship",
			args: `{"child_table":"orders","child_column":"customer_id","parent_table":"customers","parent_column":"id","on_delete":"CASCADE"}`,
			check: func(t *testing.T, d *fakeDeps) {
				require.Len(t, d.rels, 1)
				assert.Equal(t, "orders", d.rels[0].ChildTable)
				assert.Equal(t, "CASCADE", d.rels[0].OnDelete)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+" runs under full sandbox", func(t *testing.T) {
			srv := fakeLLMServer(t, tc.tool, tc.args, "Done.")
			defer srv.Close()
			deps := &fakeDeps{}
			_, err := NewRunner(deps, nil).Chat(context.Background(), Config{
				Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxFull,
			}, "build it", "")
			require.NoError(t, err)
			tc.check(t, deps)
		})
		t.Run(tc.name+" is hidden under read-only", func(t *testing.T) {
			srv := fakeLLMServer(t, tc.tool, tc.args, "I can't, read-only.")
			defer srv.Close()
			deps := &fakeDeps{}
			_, _ = NewRunner(deps, nil).Chat(context.Background(), Config{
				Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
			}, "build it", "")
			assert.Empty(t, deps.forms)
			assert.Empty(t, deps.queries)
			assert.Empty(t, deps.reports)
			assert.Empty(t, deps.rels)
		})
	}
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

	runner := NewRunner(&fakeDeps{}, nil)
	_, err := runner.Chat(context.Background(), Config{
		Endpoint: srv.URL + "/v1", Model: "local", Sandbox: SandboxReadOnly,
	}, "hello", "the tables screen (/tables)")
	require.NoError(t, err)
	assert.Contains(t, capturedPrompt, "tables screen", "pinned context must be sent to the model")
}

func TestChat_Errors(t *testing.T) {
	runner := NewRunner(&fakeDeps{}, nil)
	_, err := runner.Chat(context.Background(), Config{Endpoint: "http://localhost:8080/v1"}, "  ", "")
	assert.Error(t, err, "empty message should error")

	_, err = runner.Chat(context.Background(), Config{Endpoint: ""}, "hi", "")
	assert.Error(t, err, "missing endpoint should error")
}

func TestIsReadOnlySQL(t *testing.T) {
	readOnly := []string{"SELECT * FROM t", "  select 1", "WITH x AS (SELECT 1) SELECT * FROM x", "PRAGMA table_info('t')", "DESCRIBE t", "(SELECT 1)"}
	for _, s := range readOnly {
		assert.True(t, isReadOnlySQL(s), "should be read-only: %q", s)
	}
	mutating := []string{"INSERT INTO t VALUES (1)", "UPDATE t SET x=1", "DELETE FROM t", "DROP TABLE t", "CREATE TABLE t (id int)"}
	for _, s := range mutating {
		assert.False(t, isReadOnlySQL(s), "should be rejected: %q", s)
	}
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
	assert.Equal(t, MessageRoleFor("user"), string(msgs[0].Role))
	assert.Equal(t, "assistant", string(msgs[1].Role))
}

// MessageRoleFor is a tiny helper to keep the assertion readable.
func MessageRoleFor(role string) string {
	if role == "agent" {
		return "assistant"
	}
	return "user"
}

func TestSystemPromptFor_WriteAccessAwareness(t *testing.T) {
	full := systemPromptFor(SandboxFull)
	assert.Contains(t, full, "create_table")
	assert.Contains(t, full, "FULL WRITE ACCESS")
	assert.NotContains(t, full, "READ-ONLY access:")

	rw := systemPromptFor(SandboxReadWrite)
	assert.Contains(t, rw, "FULL WRITE ACCESS")

	// The write-capable prompt advertises all the builder tools.
	for _, tool := range []string{"create_table", "create_form", "save_query", "create_report", "add_relationship"} {
		assert.Contains(t, full, tool)
	}

	ro := systemPromptFor(SandboxReadOnly)
	assert.Contains(t, ro, "READ-ONLY access:")
	assert.NotContains(t, ro, "create_table:")
}

func TestWithPinned(t *testing.T) {
	assert.Equal(t, "hi", withPinned("hi", ""))
	assert.Equal(t, "hi", withPinned("hi", "   "))
	out := withPinned("count rows", "the tables screen")
	assert.Contains(t, out, "the tables screen")
	assert.Contains(t, out, "count rows")
	assert.Contains(t, out, "pinned")
}

func TestFormatResult(t *testing.T) {
	out := formatResult(&QueryResult{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "2"}, {"3", "4"}}})
	assert.True(t, strings.HasPrefix(out, "a | b"))
	assert.Contains(t, out, "1 | 2")
	assert.Equal(t, "(no results)", formatResult(nil))
}
