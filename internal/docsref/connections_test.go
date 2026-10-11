package docsref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
)

// Every connection type, field and credential field the registry declares is
// documented in docs/reference/connections.md.
func TestConnectionsReferenceCoversTheTypeRegistry(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "connections.md"))
	require.NoError(t, err)
	doc := string(b)
	for _, info := range connections.ListConnectionTypes() {
		found := false
		for _, line := range strings.Split(doc, "\n") {
			if strings.HasPrefix(line, "### ") && strings.Contains(line, "`"+info.Name+"`") {
				found = true
			}
		}
		assert.True(t, found, "type %s has a section heading", info.Name)
		for _, f := range append(append([]connections.Field{}, info.Fields...), info.Credentials...) {
			assert.Contains(t, strings.ToLower(doc), "`"+f.Name+"`", "%s field %s is documented", info.Name, f.Name)
		}
	}
	for _, kw := range []string{"read-only role", "read-scaling", "MOTHERDUCK_TOKEN", "--install"} {
		assert.Contains(t, doc, kw)
	}
}
