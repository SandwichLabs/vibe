package docsref

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

// yamlKeys lists the yaml keys of a struct type (the schema's source of truth).
func yamlKeys(t reflect.Type) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// docKeys reads the backticked first-column keys of the table between
// <!-- keys:NAME --> and <!-- /keys --> in docs/reference/workspaces.md.
func docKeys(t *testing.T, doc, name string) []string {
	t.Helper()
	start := strings.Index(doc, "<!-- keys:"+name+" -->")
	require.GreaterOrEqual(t, start, 0, "workspaces.md is missing the %q keys table", name)
	rest := doc[start:]
	end := strings.Index(rest, "<!-- /keys -->")
	require.Greater(t, end, 0)
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")
	var keys []string
	for _, m := range row.FindAllStringSubmatch(rest[:end], -1) {
		keys = append(keys, m[1])
	}
	sort.Strings(keys)
	return keys
}

// TestWorkspacesDocMatchesSchema keeps docs/reference/workspaces.md honest: the
// keys it documents for vibe.yaml, a connection definition, the local file and
// the user file are exactly the keys the strict schema accepts.
func TestWorkspacesDocMatchesSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "workspaces.md"))
	require.NoError(t, err)
	doc := string(data)

	assert.Equal(t, yamlKeys(reflect.TypeOf(workspace.Config{})), docKeys(t, doc, "vibe.yaml"), "vibe.yaml keys")
	assert.Equal(t, yamlKeys(reflect.TypeOf(workspace.ConnectionDef{})), docKeys(t, doc, "connection"), "connection definition keys")
	assert.Equal(t, yamlKeys(reflect.TypeOf(workspace.LocalConfig{})), docKeys(t, doc, "local"), "local connections.yaml keys")
	assert.Equal(t, yamlKeys(reflect.TypeOf(workspace.UserConfig{})), docKeys(t, doc, "user"), "user connections.yaml keys")

	// Reserved names and the file names are documented too.
	for _, n := range workspace.ReservedNames {
		assert.Contains(t, doc, "`"+n+"`", "reserved connection name %q", n)
	}
	for _, f := range []string{workspace.ConfigFile, workspace.LocalDir, workspace.LocalConfigFile, workspace.LocalStateFile} {
		assert.Contains(t, doc, f)
	}
	for _, tier := range []string{workspace.SandboxReadOnly, workspace.SandboxReadWrite, workspace.SandboxFull} {
		assert.Contains(t, doc, "`"+tier+"`")
	}
}
