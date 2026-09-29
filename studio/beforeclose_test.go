package main

// beforeClose/Quit/CancelQuit watchdog tests (review fix, step 4a round 2
// #1 and round 3 #1; extended #81 for the dirty payload/closePending/
// CancelQuit). Quit() itself would otherwise call the real Wails
// runtime.Quit, which needs a live window context these unit tests don't
// have (the same reason emitEvent guards on a.emit rather than calling
// runtime.EventsEmit directly under test) — so every test here injects
// quitFn (and, where relevant, closeWatchdog) rather than exercising the
// real Wails runtime.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// beforeCloseEvents pulls the "app:before-close" payloads recorded by an
// app.emit sink, in order.
func lastBeforeClosePayload(events []map[string]any) map[string]any {
	if len(events) == 0 {
		return nil
	}
	return events[len(events)-1]
}

// recordingEmit builds an app.emit sink that appends every "app:before-close"
// event's payload (as a map) to payloads, and every event name to names.
func recordingEmit(names *[]string, payloads *[]map[string]any) func(string, ...any) {
	return func(name string, data ...any) {
		*names = append(*names, name)
		if name != "app:before-close" || len(data) == 0 {
			return
		}
		if m, ok := data[0].(map[string]any); ok {
			*payloads = append(*payloads, m)
		}
	}
}

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
	app.closeWatchdog = func(time.Duration, func()) {}

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
	app.closeWatchdog = func(time.Duration, func()) {}
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
	app.closeWatchdog = func(d time.Duration, f func()) {
		gotTimeout = d
		f() // simulate the timer elapsing — the frontend never called Quit()
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
	app.closeWatchdog = func(time.Duration, func()) {
		watchdogArmed++
		// Deliberately never invoke the callback — simulates the timer
		// still pending, not yet elapsed.
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

// TestApp_BeforeClose_PayloadCarriesDirty (#81): the "app:before-close"
// payload's "dirty" field reflects the open container's Dirty() at the
// moment of the attempt — false for a clean app, true once a table write
// has marked the container dirty.
func TestApp_BeforeClose_PayloadCarriesDirty(t *testing.T) {
	var payloads []map[string]any
	app := newOpenApp(t)
	app.emit = recordingEmit(new([]string), &payloads)
	app.quitFn = func() {}
	app.closeWatchdog = func(time.Duration, func()) {}

	app.beforeClose(context.Background())
	require.Len(t, payloads, 1)
	assert.Equal(t, false, lastBeforeClosePayload(payloads)["dirty"])

	app.CancelQuit()
	require.True(t, app.container.MarkDirty())

	app.beforeClose(context.Background())
	require.Len(t, payloads, 2)
	assert.Equal(t, true, lastBeforeClosePayload(payloads)["dirty"])
}

// TestApp_BeforeClose_DirtyDoesNotArmWatchdog (#81): a hung webview escape
// hatch (the watchdog) must not fire while a Save/Don't save/Cancel prompt
// may legitimately be sitting open waiting on the person — the SECOND close
// attempt is the only escape hatch in that case (asserted separately below).
func TestApp_BeforeClose_DirtyDoesNotArmWatchdog(t *testing.T) {
	var watchdogArmed int
	app := newOpenApp(t)
	app.emit = func(string, ...any) {}
	app.quitFn = func() {}
	app.closeWatchdog = func(time.Duration, func()) { watchdogArmed++ }
	require.True(t, app.container.MarkDirty())

	prevent := app.beforeClose(context.Background())
	assert.True(t, prevent)
	assert.Equal(t, 0, watchdogArmed, "a dirty close must not arm the clean-case watchdog")
}

// TestApp_CancelQuit_ReattemptPromptsAgain (#81): after Cancel, closePending
// is cleared so the NEXT close/quit attempt starts the dance over — a fresh
// "app:before-close" emission — rather than immediately quitting the way a
// second attempt while one is still pending would.
func TestApp_CancelQuit_ReattemptPromptsAgain(t *testing.T) {
	var names []string
	app := &App{logger: testLogger()}
	app.emit = func(name string, _ ...any) { names = append(names, name) }
	app.quitFn = func() {}
	app.closeWatchdog = func(time.Duration, func()) {}

	prevent1 := app.beforeClose(context.Background())
	assert.True(t, prevent1)
	assert.Equal(t, 1, len(names))

	app.CancelQuit()
	assert.False(t, app.quitConfirmed.Load(), "CancelQuit must never confirm the quit")

	prevent2 := app.beforeClose(context.Background())
	assert.True(t, prevent2, "a fresh attempt after CancelQuit must prompt again, not quit immediately")
	assert.Equal(t, 2, len(names), "CancelQuit must let the next attempt re-emit app:before-close")
}

// TestApp_BeforeClose_TryLockFailureFailsTowardDirty (#81): dirtyForClose
// must never block the close-attempt path waiting on a.mu — a long-running
// call already holding the lock makes it fail toward "dirty" (an extra
// prompt, never a silently skipped one) instead of deadlocking or blocking.
func TestApp_BeforeClose_TryLockFailureFailsTowardDirty(t *testing.T) {
	var payloads []map[string]any
	app := &App{logger: testLogger()}
	app.emit = recordingEmit(new([]string), &payloads)
	app.quitFn = func() {}
	app.closeWatchdog = func(time.Duration, func()) {}

	app.mu.Lock()
	defer app.mu.Unlock()

	done := make(chan bool, 1)
	go func() {
		done <- app.beforeClose(context.Background())
	}()

	select {
	case prevent := <-done:
		assert.True(t, prevent)
	case <-time.After(2 * time.Second):
		t.Fatal("beforeClose deadlocked while a.mu was held elsewhere")
	}
	require.Len(t, payloads, 1)
	assert.Equal(t, true, lastBeforeClosePayload(payloads)["dirty"])
}
