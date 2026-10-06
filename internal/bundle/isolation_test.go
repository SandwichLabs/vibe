package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The bundle is a user action. Nothing the agent or an MCP client can reach
// may import this package: no tool, no prompt, no way to export the workspace
// or to import a file into it.
func TestAgentAndMCPCannotReachTheBundle(t *testing.T) {
	for _, dir := range []string{"agents", "llm", "mcp", "a2ui", "usersql"} {
		root := filepath.Join("..", dir)
		require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			require.NoError(t, err)
			if d.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			b, err := os.ReadFile(p)
			require.NoError(t, err)
			require.NotContains(t, string(b), "internal/bundle", p)
			return nil
		}))
	}
}
