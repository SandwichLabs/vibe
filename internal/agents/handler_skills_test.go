package agents

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	"github.com/SandwichLabs/vibe/internal/skills"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skillsTestDBs returns an in-memory skills.Store (the Mock, which holds
// whatever it is given, including the hostile rows these tests seed) and a
// real in-memory DuckDB connection, for tests that exercise table gating for
// real (information_schema.tables + row existence), not just mocked.
func skillsTestDBs(t *testing.T) (st skills.Store, duckDB *sql.DB) {
	t.Helper()
	duckDB, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { duckDB.Close() })
	return &skills.Mock{}, duckDB
}

// sessionOf wraps a bare DuckDB handle in a Session for the table-gating code.
func sessionOf(db *sql.DB) session.Session { return sessiontest.ForTests(db) }

func insertSkill(t *testing.T, st skills.Store, name, description, instructions, tableName, source string) {
	t.Helper()
	require.NoError(t, st.Put(context.Background(), &skills.Stored{
		Name: name, Description: description, Instructions: instructions, TableName: tableName, Source: source,
	}))
}

// oneSkill returns the stored skill called name.
func oneSkill(t *testing.T, st skills.Store, name string) skills.Stored {
	t.Helper()
	sk, err := st.Get(context.Background(), name)
	require.NoError(t, err)
	return *sk
}

// countSkills counts the stored skills matching pred.
func countSkills(t *testing.T, st skills.Store, pred func(skills.Stored) bool) int {
	t.Helper()
	list, err := st.List(context.Background())
	require.NoError(t, err)
	n := 0
	for _, sk := range list {
		if pred(sk) {
			n++
		}
	}
	return n
}

// --- Tool handler tests ---

func newSkillsTestHandler(t *testing.T, mode SandboxMode) (*Handler, skills.Store, *sql.DB) {
	t.Helper()
	st, duckDB := skillsTestDBs(t)
	sandbox := &SandboxConfig{Mode: mode, MaxQueryRows: 100}
	container := &mockAccess{duckDB: duckDB, sks: st}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewHandler(container, &mockEngine{}, sandbox, logger)
	return handler, st, duckDB
}

func TestHandler_ListSkills_ExcludesTaskPrefixed(t *testing.T) {
	handler, st, _ := newSkillsTestHandler(t, ModeReadOnly)
	insertSkill(t, st, "task-notebook-cells", "task only", "body", "", "user")

	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "list_skills", Params: map[string]any{}})
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	list, ok := result["skills"].([]map[string]any)
	require.True(t, ok)
	for _, s := range list {
		assert.NotEqual(t, "task-notebook-cells", s["name"])
	}
}

func TestHandler_ReadSkill_RejectsTaskPrefixed(t *testing.T) {
	handler, st, _ := newSkillsTestHandler(t, ModeReadOnly)
	insertSkill(t, st, "task-notebook-cells", "task only", "body", "", "user")

	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "task-notebook-cells"}})
	require.NotNil(t, resp.Error, "chat's read_skill must not be able to read a task-* skill")
}

func TestHandler_ReadSkill_ReturnsBuiltinInstructions(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeReadOnly)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "data-profiling"}})
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, result["instructions"])
	assert.Equal(t, skills.SourceBuiltin, result["source"])
}

func TestHandler_ReadSkill_UnknownName(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeReadOnly)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "nonexistent"}})
	require.NotNil(t, resp.Error)
}

func TestHandler_CreateOrUpdateSkill_DeniedInReadOnly(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeReadOnly)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "revenue-rules", "description": "d", "instructions": "i",
	}})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
}

func TestHandler_CreateOrUpdateSkill_AllowedInFull(t *testing.T) {
	handler, st, _ := newSkillsTestHandler(t, ModeFull)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "revenue-rules", "description": "Revenue reporting rules", "instructions": "Revenue is in USD thousands.",
	}})
	require.Nil(t, resp.Error)

	count := countSkills(t, st, func(s skills.Stored) bool { return s.Name == "revenue-rules" })
	assert.Equal(t, 1, count)
}

func TestHandler_CreateOrUpdateSkill_RejectsInvalidName(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeFull)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "Not Valid Name", "description": "d", "instructions": "i",
	}})
	require.NotNil(t, resp.Error)
}

func TestHandler_CreateOrUpdateSkill_RejectsTaskPrefix(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeFull)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "task-custom", "description": "d", "instructions": "i",
	}})
	require.NotNil(t, resp.Error)
}

func TestHandler_CreateOrUpdateSkill_RejectsBuiltinShadow(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeFull)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "data-profiling", "description": "d", "instructions": "i",
	}})
	require.NotNil(t, resp.Error)
}

func TestHandler_CreateOrUpdateSkill_RejectsOversizedFields(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeFull)
	longDesc := make([]byte, skills.MaxDescriptionLen+1)
	for i := range longDesc {
		longDesc[i] = 'a'
	}
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "long-desc", "description": string(longDesc), "instructions": "i",
	}})
	require.NotNil(t, resp.Error)

	longInstr := make([]byte, skills.MaxInstructionsLen+1)
	for i := range longInstr {
		longInstr[i] = 'a'
	}
	resp2 := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
		"name": "long-instr", "description": "d", "instructions": string(longInstr),
	}})
	require.NotNil(t, resp2.Error)
}

