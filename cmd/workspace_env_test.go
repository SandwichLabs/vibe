package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

func envWS(t *testing.T) (string, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("VIBE_CONFIG_DIR", cfg)
	dir := filepath.Join(t.TempDir(), "acme")
	res, err := workspace.Init(dir)
	require.NoError(t, err)
	_ = res.Workspace.Close()
	require.NoError(t, os.WriteFile(filepath.Join(res.Workspace.Root(), ".env"), []byte("MOTHERDUCK_TOKEN=md-test-not-a-real-token\nPGUSER=rosa\n"), 0o600))
	return res.Workspace.Root(), cfg
}

func runEnv(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := workspaceEnvCmd
	for _, sub := range c.Commands() {
		if sub.Name() == args[0] {
			_ = sub.Flags().Set("workspace", root)
			sub.SetOut(&out)
			sub.SetErr(&out)
			err := sub.RunE(sub, nil)
			return out.String(), err
		}
	}
	t.Fatalf("no env subcommand %q", args[0])
	return "", nil
}

func TestWorkspaceEnv_StatusAllowDenyRevoke(t *testing.T) {
	root, _ := envWS(t)

	out, err := runEnv(t, root, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "not decided")
	assert.Contains(t, out, "MOTHERDUCK_TOKEN, PGUSER")
	assert.NotContains(t, out, "md-test-not-a-real-token")
	assert.NotContains(t, out, "rosa")

	out, err = runEnv(t, root, "allow")
	require.NoError(t, err)
	assert.Contains(t, out, "allowed")
	assert.NotContains(t, out, "md-test-not-a-real-token")

	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("MOTHERDUCK_TOKEN=md-test-second\n"), 0o600))
	out, err = runEnv(t, root, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "changed since you allowed it")

	out, err = runEnv(t, root, "deny")
	require.NoError(t, err)
	assert.Contains(t, out, "denied")

	out, err = runEnv(t, root, "revoke")
	require.NoError(t, err)
	assert.Contains(t, out, "not decided")
}

func TestAskDotEnvPromptsNamesOnly(t *testing.T) {
	root, _ := envWS(t)
	ws, err := workspace.Open(root)
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck

	var errW bytes.Buffer
	askDotEnv(ws, ws.DotEnv(), strings.NewReader("\n"), &errW)
	assert.Equal(t, "undecided", ws.DotEnv().State, "Not now decides nothing")
	assert.Contains(t, errW.String(), "MOTHERDUCK_TOKEN, PGUSER")
	assert.NotContains(t, errW.String(), "md-test-not-a-real-token")

	askDotEnv(ws, ws.DotEnv(), strings.NewReader("a\n"), &errW)
	assert.Equal(t, "allowed", ws.DotEnv().State)
	require.NoError(t, ws.RevokeDotEnv())
	askDotEnv(ws, ws.DotEnv(), strings.NewReader("d\n"), &errW)
	assert.Equal(t, "denied", ws.DotEnv().State)
}

func TestMaybePromptDotEnvNeverPromptsWithoutATerminal(t *testing.T) {
	root, _ := envWS(t)
	ws, err := workspace.Open(root)
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	var errW bytes.Buffer
	maybePromptDotEnv(ws, &errW) // go test's stdin is not a terminal
	assert.Empty(t, errW.String())
	assert.Equal(t, "undecided", ws.DotEnv().State)
}

func TestMaybePromptDotEnvOncePerProcessAndSuppressible(t *testing.T) {
	root, _ := envWS(t)
	ws, err := workspace.Open(root)
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	// Not a terminal under go test, so nothing is asked and nothing is consumed.
	dotenvPrompted, suppressDotEnvPrompt = false, true
	maybePromptDotEnv(ws, io.Discard)
	assert.False(t, dotenvPrompted)
	suppressDotEnvPrompt = false
	maybePromptDotEnv(ws, io.Discard)
	assert.Equal(t, "undecided", ws.DotEnv().State)
}
