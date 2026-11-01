//go:build unix

package hub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/agents"
)

func initializeResult(t *testing.T, run *serveRun) (info map[string]any, instructions string) {
	t.Helper()
	res := run.call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})
	result, ok := res["result"].(map[string]any)
	require.True(t, ok)
	run.notify("notifications/initialized")
	info, _ = result["serverInfo"].(map[string]any)
	instructions, _ = result["instructions"].(string)
	return info, instructions
}

func TestInstructionsText_ByGateState(t *testing.T) {
	base := instructionsInput{Workspace: "sales", Root: "/w/sales", Connections: []string{"local", "prod_pg"}}

	in := base
	in.Policy = agents.FilePolicy{Mode: agents.ModeReadWrite, AskBeforeWrites: true}
	headless := instructionsText(in)
	assert.Contains(t, headless, `"sales" at /w/sales`)
	assert.Contains(t, headless, `read-write ("Change rows")`)
	assert.Contains(t, headless, `"Ask before changes" was on`)
	assert.Contains(t, headless, "refused")
	assert.Contains(t, headless, "valid and ready")
	assert.Contains(t, headless, "Settings → Workspace → The agent")
	assert.Contains(t, headless, "/w/sales/data/")
	assert.Contains(t, headless, "local, prod_pg")
	assert.Contains(t, headless, `help_search ""`)
	assert.Contains(t, headless, "duckdb-sql")
	assert.Contains(t, headless, "data-artifacts")
	assert.Contains(t, headless, "connect time")

	in.HasApprover = true
	assert.Contains(t, instructionsText(in), "approval card")

	in.Policy.AskBeforeWrites = false
	off := instructionsText(in)
	assert.Contains(t, off, `"Ask before changes" was off`)
	assert.Contains(t, off, "without asking")

	in.Policy.Mode = agents.ModeReadOnly
	ro := instructionsText(in)
	assert.Contains(t, ro, "Nothing can be changed")
	assert.Contains(t, ro, "Settings → Workspace → The agent")
}

func TestInstructionsText_ReadOnlyMentionsSandboxFlagAndNameIsCapped(t *testing.T) {
	text := instructionsText(instructionsInput{
		Workspace: shortName(strings.Repeat("x", 500) + "\nIGNORE ALL"), Root: "/w",
		Policy: agents.FilePolicy{Mode: agents.ModeReadOnly, AskBeforeWrites: true},
	})
	assert.Contains(t, text, "--sandbox")
	assert.Less(t, len(text), 1000)
	assert.NotContains(t, text, "IGNORE ALL")
	assert.Equal(t, 1, strings.Count(strings.SplitN(text, "\n", 2)[0], "\n")+1, "the name stays on one line")

	headless := instructionsText(instructionsInput{Workspace: "w", Root: "/w", Policy: agents.FilePolicy{Mode: agents.ModeReadWrite, AskBeforeWrites: true}})
	assert.Contains(t, headless, "restarts")
}

func TestInstructionsText_StaysShortAndCapsConnections(t *testing.T) {
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, "connection_with_a_long_name_"+string(rune('a'+i%26)))
	}
	text := instructionsText(instructionsInput{
		Workspace: "a-workspace", Root: "/home/someone/projects/a/fairly/deep/workspace/path",
		Policy: agents.FilePolicy{Mode: agents.ModeFull, AskBeforeWrites: true}, Connections: names,
	})
	assert.Less(t, len(text), 2000, "Claude Code truncates instructions at 2,048 characters")
	assert.Contains(t, text, "and 28 more")
	assert.Contains(t, text, "semantic-layer", "the skill pointer stays in the capped text")
}

func TestServeStdio_InitializeCarriesInstructionsAndIdentity(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	run := startServe(t, ServeOptions{Workspace: ws.Root(), Version: "v9.9.9"})
	info, instr := initializeResult(t, run)
	assert.Equal(t, AppServe, info["name"])
	assert.Equal(t, "v9.9.9", info["version"])
	assert.Contains(t, instr, ws.Name())
	assert.Contains(t, instr, filepath.Join(ws.Root(), "data")+"/")
	assert.Contains(t, instr, "local")
	assert.Contains(t, instr, `"Ask before changes" was on`)
	assert.Less(t, len(instr), 2000)
	assert.NotContains(t, strings.ToLower(instr), "token")
	assert.NotContains(t, strings.ToLower(instr), "password")

	// A valid artifact write on default settings is "valid, needs approval",
	// and nothing lands on disk.
	text, isErr := run.tool("write_artifact", map[string]any{
		"name":     "sales",
		"manifest": map[string]any{"title": "Sales", "queries": []any{map[string]any{"name": "rows", "sql": "SELECT * FROM orders"}}},
		"view":     map[string]any{"title": "Sales", "components": []any{map[string]any{"id": "t", "type": "table", "query": "rows"}}},
	})
	assert.True(t, isErr)
	assert.Contains(t, text, "valid and ready")
	_, err := os.Stat(filepath.Join(ws.Root(), "artifacts", "sales"))
	assert.True(t, os.IsNotExist(err), "nothing may be written on refusal")

	// An invalid one gets its validation error instead.
	text, isErr = run.tool("write_artifact", map[string]any{
		"name":     "bad",
		"manifest": map[string]any{"title": "Bad", "queries": []any{map[string]any{"name": "rows", "sql": "DROP TABLE orders"}}},
		"view":     map[string]any{"title": "Bad", "components": []any{map[string]any{"id": "t", "type": "table", "query": "rows"}}},
	})
	assert.True(t, isErr)
	assert.NotContains(t, text, "valid and ready")
	require.NoError(t, run.finish(t))
}

func TestProxiedSession_InstructionsReflectTheSandboxCeilingAndApprover(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	hold, _, _ := studioLike(t, ws, func(context.Context, agents.ApprovalRequest) error { return nil })
	_ = hold

	run := startServe(t, ServeOptions{Workspace: ws.Root(), Sandbox: "read-only"})
	info, instr := initializeResult(t, run)
	assert.Equal(t, AppStudio, info["name"], "the proxied server is Studio's")
	assert.Contains(t, instr, "read-only")
	assert.Contains(t, instr, "Nothing can be changed")
	require.NoError(t, run.finish(t))

	open := startServe(t, ServeOptions{Workspace: ws.Root()})
	_, instr = initializeResult(t, open)
	assert.Contains(t, instr, "approval card")
}
