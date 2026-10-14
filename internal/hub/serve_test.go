//go:build unix

package hub

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLocalDB puts a small table into the workspace's default `local` DuckDB
// file before anything opens it.
func seedLocalDB(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "data", "local.duckdb")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	db, err := sql.Open("duckdb", p)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE orders (id INTEGER, status VARCHAR); INSERT INTO orders VALUES (1,'Open'),(2,'Void'),(3,'Open')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

// rpc is a line-oriented JSON-RPC client over a pair of pipes.
type rpc struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Scanner
	next int
}

func (c *rpc) call(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	msg, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	require.NoError(c.t, err)
	_, err = c.in.Write(append(msg, '\n'))
	require.NoError(c.t, err)
	require.True(c.t, c.out.Scan(), "server closed stdout: %v", c.out.Err())
	var m map[string]any
	require.NoError(c.t, json.Unmarshal(c.out.Bytes(), &m), "stdout must be JSON-RPC only: %q", c.out.Text())
	require.EqualValues(c.t, c.next, m["id"])
	return m
}

func (c *rpc) notify(method string) {
	c.t.Helper()
	_, err := io.WriteString(c.in, fmt.Sprintf(`{"jsonrpc":"2.0","method":%q}`+"\n", method))
	require.NoError(c.t, err)
}

func (c *rpc) handshake() {
	c.t.Helper()
	res := c.call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})
	require.NotNil(c.t, res["result"])
	c.notify("notifications/initialized")
}

func (c *rpc) tool(name string, args map[string]any) (text string, isErr bool) {
	c.t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	r, ok := res["result"].(map[string]any)
	require.True(c.t, ok, "tools/call answered with %v", res)
	content, _ := r["content"].([]any)
	require.NotEmpty(c.t, content)
	first, _ := content[0].(map[string]any)
	text, _ = first["text"].(string)
	isErr, _ = r["isError"].(bool)
	return text, isErr
}

