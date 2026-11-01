package agents

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/SandwichLabs/slaw/internal/skills"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFileSkillsHandler wires create_or_update_skill to a real FileStore, the
// only store whose revision checks the mock does not fake (#166).
func newFileSkillsHandler(t *testing.T) (*Handler, *skills.FileStore) {
	t.Helper()
	fs := skills.NewFileStore(t.TempDir())
	_, duckDB := skillsTestDBs(t)
	access := &mockAccess{duckDB: duckDB, sks: fs}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHandler(access, &mockEngine{}, &SandboxConfig{Mode: ModeFull, MaxQueryRows: 100}, logger), fs
}

func saveSkill(h *Handler, instructions, rev string) *Response {
	params := map[string]any{"name": "revenue-rules", "description": "Revenue rules", "instructions": instructions}
	if rev != "" {
		params["rev"] = rev
	}
	return h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "create_or_update_skill", Params: params})
}

func TestCreateOrUpdateSkill_RevRules(t *testing.T) {
	ctx := context.Background()
	h, fs := newFileSkillsHandler(t)

	// New name, no rev: creates.
	resp := saveSkill(h, "v1", "")
	require.Nil(t, resp.Error)
	res, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	rev1, _ := res["rev"].(string)
	require.NotEmpty(t, rev1)

	// A person edits it; a List in the same process refreshes last-seen.
	person := skills.Stored{Name: "revenue-rules", Description: "Revenue rules", Instructions: "person edit"}
	require.NoError(t, fs.Put(ctx, &person, skills.WithRev(rev1)))
	_, err := fs.List(ctx)
	require.NoError(t, err)

	// Existing skill, no rev: refused even after the List, and untouched.
	resp = saveSkill(h, "agent clobber", "")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already exists")
	assert.Contains(t, resp.Error.Message, "read_skill")
	got, err := fs.Get(ctx, "revenue-rules")
	require.NoError(t, err)
	assert.Equal(t, "person edit", got.Instructions)

	// Stale rev: conflict.
	resp = saveSkill(h, "agent stale", rev1)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "changed since")
	got, err = fs.Get(ctx, "revenue-rules")
	require.NoError(t, err)
	assert.Equal(t, "person edit", got.Instructions)

	// Current rev (as read_skill returns it): succeeds.
	resp = saveSkill(h, "agent merge", got.Rev)
	require.Nil(t, resp.Error)
	got, err = fs.Get(ctx, "revenue-rules")
	require.NoError(t, err)
	assert.Equal(t, "agent merge", got.Instructions)
}

func TestReadSkill_ReturnsRev(t *testing.T) {
	h, _ := newFileSkillsHandler(t)
	require.Nil(t, saveSkill(h, "v1", "").Error)
	resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "revenue-rules"}})
	require.Nil(t, resp.Error)
	res, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, res["rev"])
}
