//go:build unix

package hub

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spyWS and spySess count Close so a test can tell what was released.
type spyWS struct {
	workspace.Workspace
	closed *int
}

func (s spyWS) Close() error { *s.closed++; return s.Workspace.Close() }

type spySess struct {
	session.Session
	closed *int
}

func (s spySess) Close() error { *s.closed++; return s.Session.Close() }

// failAt returns steps that wrap the real ones with spies and fail the named
// step (Step("") fails nothing).
func failAt(fail Step, wsClosed, sessClosed *int, hubHeld *bool) steps {
	real := defaultSteps()
	boom := errors.New("injected failure")
	return steps{
		openWorkspace: func(root string, opts ...workspace.Option) (workspace.Workspace, error) {
			if fail == StepOpen {
				return nil, boom
			}
			ws, err := real.openWorkspace(root, opts...)
			if err != nil {
				return nil, err
			}
			return spyWS{ws, wsClosed}, nil
		},
		acquire: func(ctx context.Context, ws workspace.Workspace, o OpenOptions) (*Hold, error) {
			if fail == StepHub {
				return nil, boom
			}
			h, err := real.acquire(ctx, ws, o)
			*hubHeld = err == nil
			return h, err
		},
		openSession: func(ctx context.Context, ws workspace.Workspace, l *slog.Logger, opts ...workspace.SessionOption) (session.Session, []workspace.SessionIssue, error) {
			if fail == StepSession {
				return nil, nil, boom
			}
			s, issues, err := real.openSession(ctx, ws, l, opts...)
			if err != nil {
				return nil, nil, err
			}
			return spySess{s, sessClosed}, issues, nil
		},
		newEngine: func(sess session.Session, l *slog.Logger) (*engine.Manager, error) {
			if fail == "engine" {
				return nil, boom
			}
			return real.newEngine(sess, l)
		},
	}
}

func assertLockFree(t *testing.T, ws workspace.Workspace) {
	t.Helper()
	_, err := ReadHolder(ws.Root())
	assert.ErrorIs(t, err, ErrNotHeld)
	hold, err := AcquireHub(ws, "probe")
	require.NoError(t, err, "the hub lock must be re-acquirable")
	require.NoError(t, hold.Release())
}

func TestOpenReleasesEverythingWhenAStepFails(t *testing.T) {
	for _, fail := range []Step{StepOpen, StepHub, StepSession, "engine"} {
		t.Run(string(fail), func(t *testing.T) {
			ws := newWorkspace(t)
			var wsClosed, sessClosed int
			var held bool
			o, err := openWith(t.Context(), ws.Root(), OpenOptions{App: "test", Logger: quietLogger()},
				failAt(fail, &wsClosed, &sessClosed, &held))
			require.Error(t, err)
			assert.Nil(t, o)
			var oe *OpenError
			require.ErrorAs(t, err, &oe)
			if fail != "engine" {
				assert.Equal(t, fail, oe.Step)
			}
			switch fail {
			case StepOpen:
				assert.Zero(t, wsClosed)
			case StepHub:
				assert.Equal(t, 1, wsClosed, "the workspace is closed")
			case StepSession, "engine":
				assert.Equal(t, 1, wsClosed, "the workspace is closed")
				assert.True(t, held)
			}
			if fail == "engine" {
				assert.Equal(t, 1, sessClosed, "the session (DuckDB handles) is closed")
			}
			assertLockFree(t, ws)
		})
	}
}

