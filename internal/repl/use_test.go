package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustUse(t *testing.T, f *fixture, name string) string {
	t.Helper()
	cur, err := f.s.Use(name)
	require.NoError(t, err)
	return cur
}

func TestUseSwitchesAtAnEmptyPrompt(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	assert.Equal(t, "other", mustUse(t, f, "other"))
	f.waitCount(prompt, 2)
	assert.Contains(t, f.out.String(), "current: other")
	assert.Equal(t, "other", f.s.Current())
	f.run("SELECT x FROM t;\r") // plain table names now resolve on the new connection
	f.waitFor("7")
}

func TestUseEmptyMeansTheDefault(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	mustUse(t, f, "other")
	assert.Equal(t, f.sess.DefaultCatalog(), mustUse(t, f, ""))
	assert.Equal(t, f.sess.DefaultCatalog(), f.s.Current())
}

func TestUseRefusesMidStatement(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	f.typ("select 1,\r")
	f.waitFor("· ")
	_, err := f.s.Use("other")
	assert.ErrorIs(t, err, ErrUseMidStatement)
	f.run("2;\r")
	assert.NotContains(t, f.out.String(), "Error")
	assert.NotContains(t, f.out.String(), "")
}

func TestUseRefusesWithTextOnTheLine(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	f.typ("select 1")
	_, err := f.s.Use("other")
	assert.ErrorIs(t, err, ErrUseLineNotEmpty)
	f.run(";\r") // the typed text is intact: it ran as one statement
	assert.NotContains(t, f.out.String(), "Error")
	assert.Equal(t, f.sess.DefaultCatalog(), f.s.Current())
	mustUse(t, f, "other")
}

// The editor's real line decides, not the bytes that were typed.
func TestUseAllowsALineThatIsEmptyAgain(t *testing.T) {
	t.Run("backspaced to empty", func(t *testing.T) {
		f := newSession(t, nil)
		f.lazyFile("other")
		f.waitFor(prompt)
		f.typ("abc\x7f\x7f\x7f")
		assert.Equal(t, "other", mustUse(t, f, "other"))
	})
	t.Run("history up then down", func(t *testing.T) {
		f := newSession(t, nil)
		f.lazyFile("other")
		f.waitFor(prompt)
		f.run("select 1;\r")
		f.typ("\x1b[A\x1b[B")
		assert.Equal(t, "other", mustUse(t, f, "other"))
	})
	t.Run("empty bracketed paste", func(t *testing.T) {
		f := newSession(t, nil)
		f.lazyFile("other")
		f.waitFor(prompt)
		f.typ("\x1b[200~\x1b[201~")
		assert.Equal(t, "other", mustUse(t, f, "other"))
	})
	t.Run("Ctrl+C cleared it", func(t *testing.T) {
		f := newSession(t, nil)
		f.lazyFile("other")
		f.waitFor(prompt)
		f.typ("abc")
		f.typ("\x03")
		f.waitCount(prompt, 2)
		assert.Equal(t, "other", mustUse(t, f, "other"))
	})
}

func TestUseRefusesWhileAStatementRuns(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	f.typ("SELECT count(*) FROM range(1000000000) a, range(1000000000) b;\r")
	require.Eventually(t, func() bool { return f.s.running.Load() }, 5*time.Second, 5*time.Millisecond)
	_, err := f.s.Use("other")
	assert.ErrorIs(t, err, ErrUseBusy)
	f.typ("\x03")
	f.waitFor("^C")
}

func TestUseUnknownConnection(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	_, err := f.s.Use("nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown connection")
}

func TestUseTellsTheSurfaceAndHandTypedConnToo(t *testing.T) {
	var names []string
	f := newSession(t, func(o *Options) { o.Hooks.Conn = func(n string) { names = append(names, n) } })
	f.lazyFile("other")
	f.waitFor(prompt)
	mustUse(t, f, "other")
	f.run(".conn " + f.sess.DefaultCatalog() + "\r")
	assert.Equal(t, []string{"other", f.sess.DefaultCatalog()}, names)
}

// Ctrl+C at an idle prompt is never refused, even with the queue full.
func TestIdleCtrlCFlushesAFullQueue(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	// Stop the loop from draining: it is busy with a statement.
	f.typ("SELECT count(*) FROM range(1000000000) a, range(1000000000) b;\r")
	require.Eventually(t, func() bool { return f.s.running.Load() }, 5*time.Second, 5*time.Millisecond)
	filler := strings.Repeat("x", MaxInput)
	for i := 0; i < 8; i++ {
		if err := f.s.Input([]byte(filler)); err != nil {
			require.ErrorIs(t, err, ErrInputOverflow)
			break
		}
	}
	require.NoError(t, f.s.Input([]byte("\x03")), "Ctrl+C is never refused")
	f.waitFor("^C")
	// now idle again with a stuffed queue
	f.waitCount(prompt, 2)
	for i := 0; i < 8; i++ {
		if f.s.Input([]byte(filler)) != nil {
			break
		}
	}
	require.NoError(t, f.s.Input([]byte("\x03")))
}

// Ctrl+C in the middle of a bracketed paste ends the paste: the console does
// not stay in paste mode with its keys swallowed.
func TestCtrlCMidPasteEndsThePaste(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.typ("\x1b[200~INSERT INTO t VALUES (1),\r(2),")
	f.typ("\x03")
	f.waitCount(prompt, 2)
	f.typ("\x7f\x7f\x7f\x03") // paste mode is over: these act as keys again
	f.waitCount(prompt, 3)
	f.run("select 42 AS after;\r")
	f.waitFor("42")
	assert.NotContains(t, f.out.String(), "Error")
	assert.False(t, f.s.flt.snapshot().paste)
}

func TestUseDuringAPasteRefusesAndNeverLeaksIntoTheStatement(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	f.typ("\x1b[200~select 'a")
	_, err := f.s.Use("other")
	assert.ErrorIs(t, err, ErrUseBusy)
	f.typ("b' AS v;\x1b[201~\r")
	f.waitCount(prompt, 2)
	f.waitFor("ab")
	assert.NotContains(t, f.out.String(), "")
}

func TestStrayUseRuneNeverReachesSQL(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	require.NoError(t, f.s.in.write([]byte("select 'ab' AS v;\r"))) // bypasses the input filter
	f.waitCount(prompt, 2)
	f.waitFor("ab")
}

func TestCtrlCAnswersAPendingUseAtOnce(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)
	f.typ("SELECT count(*) FROM range(1000000000) a, range(1000000000) b;\r")
	require.Eventually(t, func() bool { return f.s.running.Load() }, 5*time.Second, 5*time.Millisecond)
	// Simulate a request that was queued behind the statement.
	req := &useRequest{name: "other", reply: make(chan useResult, 1)}
	f.s.useMu.Lock()
	f.s.useReq = req
	f.s.useMu.Unlock()
	f.typ("\x03")
	select {
	case r := <-req.reply:
		assert.ErrorIs(t, r.err, ErrUseBusy)
	case <-time.After(time.Second):
		t.Fatal("the pending Use was not answered")
	}
	f.waitFor("^C")
}
