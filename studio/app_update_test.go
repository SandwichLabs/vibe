package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/selfupdate"
)

func updateTestApp(t *testing.T) (*App, *[]string) {
	t.Helper()
	rec := prefs.NewRecents(prefs.NewStore(filepath.Join(t.TempDir(), "cfg")), nil)
	app := &App{logger: testLogger()}
	app.recents = rec
	app.updates = selfupdate.NewService(selfupdate.New("v0.1.1"), rec, nil)
	var started []string
	app.relaunch = func(_ string, target string, args ...string) error {
		started = append(started, append([]string{target}, args...)...)
		return nil
	}
	return app, &started
}

func TestUpdateBindings_AskOnceThenRemember(t *testing.T) {
	app, _ := updateTestApp(t)
	assert.True(t, app.GetUpdateState().NeedsConsent)
	require.NoError(t, app.SetUpdateCheck("weekly"))
	st := app.GetUpdateState()
	assert.False(t, st.NeedsConsent)
	assert.Equal(t, "weekly", st.Check)
	assert.Error(t, app.SetUpdateCheck("sometimes"))
	require.NoError(t, app.SkipUpdate("v0.1.2"))
	assert.Equal(t, "v0.1.2", app.GetUpdateState().SkippedVersion)
}

func TestRestartAfterUpdate_StartsTheNewStudioOnlyAfterTheQuitIsCommitted(t *testing.T) {
	app, started := updateTestApp(t)
	quits := 0
	app.quitFn = func() { quits++ }

	assert.ErrorIs(t, app.RestartAfterUpdate(), selfupdate.ErrNothingToRestart)
	assert.Zero(t, quits)

	app.restart.prepare("/home/me/.local/bin/vibe-studio")
	require.NoError(t, app.RestartAfterUpdate())
	assert.Equal(t, 1, quits, "the normal quit path is asked for")
	assert.Empty(t, *started, "nothing is started until shutdown")

	// The person cancels the quit (a notebook could not be saved): no second instance.
	app.CancelQuit()
	app.relaunchAfterQuit("/work")
	assert.Empty(t, *started)

	// Arm again; this time the quit goes through and shutdown starts it, reopening the workspace.
	require.NoError(t, app.RestartAfterUpdate())
	app.relaunchAfterQuit("/work")
	assert.Equal(t, []string{"/home/me/.local/bin/vibe-studio", "/work"}, *started)

	// A later ordinary quit starts nothing.
	*started = nil
	app.relaunchAfterQuit("/work")
	assert.Empty(t, *started)
}

func TestBeforeClose_HoldsWhileAnUpdateInstalls(t *testing.T) {
	app, _ := updateTestApp(t)
	var events []string
	app.emit = func(name string, _ ...any) { events = append(events, name) }
	app.quitFn = func() {}
	armed := false
	app.closeWatchdog = func(time.Duration, func()) func() { armed = true; return func() {} }

	app.installing.Store(true)
	assert.True(t, app.beforeClose(context.Background()), "the close is held")
	assert.True(t, app.beforeClose(context.Background()), "and again on the next attempt")
	assert.Equal(t, []string{"app:update-installing", "app:update-installing"}, events)
	assert.False(t, armed, "no quit watchdog while installing")

	app.installing.Store(false)
	assert.True(t, app.beforeClose(context.Background()), "afterwards the normal first-attempt flow runs")
	assert.Contains(t, events, "app:before-close")
}
