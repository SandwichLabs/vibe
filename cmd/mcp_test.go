package cmd

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:0":       true,
		"127.0.0.1:8080":    true,
		"localhost:8080":    true,
		"0.0.0.0:8080":      false,
		"192.168.1.5:8080":  false,
		"evil.example:8080": false,
	}
	for addr, want := range cases {
		got, err := isLoopbackAddr(addr)
		require.NoError(t, err, "addr=%q", addr)
		assert.Equal(t, want, got, "addr=%q", addr)
	}
}

// TestIsLoopbackAddr_GarbageAddrIsNotLoopback: splitHostPortLenient
// tolerates a bare-host string with no port, so garbage input falls through
// to "not loopback" (false, nil) rather than necessarily erroring — the
// important guarantee is that it is never mistaken FOR loopback.
func TestIsLoopbackAddr_GarbageAddrIsNotLoopback(t *testing.T) {
	got, err := isLoopbackAddr("not an addr!!")
	require.NoError(t, err)
	assert.False(t, got)
}

// TestHelperVibe re-runs this test binary as the `vibe` CLI: the real cobra
// tree, the real os.Stdin/os.Stdout, the real signals.
func testCmdLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHelperVibe(t *testing.T) {
	args := os.Getenv("VIBE_TEST_ARGS")
	if args == "" {
		t.Skip("not a helper invocation")
	}
	rootCmd.SetArgs(strings.Split(args, "\x1f"))
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// lockedBuf collects a subprocess's stderr; the test reads it while the copy
// goroutine may still be writing.
type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func vibeProc(t *testing.T, cfgDir string, args ...string) (*exec.Cmd, io.WriteCloser, *bufio.Scanner, *lockedBuf) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperVibe$")
	cmd.Env = append(os.Environ(), "VIBE_TEST_ARGS="+strings.Join(args, "\x1f"), "VIBE_CONFIG_DIR="+cfgDir)
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr := &lockedBuf{}
	cmd.Stderr = stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	return cmd, in, sc, stderr
}

func newCmdWorkspace(t *testing.T) (root, cfgDir string) {
	t.Helper()
	cfgDir = t.TempDir()
	t.Setenv("VIBE_CONFIG_DIR", cfgDir)
	res, err := workspace.Init(filepath.Join(t.TempDir(), "ws"), workspace.WithInitLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, res.Workspace.Close())
	return res.Workspace.Root(), cfgDir
}

func TestMCPServe_RejectsVibeFiles(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "serve", "old.vibe"},
		{"mcp", "serve", "/some/where/OLD.VIBE"},
		{"mcp", "serve", "--workspace", "old.vibe"},
	} {
		_, err := executeCommand(args...)
		require.Error(t, err, "%v", args)
		assert.ErrorIs(t, err, hub.ErrLegacyFile, "%v", args)
		assert.Contains(t, err.Error(), "no longer opened directly", "%v", args)
		assert.Contains(t, err.Error(), "workspace folder", "%v", args)
	}
}

func TestMCPServe_NoLongerTakesAPositional(t *testing.T) {
	_, err := executeCommand("mcp", "serve", "/tmp/some-folder")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--workspace")
}

func TestMCPServe_NoWorkspaceHere(t *testing.T) {
	_, err := executeCommand("mcp", "serve", "--workspace", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Vibe workspace found")
}

// TestMCPServe_StdoutIsProtocolOnly runs the real CLI: every byte on stdout
// must be a JSON-RPC message, whatever the libraries do while the workspace
// opens, and nothing but JSON-RPC may be on it before the first request.
func TestMCPServe_StdoutIsProtocolOnly(t *testing.T) {
	root, cfg := newCmdWorkspace(t)
	cmd, in, out, stderr := vibeProc(t, cfg, "mcp", "serve", "--workspace", root)

	send := func(line string) {
		_, err := io.WriteString(in, line+"\n")
		require.NoError(t, err)
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_query","arguments":{"sql":"SELECT 42 AS answer"}}}`)

	var lines []string
	for len(lines) < 3 && out.Scan() {
		lines = append(lines, out.Text())
	}
	for _, l := range lines {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), "non-protocol bytes on stdout: %q", l)
		assert.Equal(t, "2.0", m["jsonrpc"])
	}
	require.Len(t, lines, 3, "stderr: %s", stderr.String())
	assert.Contains(t, lines[2], "42")

	require.NoError(t, in.Close())
	done := make(chan error, 1)
	go func() {
		// Anything else on stdout after the replies would be noise too.
		for out.Scan() {
			t.Errorf("unexpected stdout after the replies: %q", out.Text())
		}
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		assert.NoError(t, err, stderr.String())
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not exit when stdin closed")
	}
}

// TestMCPServe_TakeOver is the headless side of Studio's "Take over": a
// running `vibe mcp serve` lets go of the hub when asked, closes its session
// and exits 0, and the taker owns the hub.
func TestMCPServe_TakeOver(t *testing.T) {
	root, cfg := newCmdWorkspace(t)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()), workspace.WithConfigDir(cfg))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck

	cmd, in, out, stderr := vibeProc(t, cfg, "mcp", "serve", "--workspace", root)
	_, err = io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`+"\n")
	require.NoError(t, err)
	require.True(t, out.Scan(), stderr.String())

	h, err := hub.ReadHolder(root)
	require.NoError(t, err)
	assert.Equal(t, cmd.Process.Pid, h.PID)
	assert.Equal(t, hub.AppServe, h.App)
	assert.Equal(t, hub.ControlSigusr1, h.Control)

	hold, err := hub.TakeOver(t.Context(), ws, hub.AppStudio, 15*time.Second)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	assert.Equal(t, os.Getpid(), hold.Holder().PID)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		assert.NoError(t, err, "the old owner exits cleanly: %s", stderr.String())
	case <-time.After(15 * time.Second):
		t.Fatal("the old owner did not exit after releasing")
	}
	assert.Contains(t, stderr.String(), "releasing it and exiting")
	assert.NotContains(t, stderr.String(), h.MCP.Token)
}

// TestMCPServe_TwoClientsStartedTogether: Claude Desktop and Claude Code
// launching `vibe mcp serve` for one workspace at the same moment must both
// work: one owns the hub, the other waits for its endpoint and proxies.
func TestMCPServe_TwoClientsStartedTogether(t *testing.T) {
	root, cfg := newCmdWorkspace(t)
	type client struct {
		in     io.WriteCloser
		out    *bufio.Scanner
		stderr *lockedBuf
	}
	var cs []client
	for i := 0; i < 2; i++ {
		_, in, out, stderr := vibeProc(t, cfg, "mcp", "serve", "--workspace", root)
		cs = append(cs, client{in, out, stderr})
	}
	for i, c := range cs {
		_, err := io.WriteString(c.in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`+"\n"+
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_query","arguments":{"sql":"SELECT 42 AS answer"}}}`+"\n")
		require.NoError(t, err)
		for want := 1; want <= 2; want++ {
			require.True(t, c.out.Scan(), "client %d got no reply %d; stderr: %s", i, want, c.stderr.String())
			var m map[string]any
			require.NoError(t, json.Unmarshal(c.out.Bytes(), &m), c.out.Text())
			if want == 2 {
				assert.Contains(t, c.out.Text(), "42")
			}
		}
	}
	proxied := 0
	for _, c := range cs {
		if strings.Contains(c.stderr.String(), "passing MCP through") {
			proxied++
		}
	}
	assert.Equal(t, 1, proxied, "exactly one of the two proxied to the other")
}
