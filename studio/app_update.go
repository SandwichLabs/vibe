package main

import (
	"runtime"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/SandwichLabs/vibe/internal/selfupdate"
)

// --- Updates (Settings > About > Updates, the first-run prompt) ---
//
// Nothing here talks to the network on its own: the frontend decides when to ask (once, on a
// release build; weekly when the person chose it) and what to install. The logic lives in
// internal/selfupdate; these are thin bindings.

// restartRequest is the pending "Restart now": the new Studio to start once this one has quit.
type restartRequest struct {
	mu       sync.Mutex
	prepared string // from the last successful ApplyUpdate (never from the frontend)
	onQuit   string // armed by RestartAfterUpdate; cleared by CancelQuit
}

func (r *restartRequest) prepare(target string) { r.mu.Lock(); r.prepared = target; r.mu.Unlock() }

// arm moves the prepared target into "start on quit"; "" when nothing was installed.
func (r *restartRequest) arm() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onQuit = r.prepared
	return r.onQuit
}

func (r *restartRequest) disarm() { r.mu.Lock(); r.onQuit = ""; r.mu.Unlock() }

func (r *restartRequest) take() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.onQuit
	r.onQuit = ""
	return t
}

// GetUpdateState returns the saved update choice and install kind (no network).
func (a *App) GetUpdateState() selfupdate.State { return a.updates.State() }

// SetUpdateCheck saves the answer to "Check GitHub for new releases?": "off" or "weekly".
func (a *App) SetUpdateCheck(mode string) error { return a.updates.SetCheck(mode) }

// CheckForUpdate is "Check now": one explicit request, whatever the saved choice is.
func (a *App) CheckForUpdate() (selfupdate.Result, error) {
	return a.updates.CheckNow(a.ctxOrBackground())
}

// AutoCheckForUpdate runs the weekly startup check. It returns null when none was due (not
// weekly, or checked within a week, or a dev build); a failure is logged and also returns null.
func (a *App) AutoCheckForUpdate() *selfupdate.Result {
	return a.updates.AutoCheck(a.ctxOrBackground())
}

// SkipUpdate remembers a release as skipped ("" clears it).
func (a *App) SkipUpdate(tag string) error { return a.updates.Skip(tag) }

// ApplyUpdate installs tag by running that release's verified install.sh. The person confirmed in
// the UI; the running Studio keeps its old version until RestartAfterUpdate.
func (a *App) ApplyUpdate(tag string) (selfupdate.ApplyResult, error) {
	a.installing.Store(true)
	defer a.installing.Store(false)
	res, err := a.updates.Apply(a.ctxOrBackground(), tag)
	if err == nil {
		a.restart.prepare(res.Restart)
	}
	return res, err
}

// RestartAfterUpdate quits through the normal path (pending saves are flushed and the unsaved-work
// question is asked as always). The new Studio starts from shutdown, after the workspace and its
// lock are released; CancelQuit abandons the restart too.
func (a *App) RestartAfterUpdate() error {
	if a.restart.arm() == "" {
		return selfupdate.ErrNothingToRestart
	}
	if a.quitFn != nil {
		a.quitFn()
		return nil
	}
	wruntime.Quit(a.ctx)
	return nil
}

// relaunchAfterQuit starts the new Studio, reopening workspaceRoot, when a restart was armed. It is
// called from shutdown once the workspace is closed and the hub lock released.
func (a *App) relaunchAfterQuit(workspaceRoot string) {
	target := a.restart.take()
	if target == "" || a.relaunch == nil {
		return
	}
	var args []string
	if workspaceRoot != "" {
		args = append(args, workspaceRoot)
	}
	if err := a.relaunch(runtime.GOOS, target, args...); err != nil {
		a.logger.Warn("could not start the updated Studio", "error", err)
	}
}