func TestOpenCloseOrderIsReverseOfAcquisition(t *testing.T) {
	ws := newWorkspace(t)
	var order []string
	real := defaultSteps()
	st := real
	st.openSession = func(ctx context.Context, w workspace.Workspace, l *slog.Logger, opts ...workspace.SessionOption) (session.Session, []workspace.SessionIssue, error) {
		s, i, err := real.openSession(ctx, w, l, opts...)
		return orderSess{s, &order, w.Root()}, i, err
	}
	st.openWorkspace = func(root string, opts ...workspace.Option) (workspace.Workspace, error) {
		w, err := real.openWorkspace(root, opts...)
		return orderWS{w, &order}, err
	}
	o, err := openWith(t.Context(), ws.Root(), OpenOptions{App: "test", Logger: quietLogger()}, st)
	require.NoError(t, err)
	// While the session is open the lock is held; when it closes the lock is still ours.
	h, err := ReadHolder(ws.Root())
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), h.PID)
	o.Close()
	o.Close() // idempotent
	assert.Equal(t, []string{"session", "workspace"}, order)
	assertLockFree(t, ws)
}

type orderSess struct {
	session.Session
	o    *[]string
	root string
}

func (s orderSess) Close() error {
	// The session closes while the lock is still held.
	if _, err := ReadHolder(s.root); err == nil {
		*s.o = append(*s.o, "session")
	} else {
		*s.o = append(*s.o, "session-after-lock")
	}
	return s.Session.Close()
}

type orderWS struct {
	workspace.Workspace
	o *[]string
}

func (s orderWS) Close() error {
	// The hub lock was released before the workspace closed.
	if _, err := ReadHolder(s.Root()); !errors.Is(err, ErrNotHeld) {
		*s.o = append(*s.o, "workspace-before-lock")
		return s.Workspace.Close()
	}
	*s.o = append(*s.o, "workspace")
	return s.Workspace.Close()
}

func TestOpenReadWriteTakesTheHubAndCloseFreesIt(t *testing.T) {
	ws := newWorkspace(t)
	o, err := Open(t.Context(), ws.Root(), OpenOptions{App: "Vibe Studio", Logger: quietLogger()})
	require.NoError(t, err)
	assert.False(t, o.ReadOnly)
	require.NotNil(t, o.Hold)
	require.NotNil(t, o.Eng)
	assert.Equal(t, "Vibe Studio", o.Hold.Holder().App)
	o.Close()
	assertLockFree(t, ws)

	// The DuckDB file is usable again right away.
	o2, err := Open(t.Context(), ws.Root(), OpenOptions{App: "again", Logger: quietLogger()})
	require.NoError(t, err)
	_, err = o2.Sess.Exec(t.Context(), "SELECT 1")
	require.NoError(t, err)
	o2.Close()
}

func TestOpenHeldIsAnErrorUnlessFallbackReadOnly(t *testing.T) {
	ws := newWorkspace(t)
	helperHolder(t, ws.Root())

	_, err := Open(t.Context(), ws.Root(), OpenOptions{App: "vibe query", Logger: quietLogger()})
	require.Error(t, err)
	var oe *OpenError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, StepHub, oe.Step)
	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.ErrorIs(t, err, ErrHubHeld)

	o, err := Open(t.Context(), ws.Root(), OpenOptions{App: "Vibe Studio", Logger: quietLogger(), FallbackReadOnly: true})
	require.NoError(t, err)
	defer o.Close()
	assert.True(t, o.ReadOnly)
	assert.Nil(t, o.Hold)
	require.NotNil(t, o.Holder)
	assert.Equal(t, "Helper App", o.Holder.App)
}

func TestOpenReadOnlyTakesNoLock(t *testing.T) {
	ws := newWorkspace(t)
	helperHolder(t, ws.Root())
	o, err := Open(t.Context(), ws.Root(), OpenOptions{App: "vibe query", Mode: ModeReadOnly, Logger: quietLogger()})
	require.NoError(t, err)
	defer o.Close()
	assert.True(t, o.ReadOnly)
	assert.Nil(t, o.Hold)
	require.NotNil(t, o.Holder)
	assert.Equal(t, "Helper App", o.Holder.App)
}

