package cmd

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// agentCall runs one tool exactly as an MCP client or the chat agent would:
// through the agents handler over the workspace, at a tier, with no approver.
func agentCall(t *testing.T, root string, tier agents.SandboxMode, method string, params map[string]any) *agents.Response {
	t.Helper()
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close()
	h := agents.NewHandler(workspace.NewAgentAccess(ws, nil), nil,
		&agents.SandboxConfig{Mode: tier, MaxQueryRows: 100}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h.Handle(context.Background(), &agents.Request{JSONRPC: "2.0", ID: "1", Method: method, Params: params})
}

func proposalWorkspace(t *testing.T) string {
	t.Helper()
	root := metricWorkspace(t)
	require.NoError(t, os.Remove(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml")))
	return root
}

func proposeNetRevenue(t *testing.T, root string, extra map[string]any) string {
	t.Helper()
	params := map[string]any{
		"kind": "metric", "name": "net_revenue", "rationale": "Refunds are excluded, per finance.",
		"definition": map[string]any{"query": "net_revenue_base", "measure": "sum(amount)", "dimensions": []any{"region", "month"}},
	}
	for k, v := range extra {
		params[k] = v
	}
	resp := agentCall(t, root, agents.ModeReadWrite, "propose_definition", params)
	require.Nil(t, resp.Error, "%+v", resp.Error)
	res, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	id, _ := res["id"].(string)
	require.NotEmpty(t, id)
	return id
}

// The exit criterion: an agent proposes a metric, a person accepts it with the
// CLI, and `vibe query` runs it.
func TestSemanticsProposals_AgentProposesPersonAcceptsQueryRuns(t *testing.T) {
	root := proposalWorkspace(t)
	id := proposeNetRevenue(t, root, nil)

	// proposing wrote the machine-local inbox and nothing under semantics/
	_, err := os.Stat(filepath.Join(root, ".vibe-local", "proposals", id+".yaml"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.True(t, os.IsNotExist(err))
	_, _, err = runVibe("query", "--workspace", root, "SELECT * FROM metric('net_revenue')")
	require.Error(t, err, "the metric does not exist until it is accepted")

	out, stderr, err := runVibe("semantics", "proposals", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Regexp(t, id+`\s+metric\s+net_revenue\s+open\s+agent\s+Refunds are excluded`, out)

	_, stderr, err = runVibe("semantics", "list", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "1 proposed definition(s) waiting")

	out, stderr, err = runVibe("semantics", "show-proposal", id, "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "semantics/metrics/net_revenue.yaml is new.")
	assert.Contains(t, out, "+ measure: sum(amount)")
	assert.Contains(t, out, "Refunds are excluded")

	// a --rev that is not the version on screen is refused, and nothing is written
	_, _, err = runVibe("semantics", "accept", id, "--rev", "0000", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed again since you looked")
	_, err = os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.True(t, os.IsNotExist(err))

	// plain accept looks right then and shows what it is writing
	out, stderr, err = runVibe("semantics", "accept", id, "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "+ measure: sum(amount)")
	assert.Regexp(t, `(?m)^Rev\s+\S+`, stderr)
	assert.Contains(t, out, "Accepted "+id)
	written, err := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(written), "measure: sum(amount)")

	out, stderr, err = runVibe("query", "--workspace", root, "--format", "csv", "SELECT * FROM metric('net_revenue', by => 'region') ORDER BY region")
	require.NoError(t, err, stderr)
	assert.Equal(t, "region,net_revenue\neast,30\nwest,5\n", out)

	// decided proposals leave the default list and stay in --all
	out, _, err = runVibe("semantics", "proposals", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "No proposals are waiting")
	out, _, err = runVibe("semantics", "proposals", "--all", "--json", "--workspace", root)
	require.NoError(t, err)
	var all []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &all))
	require.Len(t, all, 1)
	assert.Equal(t, "accepted", all[0]["status"])

	_, _, err = runVibe("semantics", "accept", id, "--workspace", root)
	require.Error(t, err, "a decided proposal cannot be accepted again")
}

func TestSemanticsProposals_StaleNeedsOverwrite(t *testing.T) {
	root := metricWorkspace(t) // net_revenue already exists, hand-written
	cur := agentCall(t, root, agents.ModeReadOnly, "get_definition", map[string]any{"kind": "metric", "name": "net_revenue"})
	require.Nil(t, cur.Error)
	rev, _ := cur.Result.(map[string]any)["rev"].(string)
	require.NotEmpty(t, rev)

	id := proposeNetRevenue(t, root, map[string]any{
		"replaces_rev": rev,
		"definition":   map[string]any{"query": "net_revenue_base", "measure": "sum(amount) FILTER (WHERE amount > 1)", "dimensions": []any{"region"}},
	})
	// the person edits the file after the proposal
	seedMetricFile(t, root, "net_revenue", cliMetric+"description: edited by hand\n")

	out, stderr, err := runVibe("semantics", "show-proposal", id, "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "WARNING: semantics/metrics/net_revenue.yaml changed since this was proposed")
	assert.Contains(t, out, "- description: edited by hand")
	assert.Contains(t, out, "+ measure: sum(amount) FILTER (WHERE amount > 1)")

	_, _, err = runVibe("semantics", "accept", id, "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--overwrite")
	still, _ := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.Contains(t, string(still), "edited by hand", "nothing was written")

	// --overwrite alone refuses and says which version it would accept
	_, _, err = runVibe("semantics", "accept", id, "--overwrite", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--overwrite needs --rev")
	// a rev that is not the one on screen now is refused even with --overwrite
	_, _, err = runVibe("semantics", "accept", id, "--overwrite", "--rev", "0000", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed again since you looked")
	revM := regexp.MustCompile(`(?m)^Rev\s+(\S+)`).FindStringSubmatch(out)
	require.Len(t, revM, 2)
	assert.Contains(t, out, "vibe semantics accept "+id+" --overwrite --rev "+revM[1])
	assert.Contains(t, out, "comments")
	_, stderr, err = runVibe("semantics", "accept", id, "--overwrite", "--rev", revM[1], "--workspace", root)
	require.NoError(t, err, stderr)
	now, _ := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.Contains(t, string(now), "FILTER (WHERE amount > 1)")
	assert.NotContains(t, string(now), "edited by hand")
}

func TestSemanticsProposals_RejectAndGlossary(t *testing.T) {
	root := proposalWorkspace(t)
	id := proposeNetRevenue(t, root, nil)
	out, stderr, err := runVibe("semantics", "reject", id, "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Nothing in semantics/ changed")
	_, err = os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.True(t, os.IsNotExist(err))

	resp := agentCall(t, root, agents.ModeFull, "propose_definition", map[string]any{
		"kind": "glossary", "rationale": "Used both ways in the thread.",
		"definition": map[string]any{"term": "Churn", "text": "Customers who cancelled in the period."},
	})
	require.Nil(t, resp.Error, "%+v", resp.Error)
	gid, _ := resp.Result.(map[string]any)["id"].(string)
	_, stderr, err = runVibe("semantics", "accept", gid, "--workspace", root)
	require.NoError(t, err, stderr)
	g, err := os.ReadFile(filepath.Join(root, "semantics", "glossary.md"))
	require.NoError(t, err)
	assert.Equal(t, "## Churn\n\nCustomers who cancelled in the period.\n", string(g))
}

func TestSemanticsProposals_IDsAreValidatedEverywhere(t *testing.T) {
	root := proposalWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.yaml"), []byte("x: 1\n"), 0o600))
	for _, id := range []string{"../../secret", "..%2f..%2fsecret", "/etc/passwd", strings.Repeat("a", 300), ""} {
		for _, verb := range []string{"show-proposal", "accept", "reject"} {
			_, _, err := runVibe("semantics", verb, id, "--workspace", root)
			require.Error(t, err, "%s %q", verb, id)
		}
	}
	_, err := os.Stat(filepath.Join(root, "secret.yaml"))
	assert.NoError(t, err)
}

func TestSemanticsProposals_NeverInABundle(t *testing.T) {
	root := proposalWorkspace(t)
	id := proposeNetRevenue(t, root, nil)
	out := filepath.Join(t.TempDir(), "all.vibe")
	_, stderr, err := runVibe("export", "bundle", "--workspace", root, "--output", out)
	if err != nil {
		t.Skipf("bundle export is not available in this build: %v %s", err, stderr)
	}
	zr, err := zip.OpenReader(out)
	require.NoError(t, err)
	defer zr.Close()
	require.NotEmpty(t, zr.File)
	for _, f := range zr.File {
		assert.NotContains(t, f.Name, "proposals", f.Name)
		assert.NotContains(t, f.Name, id, f.Name)
	}
}
