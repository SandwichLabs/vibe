package repl

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const longQuery = "SELECT count(*) FROM range(10000000000) t1, range(1000) t2;\r"

func (f *fixture) waitRunning() {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !f.s.Running() {
		if time.Now().After(deadline) {
			f.t.Fatal("the statement never started")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestCtrlCInterruptsRange(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.run("CREATE TEMP TABLE keep(x INT);\r")
	f.run("INSERT INTO keep VALUES (1), (2);\r")

	before := strings.Count(f.out.String(), prompt)
	f.typ(longQuery)
	f.waitRunning()
	start := time.Now()
	require.NoError(t, f.s.Input([]byte{0x03}))
	f.waitCount(prompt, before+1)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Contains(t, f.out.String(), "^C")
	assert.False(t, f.s.Running())

	// The pinned connection survived: its TEMP table and a new statement work.
	f.run("SELECT 42 AS answer;\r")
	assert.Contains(t, f.out.String(), "answer")
	f.run("SELECT count(*) AS n FROM keep;\r")
	assert.Contains(t, f.out.String(), "│     2 │")
}

func TestCtrlCIdleClearsLine(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.typ("select 1,\r")
	f.waitFor("· ")
	assert.NotEmpty(t, f.s.Buffered())
	f.typ("sel")
	n := strings.Count(f.out.String(), prompt)
	require.NoError(t, f.s.Input([]byte{0x03}))
	f.waitCount(prompt, n+1)
	assert.Empty(t, f.s.Buffered())

	// What was half typed is gone: this runs as a fresh statement.
	f.run("select 42 as answer;\r")
	assert.Contains(t, f.out.String(), "answer")
	assert.NotContains(t, f.out.String(), "Error:")
	assert.NotContains(t, f.out.String(), "", "the interrupt marker is never shown")
}

func TestCtrlDExits(t *testing.T) {
	var exits atomic.Int32
	f := newSession(t, func(o *Options) { o.Hooks.Exit = func() { exits.Add(1) } })
	f.waitFor(prompt)
	require.NoError(t, f.s.Input([]byte{0x04}))
	select {
	case <-f.s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+D did not end the session")
	}
	assert.Eventually(t, func() bool { return exits.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.NoError(t, f.s.Err())
	assert.ErrorIs(t, f.s.Input([]byte("x")), ErrClosed)
}

func TestExitCommandExitsOnce(t *testing.T) {
	var exits atomic.Int32
	f := newSession(t, func(o *Options) { o.Hooks.Exit = func() { exits.Add(1) } })
	f.waitFor(prompt)
	f.typ(".exit\r")
	select {
	case <-f.s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal(".exit did not end the session")
	}
	assert.Eventually(t, func() bool { return exits.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, f.s.Close())
	time.Sleep(50 * time.Millisecond)
	assert.EqualValues(t, 1, exits.Load(), "Close after the end does not call it again")
}

func TestCloseDuringRun(t *testing.T) {
	var exits atomic.Int32
	f := newSession(t, func(o *Options) { o.Hooks.Exit = func() { exits.Add(1) } })
	f.waitFor(prompt)
	f.typ(longQuery)
	f.waitRunning()
	done := make(chan struct{})
	go func() { _ = f.s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung while a statement ran")
	}
	assert.Zero(t, exits.Load(), "closing is not the person leaving")
	assert.ErrorIs(t, f.s.Input([]byte("x")), ErrClosed)
}

func TestResizeWhileRunning(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.typ(longQuery)
	f.waitRunning()
	for i := 0; i < 50; i++ {
		f.s.Resize(40+i, 20)
	}
	require.NoError(t, f.s.Input([]byte{0x03}))
	f.waitCount(prompt, 2)
}

func TestInputLimits(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	assert.ErrorIs(t, f.s.Input(make([]byte, MaxInput+1)), ErrInputTooLarge)

	// With the editor busy nothing reads the queue: it fills, then refuses.
	f.typ(longQuery)
	f.waitRunning()
	chunk := []byte(strings.Repeat("a", MaxInput))
	var err error
	for i := 0; i < 5 && err == nil; i++ {
		err = f.s.Input(chunk)
	}
	assert.ErrorIs(t, err, ErrInputOverflow)
	require.NoError(t, f.s.Input([]byte{0x03}), "Ctrl+C still gets through a full queue")
}

func TestUserCannotTypeTheInterruptMarker(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.run("select 'ab' as v;\r")
	assert.Contains(t, f.out.String(), "│ ab ")
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(2)
	mk := func(o *Options) { o.Limiter = l }
	a := newSession(t, mk)
	b := newSession(t, mk)
	_, err := New(t.Context(), &syncBuffer{}, Options{Engine: a.eng, Sess: a.sess, Limiter: l})
	assert.ErrorIs(t, err, ErrTooManySessions)
	require.NoError(t, a.s.Close())
	c, err := New(t.Context(), &syncBuffer{}, Options{Engine: a.eng, Sess: a.sess, Limiter: l})
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NoError(t, b.s.Close())
}

func TestQueueRefusesPastItsCap(t *testing.T) {
	q := newInputQueue()
	require.NoError(t, q.write(make([]byte, maxQueued)))
	assert.ErrorIs(t, q.write([]byte{1}), ErrInputOverflow)
	q.close()
	_, err := q.Read(make([]byte, 1))
	assert.Error(t, err)
}
