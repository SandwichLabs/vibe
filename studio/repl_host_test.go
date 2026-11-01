package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/repl"
	"github.com/SandwichLabs/slaw/internal/session"
)

// consoleRig collects every console frame and every other event of an App.
type consoleRig struct {
	app *App
	ev  eventLog
	mu  sync.Mutex
	by  map[string]*frameLog
}

func newConsoleRig(t *testing.T) *consoleRig { return newConsoleRigWith(t, newOpenApp(t)) }

func newConsoleRigWith(t *testing.T, app *App) *consoleRig {
	t.Helper()
	r := &consoleRig{app: app, by: map[string]*frameLog{}}
	r.app.emit = func(event string, data ...any) {
		if strings.HasPrefix(event, "repl:out:") && len(data) == 1 {
			if f, ok := data[0].(replFrame); ok {
				r.log(strings.TrimPrefix(event, "repl:out:")).add(f)
				return
			}
		}
		r.ev.sink(event, data...)
	}
	return r
}

func (r *consoleRig) log(id string) *frameLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by[id] == nil {
		r.by[id] = &frameLog{}
	}
	return r.by[id]
}

// open opens a console and attaches it, acknowledging frames as they arrive.
func (r *consoleRig) open(t *testing.T, conn string) string {
	t.Helper()
	res, err := r.app.ReplOpen(conn)
	require.NoError(t, err)
	require.NoError(t, r.app.ReplAck(res.ID, 0))
	go r.autoAck(res.ID)
	return res.ID
}

func (r *consoleRig) autoAck(id string) {
	l := r.log(id)
	acked := 0
	for {
		time.Sleep(2 * time.Millisecond)
		fs := l.all()
		for _, f := range fs {
			if f.Seq > acked {
				acked = f.Seq
				if r.app.ReplAck(id, f.Seq) != nil {
					return
				}
			}
			if f.Closed {
				return
			}
		}
	}
}

func (r *consoleRig) waitText(t *testing.T, id, want string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(r.log(id).text(), want) }, 10*time.Second, 10*time.Millisecond,
		"waiting for %q in:\n%s", want, r.log(id).text())
}

func TestConsoleRoundTrip(t *testing.T) {
	r := newConsoleRig(t)
	id := r.open(t, "")
	r.waitText(t, id, "Slaw console on App Test")
	require.NoError(t, r.app.ReplInput(id, "SELECT 41+1 AS answer;\r"))
	r.waitText(t, id, "42")
	prev := 0
	for _, f := range r.log(id).all() {
		assert.Greater(t, f.Seq, prev)
		prev = f.Seq
		assert.True(t, utf8.ValidString(f.Data))
		assert.LessOrEqual(t, len(f.Data), pumpFrameCap)
	}
}

