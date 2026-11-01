package main

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/artifacts"
	"github.com/SandwichLabs/assay/internal/pagehost"
)

// exitSelf is the helper exiting by its own means: its pipes close and Done
// closes, with no kill from Studio.
func (f *fakeHelper) exitSelf() {
	f.once.Do(func() {
		close(f.done)
		_ = f.fromHelperW.Close()
		_ = f.toHelperW.Close()
	})
}

// exitingHelper is a fakeHelper that reports an exit code, as the real one does.
type exitingHelper struct {
	*fakeHelper
	code int
}

func (e *exitingHelper) ExitCode() int { return e.code }

func TestPageWindow_ClosingFromStudioAsksTheHelperToQuitBeforeKilling(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.quitGrace = 150 * time.Millisecond
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	app.ClosePageWindow("pg")
	ev.waitFor(t, pageStateClosed)
	fh.until(t, typed(pagehost.TypeQuit))
	assert.False(t, fh.killed.Load(), "a helper that is exiting is given a moment, not killed at once")
	assert.Eventually(t, fh.killed.Load, 3*time.Second, 10*time.Millisecond, "a helper that does not exit is killed after the grace")
}

func TestPageWindow_OnlyThePersonsCloseIsGraceful(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.quitGrace = time.Hour
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	_, err = app.RevokeArtifactView("pg")
	require.NoError(t, err)
	ev.waitFor(t, pageStateChanged)
	assert.True(t, fh.killed.Load(), "a revoke ends the window at once, with no grace")
}

func TestPageWindow_ASetupFailureIsNotAHang(t *testing.T) {
	for _, c := range []struct {
		code int
		want string
	}{
		{helperRulesFailed, pageStateSetup},
		{helperNoProxyPort, pageStateSetup},
		{-1, pageStateStopped},
		{helperBadSetup, pageStateStopped},
		{0, pageStateStopped},
	} {
		t.Run(strconv.Itoa(c.code), func(t *testing.T) {
			app, fh, ev := scriptedWindow(t)
			eh := &exitingHelper{fakeHelper: fh, code: c.code}
			app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { return eh, nil }
			_, err := app.OpenArtifactPageWindow("pg", "", nil)
			require.NoError(t, err)
			fh.exitSelf()
			ev.waitFor(t, c.want)
			assert.Len(t, ev.states(), 1, "exit code %d: one end, and it is %s", c.code, c.want)
		})
	}
}

func TestPageWindow_AWatchdogKillKeepsStoppedEvenIfTheCodeLooksLikeSetup(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.grace = 200 * time.Millisecond
	eh := &exitingHelper{fakeHelper: fh, code: helperRulesFailed}
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { return eh, nil }
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	ev.waitFor(t, pageStateStopped)
	assert.NotContains(t, ev.states(), pageStateSetup, "the watchdog ended it first")
}

func TestPageWindow_ARevokeDuringTheCloseGraceKillsAtOnce(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.quitGrace = time.Hour
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	app.ClosePageWindow("pg")
	ev.waitFor(t, pageStateClosed)
	fh.until(t, typed(pagehost.TypeQuit))
	assert.False(t, fh.killed.Load(), "within the grace it is not killed yet")
	_, err = app.RevokeArtifactView("pg")
	require.NoError(t, err)
	assert.Eventually(t, fh.killed.Load, 2*time.Second, 5*time.Millisecond, "a revoke ends a closing window now")
}

func TestPageWindow_CloseAllDuringTheCloseGraceKillsAtOnce(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.quitGrace = time.Hour
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	app.ClosePageWindow("pg")
	ev.waitFor(t, pageStateClosed)
	assert.False(t, fh.killed.Load())
	app.pages.closeAll()
	assert.Eventually(t, fh.killed.Load, 2*time.Second, 5*time.Millisecond)
}

func TestPageWindow_AClosingWindowChangesNothing(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.quitGrace = time.Hour
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	w := app.pages.wins["pg"]
	require.NotNil(t, w)
	w.caps[artifacts.CapState] = true // as if the page had declared it
	app.ClosePageWindow("pg")
	ev.waitFor(t, pageStateClosed)
	w.handlePut(&pagehost.Message{T: pagehost.TypePut, ID: 7, Up: 1, Seq: 0, Text: "x", Last: true})
	w.handleCall(&pagehost.Message{T: pagehost.TypeCall, ID: 8, Cap: "state", Op: "set", Arg: []byte(`{"key":"k","value":1}`)})
	for _, id := range []int64{7, 8} {
		id := id
		m := fh.until(t, func(m *pagehost.Message) bool { return m.T == pagehost.TypeReply && m.ID == id })
		require.NotNil(t, m.OK)
		assert.False(t, *m.OK)
		assert.Equal(t, pagehost.CodeRefused, m.Code)
	}
	assert.Nil(t, w.uploads[1], "no upload was taken")
	app.pages.closeAll()
}