type serveRun struct {
	*rpc
	stderr *syncBuf
	done   chan error
	stop   context.CancelFunc
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// startServe runs Serve with stdio wired to pipes. Closing run.in is the
// client disconnecting.
func startServe(t *testing.T, opts ServeOptions) *serveRun {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	run := &serveRun{stderr: &syncBuf{}, done: make(chan error, 1), stop: cancel}
	opts.In, opts.Out, opts.Err, opts.Logger = inR, outW, run.stderr, quietLogger()
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	run.rpc = &rpc{t: t, in: inW, out: sc}
	go func() {
		err := Serve(ctx, opts)
		_ = outW.Close()
		run.done <- err
	}()
	t.Cleanup(func() { cancel(); _ = inW.Close(); _ = outR.Close() })
	return run
}

func (r *serveRun) finish(t *testing.T) error {
	t.Helper()
	_ = r.in.Close()
	select {
	case err := <-r.done:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not end")
		return nil
	}
}

func TestServeOwnerOverStdio(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	run := startServe(t, ServeOptions{Workspace: ws.Root()})
	run.handshake()

	list, ok := run.call("tools/list", nil)["result"].(map[string]any)
	require.True(t, ok)
	tools, ok := list["tools"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, tools)

	text, isErr := run.tool("run_query", map[string]any{"sql": "SELECT count(*) AS n FROM orders WHERE status = 'Open'"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "2", "the query ran against data/local.duckdb")

	// Ask-before-changes is on by default and nobody is here to approve.
	text, isErr = run.tool("run_query", map[string]any{"sql": "INSERT INTO orders VALUES (9, 'Refused')"})
	assert.True(t, isErr)
	assert.Contains(t, text, "needs your OK")
	assert.Contains(t, text, "other than the workspace's local database always need Vibe Studio")
	text, isErr = run.tool("run_query", map[string]any{"sql": "SELECT count(*) FROM orders WHERE status = 'Refused'"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "0", "the refused write did not run")

	// While serving, the owner is visible and shares an endpoint.
	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.Equal(t, AppServe, h.App)
	require.NotNil(t, h.MCP, "a headless owner shares its MCP so a second client can proxy to it")

	require.NoError(t, run.finish(t))
	_, err = ReadHolder(ws.Root())
	assert.ErrorIs(t, err, ErrNotHeld, "the hub is released on exit")
	assert.NotContains(t, run.stderr.String(), h.MCP.Token, "the token is never logged")
}

func TestServeOwnerTightensButNeverLoosensPolicy(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	// This person turned the gate off and chose full; --sandbox read-only
	// must win, and --sandbox full must not grant anything extra.
	local := ws.Local()
	local.Agent = workspace.LocalAgent{Sandbox: workspace.SandboxReadWrite, AskBeforeWrites: workspace.Bool(false)}
	require.NoError(t, ws.SaveLocal(local))

	run := startServe(t, ServeOptions{Workspace: ws.Root(), Sandbox: "read-only"})
	run.handshake()
	text, isErr := run.tool("run_query", map[string]any{"sql": "DELETE FROM orders"})
	assert.True(t, isErr, text)
	require.NoError(t, run.finish(t))

	run = startServe(t, ServeOptions{Workspace: ws.Root(), Sandbox: "full"})
	run.handshake()
	text, isErr = run.tool("run_query", map[string]any{"sql": "DELETE FROM orders WHERE id = 2"})
	assert.False(t, isErr, "read-write with the gate off may write: %s", text)
	_, isErr = run.tool("run_query", map[string]any{"sql": "INSTALL httpfs"})
	assert.True(t, isErr, "--sandbox full does not lift the workspace's read-write tier")
	require.NoError(t, run.finish(t))
}

func TestServeProxiesToStudioWhenStudioOwnsTheHub(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())

	// What Studio does (#111): take the hub, open the session, serve MCP.
	hold, err := AcquireHub(ws, AppStudio)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	sess, _, err := workspace.OpenSession(t.Context(), ws, quietLogger())
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	studio, err := StartWorkspaceMCP(ws, sess, hold, WithMCPLogger(quietLogger()))
	require.NoError(t, err)
	defer studio.Stop() //nolint:errcheck
	var approvals []string
	studio.Handler.SetApprover(func(_ context.Context, req agents.ApprovalRequest) error {
		approvals = append(approvals, req.Tool)
		return nil // the person clicks Approve in Studio
	})

	run := startServe(t, ServeOptions{Workspace: ws.Root()})
	run.handshake()
	text, isErr := run.tool("run_query", map[string]any{"sql": "SELECT count(*) FROM orders"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "3")

	// A write goes through Studio's approval, which the headless path cannot do.
	text, isErr = run.tool("run_query", map[string]any{"sql": "INSERT INTO orders VALUES (4, 'New')"})
	require.False(t, isErr, text)
	assert.NotEmpty(t, approvals, "Studio's approver was asked")
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM orders").Scan(&n))
	assert.Equal(t, 4, n)

	assert.Contains(t, run.stderr.String(), "passing MCP through")
	assert.NotContains(t, run.stderr.String(), studio.Token())

	// Studio quits: the proxy answers and exits non-zero rather than hanging.
	require.NoError(t, studio.Stop())
	select {
	case err := <-run.done:
		assert.ErrorIs(t, err, vibemcp.ErrUpstreamLost)
		assert.NotContains(t, errString(err), studio.Token())
	case <-time.After(20 * time.Second):
		t.Fatal("the proxy did not exit after Studio stopped MCP")
	}
}

func TestServeProxiesEvenWhenTheWorkspaceFileIsBroken(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	hold, err := AcquireHub(ws, AppStudio)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	sess, _, err := workspace.OpenSession(t.Context(), ws, quietLogger())
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	studio, err := StartWorkspaceMCP(ws, sess, hold, WithMCPLogger(quietLogger()))
	require.NoError(t, err)
	defer studio.Stop() //nolint:errcheck

	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), workspace.ConfigFile), []byte("version: 1\nbogus: [\n"), 0o600))

	run := startServe(t, ServeOptions{Workspace: ws.Root()})
	run.handshake()
	text, isErr := run.tool("run_query", map[string]any{"sql": "SELECT count(*) FROM orders"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "3")
	assert.Contains(t, run.stderr.String(), "passing MCP through")
}

