//go:build unix

package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperHolder starts this test binary as a separate process that holds the
// hub of root (see TestHelperHoldHub) and returns once it reports ready.
func helperHolder(t *testing.T, root string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperHoldHub$")
	cmd.Env = append(os.Environ(), append([]string{"VIBE_HUB_TEST_ROOT=" + root, "VIBE_HUB_TEST_READY=" + ready}, extraEnv...)...)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitFor(t, "the helper to take the hub", func() bool { _, err := os.Stat(ready); return err == nil })
	return cmd
}

// TestHelperHoldHub is not a test: it is the body of the subprocess started by
// helperHolder. It takes the hub, optionally releasable (exits 0 after
// releasing when asked to), and sleeps.
func TestHelperHoldHub(t *testing.T) {
	root := os.Getenv("VIBE_HUB_TEST_ROOT")
	if root == "" {
		t.Skip("not a helper invocation")
	}
	released := make(chan struct{})
	var opts []Option
	holdCh := make(chan *Hold, 1)
	if os.Getenv("VIBE_HUB_TEST_RELEASABLE") != "" {
		opts = append(opts, WithReleaseHandler(func() {
			h := <-holdCh // the handler can fire as soon as the lock is visible
			_ = h.Release()
			close(released)
		}))
	}
	hold, err := acquireAt(root, "Helper App", opts...)
	require.NoError(t, err)
	holdCh <- hold
	if os.Getenv("VIBE_HUB_TEST_MCP") != "" {
		require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: "http://127.0.0.1:1/sse", Token: "helper-secret"}))
	}
	require.NoError(t, os.WriteFile(os.Getenv("VIBE_HUB_TEST_READY"), nil, 0o600))
	select {
	case <-released:
		if os.Getenv("VIBE_HUB_TEST_RESPAWN") != "" {
			// An auto-reconnecting client: a new releasable owner appears the
			// moment the old one lets go.
			child := exec.Command(os.Args[0], "-test.run", "^TestHelperHoldHub$")
			child.Env = append(filterEnv(os.Environ(), "VIBE_HUB_TEST_RESPAWN"), "VIBE_HUB_TEST_READY="+os.Getenv("VIBE_HUB_TEST_READY")+".child")
			_ = child.Start()
		}
		os.Exit(0)
	case <-time.After(60 * time.Second):
	}
}

func filterEnv(env []string, drop string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, drop+"=") {
			out = append(out, e)
		}
	}
	return out
}

func TestAcquireContendRelease(t *testing.T) {
	ws := newWorkspace(t)
	hold, err := AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)

	fi, err := os.Stat(LockPath(ws.Root()))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "the hub lock is private")

	_, err = AcquireHub(ws, "vibe mcp serve")
	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.ErrorIs(t, err, ErrHubHeld)
	require.NotNil(t, held.Holder)
	assert.Equal(t, "Vibe Studio", held.Holder.App)
	assert.Equal(t, os.Getpid(), held.Holder.PID)
	assert.Contains(t, err.Error(), "Vibe Studio (pid")

	require.NoError(t, hold.Release())
	_, statErr := os.Stat(LockPath(ws.Root()))
	assert.True(t, os.IsNotExist(statErr), "release removes the lock file")

	again, err := AcquireHub(ws, "vibe mcp serve")
	require.NoError(t, err)
	require.NoError(t, again.Release())
}

func TestCrashedHolderLeavesNoLock(t *testing.T) {
	ws := newWorkspace(t)
	cmd := helperHolder(t, ws.Root(), "VIBE_HUB_TEST_MCP=1")

	_, err := AcquireHub(ws, "Vibe Studio")
	var held *HeldError
	require.ErrorAs(t, err, &held)
	require.NotNil(t, held.Holder)
	assert.Equal(t, cmd.Process.Pid, held.Holder.PID)
	require.NotNil(t, held.Holder.MCP)

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	// The file (with the dead process's token) is still on disk, but it is
	// not a held lock, so nothing may trust it.
	_, statErr := os.Stat(LockPath(ws.Root()))
	require.NoError(t, statErr, "a kill -9 leaves the file behind")
	_, err = ReadHolder(ws.Root())
	assert.ErrorIs(t, err, ErrNotHeld)

	hold, err := AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err, "the kernel lock died with the process")
	defer hold.Release() //nolint:errcheck
	assert.Nil(t, hold.Holder().MCP, "the new owner starts with no endpoint")
}

