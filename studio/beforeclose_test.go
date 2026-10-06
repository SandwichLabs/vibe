package main

// beforeClose/Quit watchdog tests (review fix, step 4a round 2
// #1 and round 3 #1; #111 dropped the dirty payload and CancelQuit). Quit()
// itself would otherwise call the real Wails
// runtime.Quit, which needs a live window context these unit tests don't
// have (the same reason emitEvent guards on a.emit rather than calling
// runtime.EventsEmit directly under test) — so every test here injects
// quitFn (and, where relevant, closeWatchdog) rather than exercising the
// real Wails runtime.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApp_BeforeClose_Gating(t *testing.T) {
	var events []string
	var quitCalls int
	app := &App{logger: testLogger()}
	app.emit = func(name string, _ ...any) {
		events = append(events, name)
	}
	app.quitFn = func() { quitCalls++ }
	// A watchdog that is armed but never fires on its own — this test
	// drives the flag through Quit(), not the timeout.
	app.closeWatchdog = func(time.Duration, func()) func() { return func() {} }

	// First attempt: not yet confirmed -> prevents the close and emits the
	// event the frontend listens for to start its flush.
	prevent := app.beforeClose(context.Background())
	assert.True(t, prevent, "the first close attempt must be prevented")
	assert.Contains(t, events, "app:before-close")
	assert.Equal(t, 0, quitCalls)

	// The frontend finishes flushing and calls Quit().
	app.Quit()
	assert.Equal(t, 1, quitCalls)
	assert.True(t, app.quitConfirmed.Load())

	// A later attempt: now confirmed -> lets the close through.
	events = nil
	prevent2 := app.beforeClose(context.Background())
	assert.False(t, prevent2, "a close attempt after Quit() must be allowed through")
	assert.Empty(t, events, "a confirmed close must not re-emit app:before-close")
}

func TestApp_BeforeClose_DefaultsToPreventing(t *testing.T) {
	// A fresh App (quitConfirmed defaults to its zero value, false) must
	// always prevent the very first close attempt.
	app := &App{logger: testLogger()}
	app.emit = func(string, ...any) {}
	app.quitFn = func() {}
	app.closeWatchdog = func(time.Duration, func()) func() { return func() {} }
	assert.True(t, app.beforeClose(context.Background()))
}

// TestApp_BeforeClose_WatchdogQuitsWhenFrontendNeverResponds (review fix,
// step 4a round 3 #1, case 1): a hung/suspended webview never calls Quit()
// in response to "app:before-close". The watchdog armed on the first
// attempt must fire and quit anyway. The injected closeWatchdog here
// invokes its callback immediately (standing in for "the real 5s timer
// eventually elapsed") rather than actually waiting.
func TestApp_BeforeClose_WatchdogQuitsWhenFrontendNeverResponds(t *testing.T) {
	var quitCalls int
	var gotTimeout time.Duration
	app := &App{logger: testLogger()}
	app.emit = func(string, ...any) {}
	app.quitFn = func() { quitCalls++ }
	app.closeWatchdog = func(d time.Duration, f func()) func() {
		gotTimeout = d
		f() // simulate the timer elapsing — the frontend never called Quit()
		return func() {}
	}

	prevent := app.beforeClose(context.Background())
	assert.True(t, prevent, "the first attempt is still prevented — the watchdog firing is what actually quits")
	assert.Equal(t, closeWatchdogTimeout, gotTimeout)
	require.Equal(t, 1, quitCalls, "the watchdog must call Quit() when the frontend never responds")
	assert.True(t, app.quitConfirmed.Load())
}

// TestApp_BeforeClose_SecondAttemptQuitsImmediately (review fix, step 4a
// round 3 #1, case 2): a second close attempt (e.g. the person clicks
// close/quit again) while the first is still pending — the watchdog has
// not fired and the frontend has not called Quit() — must let the close
// through immediately rather than waiting out the full watchdog window.
func TestApp_BeforeClose_SecondAttemptQuitsImmediately(t *testing.T) {
	var quitCalls int
	var watchdogArmed int
	app := &App{logger: testLogger()}
	app.emit = func(string, ...any) {}
	app.quitFn = func() { quitCalls++ }
	app.closeWatchdog = func(time.Duration, func()) func() {
		watchdogArmed++
		// Deliberately never invoke the callback — simulates the timer
		// still pending, not yet elapsed.
		return func() {}
	}

	prevent1 := app.beforeClose(context.Background())
	assert.True(t, prevent1, "the first attempt is prevented while the flush is pending")
	assert.Equal(t, 1, watchdogArmed)
	assert.False(t, app.quitConfirmed.Load())

	prevent2 := app.beforeClose(context.Background())
	assert.False(t, prevent2, "a second attempt while the first is still pending must go through immediately")
	assert.True(t, app.quitConfirmed.Load())
	// The watchdog must not be armed a second time — repeated close
	// attempts must never stack multiple timers.
	assert.Equal(t, 1, watchdogArmed)
	// Letting the close through here is via beforeClose's own return value
	// (prevent=false), not by calling quitFn — Wails itself proceeds with
	// the close when beforeClose returns false.
	assert.Equal(t, 0, quitCalls)
}

// Choosing "Stay" at the "Quit anyway?" prompt (or merely being asked) must stop
// the hung-webview watchdog: a real timer elapsing must not quit the app.
func TestApp_HoldAndCancelQuit_StopTheWatchdog(t *testing.T) {
	for _, stopper := range []string{"HoldQuit", "CancelQuit"} {
		t.Run(stopper, func(t *testing.T) {
			var quitCalls atomic.Int32
			app := &App{logger: testLogger()}
			app.emit = func(string, ...any) {}
			app.quitFn = func() { quitCalls.Add(1) }
			app.closeWatchdog = func(d time.Duration, f func()) func() {
				tm := time.AfterFunc(30*time.Millisecond, f) // a short real timer
				return func() { tm.Stop() }
			}
			require.True(t, app.beforeClose(context.Background()))
			if stopper == "HoldQuit" {
				app.HoldQuit()
			} else {
				app.CancelQuit()
			}
			time.Sleep(150 * time.Millisecond)
			assert.Zero(t, quitCalls.Load(), "the watchdog must not quit after %s", stopper)

			// Without it the timer does quit (the control).
			app2 := &App{logger: testLogger()}
			app2.emit = func(string, ...any) {}
			var q2 atomic.Int32
			app2.quitFn = func() { q2.Add(1) }
			app2.closeWatchdog = app.closeWatchdog
			require.True(t, app2.beforeClose(context.Background()))
			time.Sleep(150 * time.Millisecond)
			assert.Equal(t, int32(1), q2.Load())
		})
	}
}