func TestServeBrokenWorkspaceFileWithNoHolderIsAnError(t *testing.T) {
	ws := newWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), workspace.ConfigFile), []byte("version: 1\nbogus: [\n"), 0o600))
	err := Serve(t.Context(), ServeOptions{Workspace: ws.Root(), In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, Logger: quietLogger()})
	require.ErrorContains(t, err, "failed to open the workspace")
}

func TestServeNamesAHolderThatIsNotServingMCP(t *testing.T) {
	ws := newWorkspace(t)
	hold, err := AcquireHub(ws, AppStudio) // Studio with MCP off
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	err = Serve(t.Context(), ServeOptions{Workspace: ws.Root(), In: strings.NewReader(""), Out: io.Discard, Logger: quietLogger()})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHubHeld)
	assert.Contains(t, err.Error(), fmt.Sprintf("Studio (pid %d)", os.Getpid()))
	assert.Contains(t, err.Error(), "MCP server is off")
	assert.Contains(t, err.Error(), "Settings → Workspace → MCP")
}

func TestServeSSERefusedWhenAnotherProcessOwns(t *testing.T) {
	ws := newWorkspace(t)
	hold, err := AcquireHub(ws, AppStudio)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: "http://127.0.0.1:1/sse", Token: "t"}))
	err = Serve(t.Context(), ServeOptions{Workspace: ws.Root(), Transport: "sse", Logger: quietLogger()})
	require.ErrorIs(t, err, ErrHubHeld)
}

func TestServeRefusesLegacyFilesAndNonWorkspaces(t *testing.T) {
	_, err := ResolveRoot("notes.vibe", "")
	assert.ErrorIs(t, err, ErrLegacyFile)
	assert.Contains(t, err.Error(), "no longer opened directly")
	_, err = ResolveRoot("/somewhere/NOTES.VIBE", "")
	assert.ErrorIs(t, err, ErrLegacyFile)

	_, err = ResolveRoot("", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Vibe workspace found")
}

func TestResolveRootWalksUp(t *testing.T) {
	ws := newWorkspace(t)
	sub := filepath.Join(ws.Root(), "notebooks")
	got, err := ResolveRoot("", sub)
	require.NoError(t, err)
	assert.Equal(t, ws.Root(), got)
	got, err = ResolveRoot(sub, "")
	require.NoError(t, err)
	assert.Equal(t, ws.Root(), got)
}

func TestServeBadSandboxNeverTakesTheLock(t *testing.T) {
	ws := newWorkspace(t)
	err := Serve(t.Context(), ServeOptions{Workspace: ws.Root(), Sandbox: "godmode", Logger: quietLogger()})
	require.Error(t, err)
	_, statErr := os.Stat(LockPath(ws.Root()))
	assert.True(t, os.IsNotExist(statErr))
}

func TestResolveSandbox(t *testing.T) {
	for _, tc := range []struct {
		base    agents.SandboxMode
		request string
		want    agents.SandboxMode
		bad     bool
	}{
		{agents.ModeReadWrite, "", agents.ModeReadWrite, false},
		{agents.ModeFull, "read-only", agents.ModeReadOnly, false},
		{agents.ModeReadOnly, "full", agents.ModeReadOnly, false},
		{agents.ModeReadWrite, "read-write", agents.ModeReadWrite, false},
		{agents.ModeFull, "godmode", "", true},
	} {
		got, err := ResolveSandbox(tc.base, tc.request)
		if tc.bad {
			assert.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "%s + %q", tc.base, tc.request)
	}
}

func TestStartWorkspaceMCPPublishesAndClearsTheEndpoint(t *testing.T) {
	ws := newWorkspace(t)
	sess, _, err := workspace.OpenSession(t.Context(), ws, quietLogger())
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	hold, err := AcquireHub(ws, AppStudio)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	m, err := StartWorkspaceMCP(ws, sess, hold, WithMCPRememberPort(), WithMCPLogger(quietLogger()))
	require.NoError(t, err)
	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	require.NotNil(t, h.MCP)
	assert.Equal(t, m.URL(), h.MCP.URL)
	assert.Equal(t, m.Token(), h.MCP.Token)
	port := ws.Local().MCPPort
	assert.NotZero(t, port, "the port is remembered for next time")

	require.NoError(t, m.Stop())
	h, err = ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.Nil(t, h.MCP, "stopping MCP clears the endpoint")

	// Next start reuses the remembered port (when it is still free).
	m2, err := StartWorkspaceMCP(ws, sess, hold, WithMCPRememberPort(), WithMCPLogger(quietLogger()))
	require.NoError(t, err)
	defer m2.Stop() //nolint:errcheck
	assert.True(t, strings.HasSuffix(m2.Addr(), fmt.Sprintf(":%d", port)), "%s should end in :%d", m2.Addr(), port)
	assert.NotEqual(t, m.Token(), m2.Token(), "the token is fresh every start")
}

// studioLike is what Studio does (#111) for a workspace it owns, in-process.
func studioLike(t *testing.T, ws workspace.Workspace, approver agents.ApproveFunc) (*Hold, *WorkspaceMCP, session.Session) {
	t.Helper()
	hold, err := AcquireHub(ws, AppStudio)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hold.Release() })
	sess, _, err := workspace.OpenSession(t.Context(), ws, quietLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	m, err := StartWorkspaceMCP(ws, sess, hold, WithMCPLogger(quietLogger()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Stop() })
	if approver != nil {
		m.Handler.SetApprover(approver)
	}
	return hold, m, sess
}