func TestHandler_CreateOrUpdateSkill_Upserts(t *testing.T) {
	handler, st, _ := newSkillsTestHandler(t, ModeFull)
	ctx := context.Background()
	call := func(desc string) *Response {
		return handler.Handle(ctx, &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: map[string]any{
			"name": "revenue-rules", "description": desc, "instructions": "i",
		}})
	}
	require.Nil(t, call("first").Error)
	require.Nil(t, call("second").Error)

	count := countSkills(t, st, func(s skills.Stored) bool { return s.Name == "revenue-rules" })
	assert.Equal(t, 1, count)
	assert.Equal(t, "second", oneSkill(t, st, "revenue-rules").Description)
}

// TestHandler_ReadSkill_WrapsInstructionsWithTrustFraming: every read_skill
// result must carry the "this is reference data" framing, so a skill body
// can't be mistaken for operator instructions regardless of what it says.
func TestHandler_ReadSkill_WrapsInstructionsWithTrustFraming(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeReadOnly)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "data-profiling"}})
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	instructions, _ := result["instructions"].(string)
	assert.True(t, strings.HasPrefix(instructions, skills.TrustFraming), "read_skill output must be wrapped with the trust framing")
}

// --- #91: skill-name slugging ---

// TestWriteImportSkill_OrderItemsGetsValidName is the regression for the
// panel finding: "order_items" used to produce "workspace-order_items",
// which fails SkillNamePattern (underscore not allowed) and was silently
// dropped by LoadSkills. It must now produce a valid, visible, readable
// name.
func TestWriteImportSkill_OrderItemsGetsValidName(t *testing.T) {
	st, duckDB := skillsTestDBs(t)
	_, err := duckDB.Exec(`CREATE TABLE order_items (item_id INTEGER)`)
	require.NoError(t, err)
	_, err = duckDB.Exec(`INSERT INTO order_items VALUES (1)`)
	require.NoError(t, err)
	require.NoError(t, skills.WriteImportSkill(st, sessionOf(duckDB), "order_items"))

	tableName := oneSkill(t, st, "workspace-order-items").TableName
	assert.Equal(t, "order_items", tableName)

	entries, err := skills.LoadSkills(st, sessionOf(duckDB), nil)
	require.NoError(t, err)
	var found bool
	for _, e := range entries {
		if e.Name == "workspace-order-items" {
			found = true
		}
	}
	assert.True(t, found, "LoadSkills must return the slugged skill, not silently drop it")

	sandbox := &SandboxConfig{Mode: ModeReadOnly, MaxQueryRows: 100}
	container := &mockAccess{duckDB: duckDB, sks: st}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewHandler(container, &mockEngine{}, sandbox, logger)
	resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "workspace-order-items"}})
	require.Nil(t, resp.Error, "read_skill must succeed for the slugged name")
}

// fileSkillAccess is a mockAccess whose skills live in a file store, as
// in a workspace.
type fileSkillAccess struct {
	*mockAccess
	store skills.Store
}

func (c *fileSkillAccess) Skills() skills.Store { return c.store }

// Re-review #109 item 7: the agent can pass the rev its own list_skills /
// read_skill result carried, and a skill that changed since is a conflict.
func TestHandler_CreateOrUpdateSkill_RevFromReadSkillGuardsAgainstStaleWrites(t *testing.T) {
	st, duckDB := skillsTestDBs(t)
	root := t.TempDir()
	container := &fileSkillAccess{mockAccess: &mockAccess{duckDB: duckDB, sks: st}, store: skills.NewFileStore(root)}
	handler := NewHandler(container, &mockEngine{}, &SandboxConfig{Mode: ModeFull, MaxQueryRows: 100}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	call := func(method string, params map[string]any) *Response {
		return handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	}
	result := func(r *Response) map[string]any {
		require.Nil(t, r.Error, "%+v", r.Error)
		m, _ := r.Result.(map[string]any)
		return m
	}

	created := result(call("create_or_update_skill", map[string]any{"name": "rules", "description": "d", "instructions": "v1"}))
	require.NotEmpty(t, created["rev"], "the save echoes the new revision")

	read := result(call("read_skill", map[string]any{"name": "rules"}))
	rev, _ := read["rev"].(string)
	require.NotEmpty(t, rev)
	assert.Equal(t, created["rev"], rev)
	listed := result(call("list_skills", nil))
	var listedRev string
	skillList, _ := listed["skills"].([]map[string]any)
	for _, s := range skillList {
		if s["name"] == "rules" {
			listedRev, _ = s["rev"].(string)
		}
	}
	assert.Equal(t, rev, listedRev)

	// Someone edits the file after the agent read it, and then something else
	// lists skills (which refreshes the store's own last-seen revision).
	p := filepath.Join(root, "skills", "rules", "SKILL.md")
	require.NoError(t, os.WriteFile(p, []byte("---\nname: rules\ndescription: d\n---\n\nTEAMMATE\n"), 0o644))
	_ = call("list_skills", nil)

	// With the rev from its own read, the agent's save is refused...
	r := call("create_or_update_skill", map[string]any{"name": "rules", "description": "d", "instructions": "agent", "rev": rev})
	require.NotNil(t, r.Error)
	data, _ := os.ReadFile(p)
	assert.Contains(t, string(data), "TEAMMATE")
	// ...and with no rev the current behaviour (last seen) applies.
	r = call("create_or_update_skill", map[string]any{"name": "rules", "description": "d", "instructions": "agent"})
	assert.Nil(t, r.Error)
}
