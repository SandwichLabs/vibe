package docsref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
)

// Every table function, CALL and PRAGMA the agent SQL policy allows, and every
// statement kind it refuses in every tier, is named in
// docs/reference/agent-policy.md (#116).
func TestAgentPolicyReferenceNamesEveryAllowedAndDeniedThing(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "agent-policy.md"))
	require.NoError(t, err)
	doc := string(b)
	for _, group := range []struct {
		what  string
		names []string
	}{
		{"reader", agents.PolicyReaderFunctions()},
		{"table function", agents.PolicyTableFunctions()},
		{"CALL", agents.PolicyCalls()},
		{"PRAGMA", agents.PolicyPragmas()},
	} {
		require.NotEmpty(t, group.names, group.what)
		for _, n := range group.names {
			assert.Contains(t, doc, "`"+n+"`", "allowed %s %q is undocumented in agent-policy.md", group.what, n)
		}
	}
	for _, kw := range []string{"ATTACH", "SET VARIABLE", "INSTALL", "EXPLAIN ANALYZE", "postgres_execute", "list_connections", "read-only", "three gates", "always needs your OK"} {
		assert.Contains(t, doc, kw)
	}
	// The README indexes the page.
	r, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "README.md"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(r), "agent-policy.md"))
}
