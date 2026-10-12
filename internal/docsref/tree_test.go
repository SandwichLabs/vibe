package docsref

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeEntry matches the leading "name/, name2/" run of an internal/ entry line
// in CLAUDE.md's architecture tree (two-space indent). Description text after
// the run may mention other paths (e.g. "llm/skills"); only the run counts.
var treeEntry = regexp.MustCompile(`^  ((?:[a-z0-9]+/(?:,\s+)?)+)`)

// TestClaudeMDTreeMatchesInternal fails when the architecture tree in
// CLAUDE.md lists a package that is not an internal/ directory, or omits one.
func TestClaudeMDTreeMatchesInternal(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)

	listed := map[string]bool{}
	in := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "internal/") {
			in = true
			continue
		}
		if in && !strings.HasPrefix(line, " ") {
			break // the next top-level tree entry (docs/) ends the block
		}
		if !in {
			continue
		}
		if m := treeEntry.FindStringSubmatch(line); m != nil {
			for _, n := range regexp.MustCompile(`[a-z0-9]+`).FindAllString(m[1], -1) {
				listed[n] = true
			}
		}
	}
	require.NotEmpty(t, listed, "CLAUDE.md has no internal/ block in its architecture tree")

	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	require.NoError(t, err)
	actual := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			actual[e.Name()] = true
		}
	}

	keys := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	assert.Equal(t, keys(actual), keys(listed),
		"CLAUDE.md's architecture tree must list exactly the internal/ directories (add or remove the entry)")
}