func TestReadHolder(t *testing.T) {
	ws := newWorkspace(t)
	_, err := ReadHolder(ws.Root())
	assert.ErrorIs(t, err, ErrNotHeld)

	hold, err := AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.Equal(t, "Vibe Studio", h.App)
	assert.Nil(t, h.MCP)

	ep := &MCPEndpoint{URL: "http://127.0.0.1:4242/sse", Token: "s3cret"}
	require.NoError(t, hold.SetMCP(ep))
	h, err = ReadHolder(ws.Root())
	require.NoError(t, err)
	require.NotNil(t, h.MCP)
	assert.Equal(t, *ep, *h.MCP)
	assert.NotContains(t, h.String(), "s3cret")
	assert.NotContains(t, h.MCP.String(), "s3cret")

	require.NoError(t, hold.SetMCP(nil))
	h, err = ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.Nil(t, h.MCP)
}

func TestReadHolderRefusesUntrustedFiles(t *testing.T) {
	ws := newWorkspace(t)
	hold, err := AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: "http://127.0.0.1:4242/sse", Token: "s3cret"}))
	lp := LockPath(ws.Root())

	t.Run("group or world readable", func(t *testing.T) {
		for _, mode := range []os.FileMode{0o640, 0o604, 0o644, 0o660} {
			require.NoError(t, os.Chmod(lp, mode))
			_, err := ReadHolder(ws.Root())
			assert.ErrorIs(t, err, ErrUntrusted, "mode %o", mode)
			assert.NotContains(t, errString(err), "s3cret")
		}
		require.NoError(t, os.Chmod(lp, 0o600))
		_, err := ReadHolder(ws.Root())
		assert.NoError(t, err)
	})

	t.Run("symlink", func(t *testing.T) {
		real := lp + ".real"
		require.NoError(t, os.Rename(lp, real))
		require.NoError(t, os.Symlink(real, lp))
		_, err := ReadHolder(ws.Root())
		assert.ErrorIs(t, err, ErrUntrusted)
		require.NoError(t, os.Remove(lp))
		require.NoError(t, os.Rename(real, lp))
	})

	t.Run("non-loopback or non-http endpoint", func(t *testing.T) {
		for _, u := range []string{"http://evil.example:80/sse", "http://10.0.0.5:4242/sse", "https://127.0.0.1:4242/sse", "http://user:pw@127.0.0.1/sse", "file:///etc/passwd"} {
			require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: u, Token: "s3cret"}))
			_, err := ReadHolder(ws.Root())
			assert.ErrorIs(t, err, ErrUntrusted, u)
		}
		require.NoError(t, hold.SetMCP(&MCPEndpoint{URL: "http://127.0.0.1:4242/sse"}))
		_, err := ReadHolder(ws.Root())
		assert.ErrorIs(t, err, ErrUntrusted, "no token")
	})
}

func TestACommittedLockFileIsNeverTrusted(t *testing.T) {
	// A cloned repository can ship a .vibe-local/hub.lock pointing anywhere.
	// With nobody holding the lock it is ignored, whatever it says.
	ws := newWorkspace(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(LockPath(ws.Root())), 0o755))
	evil, _ := json.Marshal(Holder{App: "Vibe Studio", PID: 1, MCP: &MCPEndpoint{URL: "http://127.0.0.1:9/sse", Token: "x"}})
	require.NoError(t, os.WriteFile(LockPath(ws.Root()), evil, 0o600))
	_, err := ReadHolder(ws.Root())
	assert.ErrorIs(t, err, ErrNotHeld)
	hold, err := AcquireHub(ws, "vibe mcp serve")
	require.NoError(t, err)
	require.NoError(t, hold.Release())
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestRequestReleaseRefusals(t *testing.T) {
	ws := newWorkspace(t)

	assert.ErrorIs(t, RequestRelease(ws), ErrNotHeld, "nothing to release")

	hold, err := AcquireHub(ws, "Vibe Studio") // this process, no release handler
	require.NoError(t, err)
	err = RequestRelease(ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already holds", "never signals itself")
	require.NoError(t, hold.Release())

	// A holder that never advertised a release handler is never signalled
	// (SIGUSR1's default action would kill it).
	cmd := helperHolder(t, ws.Root())
	err = RequestRelease(ws)
	require.ErrorIs(t, err, ErrNotReleasable)
	require.NoError(t, cmd.Process.Signal(syscall.Signal(0)), "the holder is still alive")
}

func TestTakeOver(t *testing.T) {
	ws := newWorkspace(t)
	cmd := helperHolder(t, ws.Root(), "VIBE_HUB_TEST_RELEASABLE=1")

	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.Equal(t, ControlSigusr1, h.Control)

	hold, err := TakeOver(t.Context(), ws, "Vibe Studio", 10*time.Second)
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck
	assert.Equal(t, "Vibe Studio", hold.Holder().App)
	assert.Equal(t, os.Getpid(), hold.Holder().PID)

	// The old owner released and exited cleanly.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the old owner did not exit")
	}
}