func TestOpenRemoteOnlyTakesNoLockAndAttachesNoLocalFile(t *testing.T) {
	ws := newWorkspace(t)
	helperHolder(t, ws.Root())
	o, err := Open(t.Context(), ws.Root(), OpenOptions{Mode: ModeRemoteOnly, Logger: quietLogger()})
	require.NoError(t, err)
	defer o.Close()
	assert.False(t, o.ReadOnly)
	assert.Nil(t, o.Hold)
	require.Len(t, o.Issues, 1)
	assert.Contains(t, o.Issues[0].Message, "is not attached")
}

func TestOpenTakeOver(t *testing.T) {
	ws := newWorkspace(t)
	cmd := helperHolder(t, ws.Root(), "VIBE_HUB_TEST_RELEASABLE=1")
	o, err := Open(t.Context(), ws.Root(), OpenOptions{
		App: "Vibe Studio", Mode: ModeTakeOver, TakeOverTimeout: 10 * time.Second, Logger: quietLogger(),
	})
	require.NoError(t, err)
	require.NotNil(t, o.Hold)
	assert.False(t, o.ReadOnly)
	assert.Equal(t, "Vibe Studio", o.Hold.Holder().App)
	o.Close()
	require.NoError(t, cmd.Wait())
}

func TestOpenTakeOverRefusedWhenNotReleasable(t *testing.T) {
	ws := newWorkspace(t)
	helperHolder(t, ws.Root())
	_, err := Open(t.Context(), ws.Root(), OpenOptions{
		App: "Vibe Studio", Mode: ModeTakeOver, TakeOverTimeout: 300 * time.Millisecond, Logger: quietLogger(),
	})
	var oe *OpenError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, StepHub, oe.Step)
	assert.ErrorIs(t, err, ErrNotReleasable)
}

// dropID removes the id line from a workspace's vibe.yaml.
func dropID(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, workspace.ConfigFile)
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	var out []byte
	for _, line := range splitLines(string(data)) {
		if len(line) >= 3 && line[:3] == "id:" {
			continue
		}
		out = append(out, line...)
	}
	require.NoError(t, os.WriteFile(p, out, 0o600))
}

func splitLines(s string) []string {
	var lines []string
	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		if i < len(s) {
			i++
		}
		lines = append(lines, s[:i])
		s = s[i:]
	}
	return lines
}

func TestOpenMissingIDIsRepairedOnlyOnRequest(t *testing.T) {
	ws := newWorkspace(t)
	root := ws.Root()
	require.NoError(t, ws.Close())
	dropID(t, root)

	_, err := Open(t.Context(), root, OpenOptions{App: "vibe query", Logger: quietLogger()})
	require.ErrorIs(t, err, workspace.ErrMissingID)
	var oe *OpenError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, StepOpen, oe.Step)
	assertLockFreeAt(t, root)

	o, err := Open(t.Context(), root, OpenOptions{App: "Vibe Studio", Logger: quietLogger(), RepairMissingID: true})
	require.NoError(t, err)
	defer o.Close()
	assert.NotEmpty(t, o.WS.ID())
}

func assertLockFreeAt(t *testing.T, root string) {
	t.Helper()
	_, err := ReadHolder(root)
	assert.ErrorIs(t, err, ErrNotHeld)
}

func TestOpenHooksRunInOrder(t *testing.T) {
	ws := newWorkspace(t)
	var seq []string
	o, err := Open(t.Context(), ws.Root(), OpenOptions{
		App: "test", Logger: quietLogger(),
		OnWorkspaceOpened: func(workspace.Workspace) {
			_, err := ReadHolder(ws.Root())
			assert.ErrorIs(t, err, ErrNotHeld, "before the hub step")
			seq = append(seq, "opened")
		},
		BeforeSession: func(workspace.Workspace) {
			h, err := ReadHolder(ws.Root())
			require.NoError(t, err, "after the hub step")
			assert.Equal(t, os.Getpid(), h.PID)
			seq = append(seq, "before-session")
		},
	})
	require.NoError(t, err)
	o.Close()
	assert.Equal(t, []string{"opened", "before-session"}, seq)
}