func TestConsoleCtrlCCancelsARunningStatement(t *testing.T) {
	r := newConsoleRig(t)
	id := r.open(t, "")
	r.waitText(t, id, "▸")
	require.NoError(t, r.app.ReplInput(id, "SELECT count(*) FROM range(1000000000) a, range(1000000000) b;\r"))
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	require.NoError(t, r.app.ReplInput(id, "\x03"))
	r.waitText(t, id, "^C")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestConsoleCatalogChangedAfterDDL(t *testing.T) {
	r := newConsoleRig(t)
	id := r.open(t, "")
	r.waitText(t, id, "▸")
	before := r.ev.count("catalog:changed")
	require.NoError(t, r.app.ReplInput(id, "CREATE TABLE made_here(x INT);\r"))
	require.Eventually(t, func() bool { return r.ev.count("catalog:changed") > before }, 10*time.Second, 10*time.Millisecond)
}

func TestConsoleBackpressureAndResume(t *testing.T) {
	r := newConsoleRig(t)
	res, err := r.app.ReplOpen("")
	require.NoError(t, err)
	id := res.ID
	require.NoError(t, r.app.ReplAck(id, 0))
	r.waitText(t, id, "▸") // no autoAck: the banner and prompt are in flight
	require.NoError(t, r.app.ReplInput(id, ".maxrows 0\r"))
	require.NoError(t, r.app.ReplInput(id, "SELECT repeat('x', 100) AS s FROM range(200000);\r"))
	require.Eventually(t, func() bool {
		return len(r.log(id).text()) > 0
	}, 10*time.Second, 10*time.Millisecond)
	// With no acknowledgements the unacknowledged bytes stay within the window.
	first := 0
	require.Eventually(t, func() bool {
		n := len(r.log(id).text())
		stable := n == first
		first = n
		time.Sleep(300 * time.Millisecond)
		return stable && n > 0
	}, 20*time.Second, 10*time.Millisecond, "the stream is held while nothing is acknowledged")
	assert.LessOrEqual(t, first, pumpWindow+pumpFrameCap)
	go r.autoAck(id)
	require.Eventually(t, func() bool { return len(r.log(id).text()) > first+100_000 }, 20*time.Second, 20*time.Millisecond)
	require.NoError(t, r.app.ReplClose(id))
}

func TestConsoleReaderGoneEndsTheSession(t *testing.T) {
	r := newConsoleRig(t)
	r.app.consoles.limits.gone = 200 * time.Millisecond
	res, err := r.app.ReplOpen("")
	require.NoError(t, err)
	l := r.log(res.ID)
	require.Eventually(t, func() bool {
		fs := l.all()
		return len(fs) > 0 && fs[len(fs)-1].Closed
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, errReaderGone.Error(), l.all()[len(l.all())-1].Reason)
	assert.Error(t, r.app.ReplInput(res.ID, "x"))
	// the slot is free again (released as the session's goroutine ends)
	r.app.consoles.limits.gone = time.Minute
	require.Eventually(t, func() bool {
		var ids []string
		defer func() {
			for _, id := range ids {
				_ = r.app.ReplClose(id)
			}
		}()
		for range maxConsoles {
			res, err := r.app.ReplOpen("")
			if err != nil {
				return false
			}
			ids = append(ids, res.ID)
		}
		return true
	}, 5*time.Second, 100*time.Millisecond)
}

func TestConsoleLimit(t *testing.T) {
	r := newConsoleRig(t)
	for range maxConsoles {
		r.open(t, "")
	}
	_, err := r.app.ReplOpen("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "At most 4 consoles")
}

func TestConsoleBindingErrors(t *testing.T) {
	r := newConsoleRig(t)
	id := r.open(t, "")
	assert.ErrorIs(t, r.app.ReplInput(id, strings.Repeat("x", repl.MaxInput+1)), repl.ErrInputTooLarge)
	for _, bad := range []string{"", "nope", strings.Repeat("0", 32)} {
		assert.Error(t, r.app.ReplInput(bad, "x"), bad)
		assert.Error(t, r.app.ReplResize(bad, 80, 24), bad)
		assert.Error(t, r.app.ReplAck(bad, 1), bad)
		assert.Error(t, r.app.ReplClose(bad), bad)
	}
	assert.Error(t, r.app.ReplResize(id, 0, 24))
	assert.Error(t, r.app.ReplResize(id, 80, 5000))
	assert.NoError(t, r.app.ReplResize(id, 120, 40))
	_, err := r.app.ReplOpen("bad name;")
	assert.Error(t, err)
}

func TestConsoleClosedWithTheWorkspaceMidQuery(t *testing.T) {
	r := newConsoleRig(t)
	id := r.open(t, "")
	r.waitText(t, id, "▸")
	require.NoError(t, r.app.ReplInput(id, "SELECT count(*) FROM range(1000000000) a, range(1000000000) b;\r"))
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	require.NoError(t, r.app.CloseWorkspace())
	assert.Less(t, time.Since(start), 5*time.Second)
	r.app.consoles.mu.Lock()
	assert.Empty(t, r.app.consoles.sessions)
	r.app.consoles.mu.Unlock()
	fs := r.log(id).all()
	require.NotEmpty(t, fs)
	last := fs[len(fs)-1]
	assert.True(t, last.Closed)
	assert.Equal(t, "workspace closed", last.Reason)
	_, err := r.app.ReplOpen("")
	assert.ErrorIs(t, err, errNoWorkspace)
}

func TestConsoleExitClosesTheStream(t *testing.T) {
	r := newConsoleRig(t)
	id := r.open(t, "")
	r.waitText(t, id, "▸")
	require.NoError(t, r.app.ReplInput(id, ".exit\r"))
	require.Eventually(t, func() bool {
		fs := r.log(id).all()
		return len(fs) > 0 && fs[len(fs)-1].Closed
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, "exit", r.log(id).all()[len(r.log(id).all())-1].Reason)
}

func TestConsoleTabCompletesFromTheLiveCatalog(t *testing.T) {
	r := newConsoleRig(t)
	_, err := r.app.sess.DB().Exec("CREATE TABLE zebra_crossings(x INT)")
	require.NoError(t, err)
	r.app.catalogChanged("")
	id := r.open(t, "")
	r.waitText(t, id, "▸")
	require.Eventually(t, func() bool {
		l := r.app.liveCat.Load()
		return l != nil && !l.Building()
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, r.app.ReplInput(id, "SELECT * FROM zebra_cr\t"))
	r.waitText(t, id, "zebra_crossings")
}

func TestConsoleUseSwitchesAndRefuses(t *testing.T) {
	r := newConsoleRig(t)
	path := filepath.Join(t.TempDir(), "other.duckdb")
	db, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE t(x INT); INSERT INTO t VALUES (7)")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, r.app.sess.Register(session.Spec{Name: "other", Type: "duckdb", Path: path}))

	id := r.open(t, "")
	r.waitText(t, id, "▸")
	cur, err := r.app.ReplUse(id, "other")
	require.NoError(t, err)
	assert.Equal(t, "other", cur)
	r.waitText(t, id, "current: other")
	require.Eventually(t, func() bool { return r.ev.count("repl:conn:"+id) >= 1 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, r.app.ReplInput(id, "SELECT x FROM t;\r"))
	r.waitText(t, id, "7")

	// back to the default with ""
	_, err = r.app.ReplUse(id, "")
	require.NoError(t, err)

	// half a line typed: refused, in words, and nothing changes
	require.NoError(t, r.app.ReplInput(id, "select 1"))
	_, err = r.app.ReplUse(id, "other")
	require.Error(t, err)
	assert.Equal(t, "finish the statement or press Ctrl+C first", err.Error())

	_, err = r.app.ReplUse("nope", "other")
	assert.Error(t, err)
	_, err = r.app.ReplUse(id, "bad name;")
	assert.Error(t, err)
}
