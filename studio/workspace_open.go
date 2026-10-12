package main

// Workspace lifecycle and bindings (#111): open / switch / close, the hub lock
// (one writer per workspace) and the file watcher.
//
// Everything here follows the order the hub requires:
//
//	open:  hub.Open (workspace.Open -> hub lock -> DuckDB session -> engine)
//	close: MCP stop -> watcher stop -> session close -> hub release -> workspace close
//
// The caller of every *Locked function holds a.mu for writing.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// releaseGrace is how long a hub release request waits, after telling the
// frontend, for it to flush open notebooks before the workspace is closed.
const releaseGrace = 400 * time.Millisecond

// legacyVibeMessage is what Studio says about a .vibe file.
const legacyVibeMessage = ".vibe files are bundles: use Import .vibe… to bring one into a workspace. Open a workspace folder to work in it."

// HolderInfo says which process owns the workspace's hub.
type HolderInfo struct {
	App       string `json:"app"`
	PID       int    `json:"pid"`
	ServesMCP bool   `json:"servesMcp"`
	Text      string `json:"text"` // "vibe mcp serve (pid 4182)"
}

// OpenWorkspaceResult is what open, create and switch return.
type OpenWorkspaceResult struct {
	// Status is "opened" or "not-workspace" (the folder has no vibe.yaml: the
	// UI asks "Make this folder a workspace?" and calls CreateWorkspace in
	// place).
	Status   string         `json:"status"`
	Path     string         `json:"path"`
	Info     *WorkspaceInfo `json:"info,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}

type openMode int

const (
	openAuto     openMode = iota // take the hub; read-only when someone else has it
	openReadOnly                 // never take the hub
	openTakeOver                 // ask the current owner to let go, then take it
)

// OpenWorkspace opens the workspace folder at path (or the one that contains
// it). When another process owns the workspace's database the folder opens
// read-only and the event "workspace:locked" says who; the UI offers Take
// over. A folder with no vibe.yaml returns Status "not-workspace": the UI asks
// before calling CreateWorkspace in place.
func (a *App) OpenWorkspace(path string) (*OpenWorkspaceResult, error) {
	return a.open(path, openAuto)
}

func (a *App) open(path string, mode openMode) (*OpenWorkspaceResult, error) {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Ext(path), ".vibe") {
		return nil, errors.New(legacyVibeMessage)
	}
	if path == "" {
		return nil, errors.New("Choose a folder to open.")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("The folder %s isn't there. It may be on a drive that isn't plugged in.", path)
	}
	if !fi.IsDir() {
		return nil, errors.New("That is a file, not a folder. Open a workspace folder.")
	}
	// A pending approval card ends right now as "cancelled, nothing changed"
	// instead of waiting out its timeout on a workspace that is being swapped.
	// It runs again under the lock to close the window where a Wait registers
	// between the first call and taking the lock.
	a.cancelAgentWork()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelAgentWork()
	return a.openLocked(path, mode)
}

func (a *App) openLocked(path string, mode openMode) (*OpenWorkspaceResult, error) {
	root, err := workspace.Discover(path)
	if errors.Is(err, workspace.ErrNotWorkspace) {
		abs, _ := filepath.Abs(path)
		return &OpenWorkspaceResult{Status: "not-workspace", Path: abs}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look for a workspace in %s: %w", path, err)
	}

	// hub.Open parses the new workspace BEFORE the current one is let go
	// (OnWorkspaceOpened), so a folder that does not parse never drops what is
	// open. Once it has parsed, the current workspace is closed: a failure at
	// the hub or the session leaves nothing open.
	hmode := hub.ModeReadWrite
	switch mode {
	case openReadOnly:
		hmode = hub.ModeReadOnly
	case openTakeOver:
		hmode = hub.ModeTakeOver
	}
	// The session shares the app's redactor, so a credential resolved for a
	// connection is scrubbed from its connect and query errors (#114).
	opened, err := hub.Open(a.ctxOrBackground(), root, hub.OpenOptions{
		App: hub.AppStudio, Mode: hmode, FallbackReadOnly: true, RepairMissingID: true,
		TakeOverTimeout: 10 * time.Second,
		HubOptions:      []hub.Option{hub.WithReleaseHandler(a.onReleaseRequested)},
		Logger:          a.logger, Resolver: a.secrets, Redactor: a.secrets.Redactor(),
		OnWorkspaceOpened: func(workspace.Workspace) { a.closeWorkspaceLocked("switch") },
	})
	if err != nil {
		return nil, openError(err, mode)
	}
	ws, sess, hold, issues := opened.WS, opened.Sess, opened.Hold, opened.Issues
	readOnly, holder := opened.ReadOnly, opened.Holder

	a.ws, a.sess, a.hold = ws, sess, hold
	a.engine = opened.Eng
	a.issues = issues
	a.recordConnSigs(ws)
	a.readOnly, a.holder = readOnly, holder
	a.sessionID = uuid.New().String()
	a.duplicateOf = workspace.DuplicateIDs(root, ws.ID(), workspace.Known(a.recents.IDs()))
	a.startWatcherLocked()
	a.recents.Record(ws.Root(), ws.ID(), ws.Name())

	info := a.infoLocked()
	a.emitEvent("workspace:opened", info)
	if readOnly {
		a.emitEvent("workspace:locked", holderInfo(holder))
	}
	return &OpenWorkspaceResult{Status: "opened", Path: ws.Root(), Info: info}, nil
}

// openError words a hub.Open failure for the person at the window.
func openError(err error, mode openMode) error {
	var oe *hub.OpenError
	if !errors.As(err, &oe) {
		return err
	}
	switch oe.Step {
	case hub.StepRepair:
		return oe.Err
	case hub.StepOpen:
		return fmt.Errorf("Couldn't open this workspace: %w", oe.Err)
	case hub.StepHub:
		if mode == openTakeOver {
			return takeOverError(oe.Err)
		}
		return fmt.Errorf("failed to take the workspace's database: %w", oe.Err)
	}
	return oe.Err
}

func takeOverError(err error) error {
	switch {
	case errors.Is(err, hub.ErrNotReleasable):
		return errors.New("The program that has this workspace open can't be asked to let go. Close it there, then try again.")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("The other program didn't let go in time. It is still running the workspace; close it there, then try again.")
	}
	return fmt.Errorf("Couldn't take over the workspace: %w", err)
}

func holderInfo(h *hub.Holder) *HolderInfo {
	if h == nil {
		return nil
	}
	return &HolderInfo{App: h.App, PID: h.PID, ServesMCP: h.MCP != nil && !h.MCP.Pending && h.MCP.URL != "", Text: h.String()}
}

// CloseWorkspace closes the open workspace: MCP off, session closed, hub
// released. The frontend flushes its pending notebook saves first.
func (a *App) CloseWorkspace() error {
	a.cancelAgentWork()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelAgentWork()
	a.closeWorkspaceLocked("closed")
	return nil
}

// closeWorkspaceLocked tears the open workspace down in the order the hub
// needs: MCP, watcher, session, then the hub lock last, so the next owner can
// open the DuckDB files the moment the lock is free. A no-op with none open.
// reason rides on "workspace:closed" ("closed", "switch", "released").
func (a *App) closeWorkspaceLocked(reason string) {
	// Previewed bundles were compared with this workspace: forget them.
	a.bundles.clear()
	// A running chat turn must not outlive the session it is writing to.
	a.chat.cancelTurn("")
	if a.ws == nil {
		return
	}
	root := a.ws.Root()
	// Credentials held in memory (ask each time, session-only) end with the session.
	a.secrets.ForgetSession()
	if a.wmcp != nil {
		_ = a.wmcp.Stop()
		a.wmcp, a.mcpTool = nil, nil
	}
	if a.watchDone != nil {
		close(a.watchDone)
		a.watchDone = nil
	}
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	if a.sess != nil {
		_ = a.sess.Close()
	}
	if a.hold != nil {
		_ = a.hold.Release()
	}
	_ = a.ws.Close()
	a.ws, a.sess, a.hold, a.engine = nil, nil, nil, nil
	a.issues, a.holder, a.readOnly, a.duplicateOf = nil, nil, false, nil
	a.sessionID = ""
	a.activeNotebookID = ""
	a.emitEvent("workspace:closed", map[string]any{"path": root, "reason": reason})
}

// onReleaseRequested runs when another process (`vibe mcp serve`) asks Studio
// to let go of the workspace. It tells the frontend first so open notebooks are
// saved, then closes the workspace, which releases the hub; the window returns
// to the launcher.
func (a *App) onReleaseRequested() {
	a.mu.RLock()
	root := ""
	var hold *hub.Hold
	if a.ws != nil {
		root, hold = a.ws.Root(), a.hold
	}
	a.mu.RUnlock()
	if root == "" || hold == nil {
		return
	}
	a.emitEvent("workspace:releasing", map[string]any{"path": root})
	time.Sleep(releaseGrace)
	a.mu.Lock()
	defer a.mu.Unlock()
	// The person may have switched workspaces during the grace window: only the
	// hold that was asked to let go is closed, never whatever is open now.
	if a.hold != hold {
		return
	}
	a.cancelAgentWork()
	a.closeWorkspaceLocked("released")
}

// TakeOverWorkspace asks the process that owns the open (read-only) workspace
// to let go, then reopens it with the database. On failure the workspace comes
// back read-only and the error says why.
func (a *App) TakeOverWorkspace() (*OpenWorkspaceResult, error) {
	a.cancelAgentWork()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if !a.readOnly {
		return &OpenWorkspaceResult{Status: "opened", Path: a.ws.Root(), Info: a.infoLocked()}, nil
	}
	root := a.ws.Root()
	res, err := a.openLocked(root, openTakeOver)
	if err != nil {
		// openLocked closed the read-only workspace before trying: put it back.
		if _, rerr := a.openLocked(root, openReadOnly); rerr != nil {
			a.logger.Warn("failed to reopen the workspace read-only after a failed take-over", "error", rerr)
		}
		return nil, err
	}
	return res, nil
}

func (a *App) startWatcherLocked() {
	w, err := a.ws.Watch()
	if err != nil {
		a.logger.Warn("workspace file watcher unavailable; external edits will show on the next save", "error", err)
		return
	}
	done := make(chan struct{})
	a.watcher, a.watchDone = w, done
	ws := a.ws
	go func() {
		for {
			select {
			case <-done:
				return
			case ch, ok := <-w.Events():
				if !ok {
					return
				}
				a.onDiskChange(ws, ch)
			}
		}
	}()
}

// onDiskChange reports something other than this window changed a file in the
// workspace: a git pull, an editor, Claude Code. Lists refresh; an open
// notebook, query or skill shows the conflict banner if it had edits.
func (a *App) onDiskChange(ws workspace.Workspace, ch workspace.Change) {
	if ch.Kind == workspace.KindConfig {
		if err := ws.Reload(); err != nil {
			a.logger.Warn("vibe.yaml changed on disk but could not be reloaded", "error", err)
		} else {
			// A host changed by a git pull must not keep using the old
			// registration (and the old credential binding).
			a.syncConnections()
		}
		a.emitEvent("workspace:changed", map[string]any{"kind": string(ch.Kind)})
	}
	if ch.Kind == workspace.KindDotEnv {
		// The file's state moved (an allowed .env is now "changed"); the
		// banner and Workspace settings re-read it. Values stay as loaded.
		a.emitEvent("workspace:changed", map[string]any{"kind": "dotenv"})
		return
	}
	a.emitEvent("workspace:changed-on-disk", map[string]any{
		"kind": string(ch.Kind), "id": ch.ID, "op": string(ch.Op), "path": ch.Path,
	})
	a.emitEvent("data:changed")
}
