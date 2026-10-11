package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClaudeConfig(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "work", "Acme Analytics")
	exe := filepath.Join(string(filepath.Separator), "usr", "local", "bin", "vibe")
	cfg, err := BuildClaudeConfig(root, exe)
	require.NoError(t, err)
	assert.Equal(t, "vibe-acme-analytics", cfg.Name)

	var desktop struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Env     any      `json:"env"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal([]byte(cfg.Desktop), &desktop))
	srv, ok := desktop.MCPServers[cfg.Name]
	require.True(t, ok)
	assert.Equal(t, exe, srv.Command)
	assert.Equal(t, []string{"mcp", "serve", "--workspace", root}, srv.Args)
	assert.Nil(t, srv.Env, "no environment, so no token")

	if runtime.GOOS != "windows" {
		assert.Equal(t, "claude mcp add vibe-acme-analytics -- /usr/local/bin/vibe mcp serve --workspace '/work/Acme Analytics'", cfg.Code)
	}
}

func TestBuildClaudeConfigNeverCarriesATokenOrALoosenedPolicy(t *testing.T) {
	ws := newWorkspace(t)
	hold, err := AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: "http://127.0.0.1:4242/sse", Token: "tok-from-the-lockfile"}))

	exe, err := os.Executable()
	require.NoError(t, err)
	cfg, err := BuildClaudeConfig(ws.Root(), exe)
	require.NoError(t, err)
	for _, s := range []string{cfg.Desktop, cfg.Code} {
		assert.NotContains(t, s, "tok-from-the-lockfile")
		assert.NotContains(t, s, "token")
		assert.NotContains(t, s, ":4242")
		assert.NotContains(t, s, "--sandbox")
	}
}

func TestBuildClaudeConfigNeedsAbsolutePaths(t *testing.T) {
	_, err := BuildClaudeConfig("relative/dir", "/usr/bin/vibe")
	assert.Error(t, err)
	_, err = BuildClaudeConfig("/abs", "vibe")
	assert.Error(t, err)
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "/a/b-c_d.e", shellQuote("/a/b-c_d.e"))
	assert.Equal(t, `'/a b'`, shellQuote("/a b"))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
	assert.Equal(t, `'$(rm -rf ~)'`, shellQuote("$(rm -rf ~)"))
	assert.Equal(t, "''", shellQuote(""))
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "acme-analytics", slug("Acme  Analytics!"))
	assert.Equal(t, "workspace", slug("???"))
}