func TestProxySandboxFlagIsEnforcedByTheOwner(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	// Studio's person approves everything; --sandbox read-only must still win.
	_, _, sess := studioLike(t, ws, func(context.Context, agents.ApprovalRequest) error { return nil })
	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.True(t, h.MCP.Has(CapSandboxHeader), "the owner advertises that it applies the header")

	run := startServe(t, ServeOptions{Workspace: ws.Root(), Sandbox: "read-only"})
	run.handshake()
	text, isErr := run.tool("run_query", map[string]any{"sql": "INSERT INTO orders VALUES (4, 'New')"})
	assert.True(t, isErr, text)
	text, isErr = run.tool("create_or_update_skill", map[string]any{"name": "evil", "description": "d", "instructions": "x"})
	assert.True(t, isErr, "content tools are refused too: %s", text)
	text, isErr = run.tool("run_query", map[string]any{"sql": "SELECT count(*) FROM orders"})
	assert.False(t, isErr, text)
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM orders").Scan(&n))
	assert.Equal(t, 3, n, "nothing was written")

	// The same owner, no flag: the approved write goes through, so the
	// refusals above came from the flag and not from the setup.
	open := startServe(t, ServeOptions{Workspace: ws.Root()})
	open.handshake()
	text, isErr = open.tool("run_query", map[string]any{"sql": "INSERT INTO orders VALUES (4, 'New')"})
	assert.False(t, isErr, text)
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM orders").Scan(&n))
	assert.Equal(t, 4, n)
}

func TestProxySandboxFlagNeedsAnOwnerThatSupportsIt(t *testing.T) {
	ws := newWorkspace(t)
	hold, studio, _ := studioLike(t, ws, nil)
	// An owner from before the header existed: same server, no capability.
	require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: studio.URL(), Token: studio.Token()}))
	err := Serve(t.Context(), ServeOptions{Workspace: ws.Root(), Sandbox: "read-only", In: strings.NewReader(""), Out: io.Discard, Logger: quietLogger()})
	require.ErrorIs(t, err, ErrHubHeld)
	assert.Contains(t, err.Error(), "--sandbox")
}