func TestTakeOverRefusedWhenNotReleasable(t *testing.T) {
	ws := newWorkspace(t)
	helperHolder(t, ws.Root()) // holds, cannot be released
	_, err := TakeOver(t.Context(), ws, "Vibe Studio", 300*time.Millisecond)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotReleasable)
	assert.Contains(t, err.Error(), "Helper App")
}

func TestTakeOverWhenFree(t *testing.T) {
	ws := newWorkspace(t)
	hold, err := TakeOver(t.Context(), ws, "Vibe Studio", time.Second)
	require.NoError(t, err)
	require.NoError(t, hold.Release())
}

func TestTakeOverAsksTheOwnerThatAppearsNext(t *testing.T) {
	ws := newWorkspace(t)
	helperHolder(t, ws.Root(), "VIBE_HUB_TEST_RELEASABLE=1", "VIBE_HUB_TEST_RESPAWN=1")
	t.Cleanup(func() {
		// Whatever the respawned owner is, do not leave it behind.
		if h, err := ReadHolder(ws.Root()); err == nil && h.PID != os.Getpid() {
			_ = syscall.Kill(h.PID, syscall.SIGKILL)
		}
	})
	hold, err := TakeOver(t.Context(), ws, "Vibe Studio", 15*time.Second)
	require.NoError(t, err, "the second owner was asked to release too")
	defer hold.Release() //nolint:errcheck
}

func mkRoot(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(r, ".vibe-local"), 0o700))
	return r
}

// The release signal is process-wide, so exactly one hold may be releasable
// and nothing else may disarm it.
func TestReleaseListenerIsASingleProcessWideRegistration(t *testing.T) {
	r1, r2 := mkRoot(t), mkRoot(t)
	var fired atomic.Int32
	a, err := acquireAt(r1, "A", WithReleaseHandler(func() { fired.Add(1) }))
	require.NoError(t, err)

	// A second releasable hold is refused...
	_, err = acquireAt(r2, "B", WithReleaseHandler(func() {}))
	require.ErrorIs(t, err, ErrOneReleasableHold)
	// ...a failed acquire of the same workspace disarms nothing...
	_, err = acquireAt(r1, "C")
	require.ErrorIs(t, err, ErrHubHeld)
	// ...and neither does releasing an unrelated, non-releasable hold.
	b, err := acquireAt(r2, "B")
	require.NoError(t, err)
	require.NoError(t, b.Release())

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
	waitFor(t, "A's release handler", func() bool { return fired.Load() == 1 })
	time.Sleep(100 * time.Millisecond)
	assert.EqualValues(t, 1, fired.Load(), "one signal, one release")

	// After A is gone, a stray SIGUSR1 must not kill the test process (the
	// listener is never torn down) and a new releasable hold works again.
	require.NoError(t, a.Release())
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
	time.Sleep(100 * time.Millisecond)
	c, err := acquireAt(r1, "D", WithReleaseHandler(func() { fired.Add(1) }))
	require.NoError(t, err)
	defer c.Release() //nolint:errcheck
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
	waitFor(t, "the new hold's handler", func() bool { return fired.Load() == 2 })
}