func TestSecondClientWaitsForAnOwnerThatIsStillOpening(t *testing.T) {
	ws := newWorkspace(t)
	seedLocalDB(t, ws.Root())
	// An owner that has taken the lock but not yet opened its session.
	hold, err := acquireAt(ws.Root(), AppServe, WithPendingMCP())
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	require.NotNil(t, h.MCP)
	assert.True(t, h.MCP.Pending)

	go func() {
		time.Sleep(600 * time.Millisecond)
		sess, _, serr := workspace.OpenSession(context.Background(), ws, quietLogger())
		if serr != nil {
			return
		}
		m, merr := StartWorkspaceMCP(ws, sess, hold, WithMCPLogger(quietLogger()))
		if merr != nil {
			return
		}
		t.Cleanup(func() { _ = m.Stop(); _ = sess.Close() })
	}()

	run := startServe(t, ServeOptions{Workspace: ws.Root()})
	run.handshake() // would have failed with "not sharing MCP" before it published
	text, isErr := run.tool("run_query", map[string]any{"sql": "SELECT count(*) FROM orders"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "3")
}

func TestHubLockSymlinksAreNeverFollowed(t *testing.T) {
	t.Run("hub.lock is a symlink", func(t *testing.T) {
		ws := newWorkspace(t)
		require.NoError(t, os.MkdirAll(filepath.Dir(LockPath(ws.Root())), 0o700))
		victim := filepath.Join(t.TempDir(), "victim.txt")
		require.NoError(t, os.WriteFile(victim, []byte("precious"), 0o644))
		require.NoError(t, os.Symlink(victim, LockPath(ws.Root())))
		_, err := AcquireHub(ws, AppStudio)
		require.Error(t, err)
		got, _ := os.ReadFile(victim)
		assert.Equal(t, "precious", string(got), "the victim was not written through")
		fi, _ := os.Stat(victim)
		assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm())
		_, err = ReadHolder(ws.Root())
		assert.ErrorIs(t, err, ErrUntrusted)
	})
	t.Run(".vibe-local is a symlink", func(t *testing.T) {
		ws := newWorkspace(t)
		elsewhere := t.TempDir()
		local := filepath.Join(ws.Root(), workspace.LocalDir)
		require.NoError(t, os.RemoveAll(local))
		require.NoError(t, os.Symlink(elsewhere, local))
		_, err := AcquireHub(ws, AppStudio)
		require.ErrorIs(t, err, ErrUntrusted)
		entries, _ := os.ReadDir(elsewhere)
		assert.Empty(t, entries, "nothing was created through the symlinked directory")
		_, err = ReadHolder(ws.Root())
		assert.ErrorIs(t, err, ErrUntrusted)
	})
	t.Run("hub.lock is a hard link", func(t *testing.T) {
		ws := newWorkspace(t)
		require.NoError(t, os.MkdirAll(filepath.Dir(LockPath(ws.Root())), 0o700))
		victim := filepath.Join(t.TempDir(), "victim.txt")
		require.NoError(t, os.WriteFile(victim, []byte("precious"), 0o600))
		if err := os.Link(victim, LockPath(ws.Root())); err != nil {
			t.Skip("hard links unavailable:", err)
		}
		_, err := AcquireHub(ws, AppStudio)
		require.Error(t, err)
		got, _ := os.ReadFile(victim)
		assert.Equal(t, "precious", string(got))
	})
	t.Run("hub.lock is a FIFO", func(t *testing.T) {
		ws := newWorkspace(t)
		require.NoError(t, os.MkdirAll(filepath.Dir(LockPath(ws.Root())), 0o700))
		require.NoError(t, syscall.Mkfifo(LockPath(ws.Root()), 0o600))
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err := ReadHolder(ws.Root())
			assert.ErrorIs(t, err, ErrUntrusted)
			_, err = AcquireHub(ws, AppStudio)
			assert.Error(t, err)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("a FIFO hung the hub lock")
		}
	})
}

func TestAcquireSurvivesConcurrentProbes(t *testing.T) {
	// ReadHolder's probe briefly holds a shared flock; a concurrent AcquireHub
	// must not report a phantom holder.
	ws := newWorkspace(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = ReadHolder(ws.Root())
			}
		}
	}()
	for i := 0; i < 200; i++ {
		hold, err := AcquireHub(ws, "Vibe Studio")
		require.NoError(t, err, "attempt %d", i)
		require.NoError(t, hold.Release())
	}
	close(stop)
	wg.Wait()
}
