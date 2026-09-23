package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/config"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/crud"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/importer"
	"github.com/sandwich-labs/vibe-app/internal/llm"
	vibemcp "github.com/sandwich-labs/vibe-app/internal/mcp"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the main application struct bound to the Wails frontend.
type App struct {
	ctx         context.Context
	logger      *slog.Logger
	mu          sync.RWMutex
	container   *container.Container
	engine      *engine.Manager
	registry    *queries.Registry
	filePath    string
	mcpServer   *vibemcp.Server
	initialFile string

	// sessionID identifies the CURRENTLY open container. It is regenerated
	// on every successful OpenFile/CreateFile/CreateFromTemplate (see
	// beginFileSession) and cleared on CloseFile. A frontend view that reads
	// data at open/load time (NotebookView.vue's loadedSessionId) stamps
	// itself with the value in effect at that moment and sends it back with
	// every write (SaveNotebook); the backend rejects a write whose token
	// doesn't match the CURRENT sessionID.
	//
	// This closes a real data-corruption bug (reviewer-caught, live-proven):
	// calling OpenFile/CreateFile/CreateFromTemplate while a NotebookView
	// for the PREVIOUS file was still mounted let that view's in-flight
	// debounced save land in the NEW container after the switch — the
	// container pointer itself was already swapped by the time the old
	// view's save fired, and nothing on the write path noticed the file
	// underneath it had changed. The frontend now also flushes pending
	// saves before every one of these three calls (matching CloseFile's
	// existing flushAll), but the backend token check is the one that
	// cannot be bypassed by a caller that skips the frontend's own gating
	// (e.g. a direct Wails-binding call, or a future call site that forgets
	// to flush first).
	sessionID string

	// activeNotebookID is the id of the notebook currently open in
	// NotebookView.vue, if any — set via SetActiveNotebook (called on
	// mount/switch/close). Consulted by add_query_cell/create_dashboard's
	// "append to the open notebook" fallback (#67 deferred part) via
	// agents.Handler.SetActiveNotebook, applied per-call in
	// newAgentToolHandler/wrapAgentHandler.
	activeNotebookID string

	// taskCancel holds one context.CancelFunc per in-flight AgentTask call,
	// keyed by the frontend-generated taskID — so closing the AI-cell dialog
	// (CancelAgentTask) can stop a task turn without touching a.ctx (the
	// whole app's lifecycle context) or any concurrently running chat turn.
	taskCancelMu sync.Mutex
	taskCancel   map[string]context.CancelFunc

	// emit publishes a frontend event. In production it is nil and emitEvent
	// falls back to the Wails runtime.EventsEmit (which requires the real Wails
	// lifecycle context). Tests inject a sink so the open lifecycle — including
	// the AutoExec "app:autoexec" startup intents — can be asserted without a
	// live Wails runtime.
	emit func(event string, data ...any)

	// llmCfg is the app-global, viper-driven LLM provider/model configuration
	// for the wizard agent (loaded at startup from ~/.vibe.yaml + VIBE_* env).
	llmCfg config.LLMConfig

	// quitConfirmed gates beforeClose (review fix, step 4a round 2 #1): the
	// FIRST window-close/quit attempt is intercepted (OnBeforeClose returns
	// true, preventing it) so the frontend gets a chance to flush any
	// pending debounced saves via the "app:before-close" event; once it has
	// (or a timeout fires), it calls Quit(), which sets this flag so the
	// NEXT beforeClose lets the close through.
	quitConfirmed atomic.Bool

	// closeWatchdogOnce guards starting the round-3 watchdog timer (below)
	// so repeated close attempts never stack multiple timers.
	closeWatchdogOnce sync.Once

	// closeWatchdog and quitFn are testing seams for beforeClose's timeout
	// logic (review fix, step 4a round 3 #1: a hung webview — e.g. its
	// process suspended — never responds to "app:before-close", and
	// beforeClose returned true forever, making the window impossible to
	// close). Production leaves both nil and beforeClose lazily defaults
	// them to a real time.AfterFunc and the real Quit(); tests inject
	// deterministic stand-ins instead of waiting on a real 5s timer.
	closeWatchdog func(d time.Duration, f func())
	quitFn        func()
}

// closeWatchdogTimeout bounds how long beforeClose waits for the frontend to
// respond to "app:before-close" (flush pending saves, then call Quit())
// before quitting anyway — a hung/suspended webview must never be able to
// make the window impossible to close.
const closeWatchdogTimeout = 5 * time.Second

// NewApp creates a new App instance. The LLM config is loaded eagerly so it is
// available even before the Wails startup hook (and in tests).
func NewApp(logger *slog.Logger) *App {
	return &App{
		logger: logger,
		llmCfg: config.Load().LLM,
	}
}

// GetInitialFile returns the file path passed as a command-line argument, if any.
func (a *App) GetInitialFile() string {
	return a.initialFile
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Reclaim DuckDB temp dirs left behind by crashed Vibe processes (P6.8
	// orphan recovery). Best-effort: logs but never blocks startup.
	container.StartupSweep(a.logger)
	// In production, route frontend events through the live Wails runtime using
	// the real lifecycle context. Tests inject their own emit sink and never call
	// startup, so emitEvent stays runtime-free under test.
	if a.emit == nil {
		a.emit = func(event string, data ...any) {
			runtime.EventsEmit(a.ctx, event, data...)
		}
	}
}

// emitEvent publishes a frontend event, using the injected emitter (tests) when
// set and otherwise the Wails runtime. Centralizing emission lets the open
// lifecycle (and its AutoExec startup intents) be exercised in unit tests.
func (a *App) emitEvent(event string, data ...any) {
	if a.emit == nil {
		// No emit sink: either a unit test that did not inject one, or a call
		// before startup wired the Wails runtime emitter. Skip emission rather
		// than invoking runtime.EventsEmit with a non-lifecycle context (which
		// log.Fatals). Production always installs the runtime emitter in startup.
		if a.logger != nil {
			a.logger.Debug("emitEvent skipped: no emit sink", "event", event)
		}
		return
	}
	a.emit(event, data...)
}

func (a *App) shutdown(_ context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mcpServer != nil {
		a.mcpServer.Stop() //nolint:errcheck
		a.mcpServer = nil
	}
	if a.container != nil {
		a.container.Save()  //nolint:errcheck
		a.container.Close() //nolint:errcheck
		a.container = nil
	}
}

// beforeClose is wired as Wails' OnBeforeClose option (studio/main.go). It
// intercepts the first window-close/quit attempt and returns true
// (preventing the close) while emitting "app:before-close" so the frontend
// can flush any pending debounced saves (e.g. a notebook's autosave) before
// the container actually closes — review fix, step 4a round 2 #1. The
// frontend then calls Quit(), which sets quitConfirmed so THIS same check
// lets the next close attempt through instead of looping forever.
//
// Round 3 review fix: the first attempt also arms a one-shot watchdog timer
// (closeWatchdogTimeout) that force-quits if the frontend never responds —
// a hung or suspended webview process must not be able to make the window
// impossible to close. closeWatchdogOnce (a sync.Once) guards arming it so
// repeated close attempts never stack multiple timers; a SECOND (or later)
// attempt that arrives while the first is still pending instead lets the
// close through immediately, on the theory that a person clicking close
// twice means it, and there is no reason to make them wait out the full
// watchdog window too.
func (a *App) beforeClose(_ context.Context) (prevent bool) {
	if a.quitConfirmed.Load() {
		return false
	}

	first := false
	a.closeWatchdogOnce.Do(func() {
		first = true
		a.emitEvent("app:before-close")
		watchdog := a.closeWatchdog
		if watchdog == nil {
			watchdog = func(d time.Duration, f func()) { time.AfterFunc(d, f) }
		}
		watchdog(closeWatchdogTimeout, a.Quit)
	})
	if first {
		return true
	}

	// A later attempt while the first is still pending: quit immediately
	// rather than making a second, deliberate close click wait out the
	// watchdog too.
	a.quitConfirmed.Store(true)
	return false
}

// Quit is called by the frontend once it has flushed (or timed out waiting
// on) every pending save in response to "app:before-close" — and by
// beforeClose's own watchdog timer if the frontend never responds at all.
// It marks the close as confirmed and asks Wails to quit again, which this
// time reaches beforeClose with quitConfirmed already set.
func (a *App) Quit() {
	a.quitConfirmed.Store(true)
	quit := a.quitFn
	if quit == nil {
		quit = func() { runtime.Quit(a.ctx) }
	}
	quit()
}

// --- File Operations ---

// SelectVibeFile opens a file dialog to select a .vibe file.
func (a *App) SelectVibeFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open Vibe File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Vibe Files (*.vibe)", Pattern: "*.vibe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// SelectNewVibeFile opens a save dialog to create a new .vibe file.
func (a *App) SelectNewVibeFile() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Create Vibe File",
		DefaultFilename: "untitled.vibe",
		Filters: []runtime.FileFilter{
			{DisplayName: "Vibe Files (*.vibe)", Pattern: "*.vibe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open save dialog: %w", err)
	}
	return path, nil
}

// closeCurrentContainerLocked saves and closes whatever container is
// currently open (if any) and clears every field tied to its lifetime,
// BEFORE the caller attempts to open/create the next one. Must be called
// with a.mu held.
//
// This is the fix for a crash a reviewer reproduced live (SIGSEGV):
// OpenFile/CreateFile/CreateFromTemplate used to Save+Close the current
// container inline and then attempt the new open/create — but on a failed
// open (e.g. a wrong passphrase), the early `return err` left a.container
// still pointing at the now-CLOSED container. The NEXT OpenFile call then
// saw a.container != nil, called Save()/Close() on an already-closed
// container, and the process died. Clearing a.container (and engine/
// registry/filePath/sessionID) here, unconditionally, right after closing —
// not only on the success path — means a failed open always leaves the app
// in a clean "no file open" state instead of a half-torn-down one.
func (a *App) closeCurrentContainerLocked() {
	if a.container == nil {
		return
	}
	if a.mcpServer != nil {
		a.mcpServer.Stop() //nolint:errcheck
		a.mcpServer = nil
	}
	a.container.Save()  //nolint:errcheck
	a.container.Close() //nolint:errcheck
	a.container = nil
	a.engine = nil
	a.registry = nil
	a.filePath = ""
	a.sessionID = ""
	// So the UI falls back to Unlock/the file picker instead of showing a
	// stale view of the file that just got closed out from under it.
	a.emitEvent("file:closed")
}

// OpenFile opens an existing .vibe file with the given passphrase.
func (a *App) OpenFile(path, passphrase string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.closeCurrentContainerLocked()

	c, err := container.Open(path,
		container.WithPassphrase(passphrase),
		container.WithLogger(a.logger),
	)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	a.container = c
	a.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(a.logger))
	a.registry = queries.NewRegistry(c.SQLiteDB(), a.logger)
	a.filePath = path
	a.beginFileSession()
	notebooks.MigrateOnOpen(a.container.SQLiteDB(), a.container.IsReadOnly(), a.logger)
	a.emitEvent("file:opened")
	return nil
}

// beginFileSession stamps a.sessionID with a fresh id, marking a new
// "generation" of the open container — see the field's doc comment. Called
// while a.mu is held, immediately after a.container is swapped in
// (OpenFile/CreateFile/CreateFromTemplate).
func (a *App) beginFileSession() {
	a.sessionID = uuid.New().String()
}

// CreateFile creates a new .vibe file.
func (a *App) CreateFile(path, passphrase, title, desc string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.closeCurrentContainerLocked()

	c, err := container.Create(path,
		container.WithPassphrase(passphrase),
		container.WithLogger(a.logger),
		container.WithTitle(title),
		container.WithDescription(desc),
	)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	a.container = c
	a.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(a.logger))
	a.registry = queries.NewRegistry(c.SQLiteDB(), a.logger)
	a.filePath = path
	a.beginFileSession()
	notebooks.MigrateOnOpen(a.container.SQLiteDB(), a.container.IsReadOnly(), a.logger)
	a.emitEvent("file:opened")
	return nil
}

// CloseFile saves and closes the current file.
func (a *App) CloseFile() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil
	}

	if a.mcpServer != nil {
		a.mcpServer.Stop() //nolint:errcheck
		a.mcpServer = nil
	}

	if err := a.container.Save(); err != nil {
		return fmt.Errorf("failed to save: %w", err)
	}
	if err := a.container.Close(); err != nil {
		return fmt.Errorf("failed to close: %w", err)
	}
	a.container = nil
	a.engine = nil
	a.registry = nil
	a.filePath = ""
	a.sessionID = ""
	a.emitEvent("file:closed")
	return nil
}

// SaveFile saves the current file.
func (a *App) SaveFile() error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	return a.container.Save()
}

// IsFileOpen returns whether a file is currently open.
func (a *App) IsFileOpen() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.container != nil
}

// FileInfo holds metadata about the open file.
type FileInfo struct {
	Path          string `json:"path"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	NotebookCount int    `json:"notebook_count"`
	TableCount    int    `json:"table_count"`
	QueryCount    int    `json:"query_count"`
	FileSize      int64  `json:"file_size"`
	// SessionID identifies the currently open container (see App.sessionID's
	// doc comment). A view that reads data at load time should capture this
	// and send it back with any later write (e.g. SaveNotebook) so the
	// backend can reject a write from a superseded file session.
	SessionID string `json:"session_id"`
}

// GetFileInfo returns metadata about the currently open file.
func (a *App) GetFileInfo() (*FileInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	info := &FileInfo{Path: a.filePath, SessionID: a.sessionID}

	title, err := a.container.Meta("title")
	if err == nil {
		info.Title = title
	}
	desc, err := a.container.Meta("description")
	if err == nil {
		info.Description = desc
	}

	db := a.container.SQLiteDB()
	db.QueryRow("SELECT COUNT(*) FROM _vibe_notebooks").Scan(&info.NotebookCount) //nolint:errcheck
	db.QueryRow("SELECT COUNT(*) FROM _vibe_queries").Scan(&info.QueryCount)      //nolint:errcheck

	if duckDB := a.container.DuckDBDB(); duckDB != nil {
		duckDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main'").Scan(&info.TableCount) //nolint:errcheck
	}

	if fi, err := os.Stat(a.filePath); err == nil {
		info.FileSize = fi.Size()
	}

	return info, nil
}

// --- Skills (#62) ---

// SkillSummary is one skill as the Skills view lists it.
type SkillSummary struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	TableName    string `json:"table_name,omitempty"`
	Source       string `json:"source"` // built-in | user | import
}

// ListSkills returns every skill visible to chat: built-ins plus stored
// user/import skills that pass table gating (a table-bound skill only
// appears once its table exists and has rows). Mirrors exactly what the
// agent's list_skills tool would return, so the Skills view shows the same
// set the model can actually see.
func (a *App) ListSkills() ([]SkillSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	entries, err := agents.LoadSkills(a.container.SQLiteDB(), a.container.DuckDBDB(), a.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to list skills: %w", err)
	}
	visible := agents.ChatVisibleSkills(entries)
	out := make([]SkillSummary, 0, len(visible))
	for _, s := range visible {
		out = append(out, SkillSummary{
			Name: s.Name, Description: s.Description, Instructions: s.Instructions,
			TableName: s.TableName, Source: s.Source,
		})
	}
	return out, nil
}

// SaveSkill creates or updates a user-authored skill. Studio-only; rejects
// built-in names, the reserved "task-" prefix, and invalid names — the same
// validation the create_or_update_skill agent tool applies (SaveUserSkill is
// shared by both entry points). Like page/table CRUD elsewhere in this file,
// this bypasses the agent sandbox gate entirely: it's the user directly
// authoring content in Studio, not an agent tool call.
func (a *App) SaveSkill(name, description, instructions, tableName string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if err := agents.SaveUserSkill(a.container.SQLiteDB(), a.container.DuckDBDB(), name, description, instructions, tableName); err != nil {
		return err
	}
	a.emitEvent("data:changed")
	return nil
}

// DeleteSkill removes a user- or import-authored skill. Built-ins can't be
// deleted (they aren't rows in _vibe_skills).
func (a *App) DeleteSkill(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if err := agents.DeleteSkill(a.container.SQLiteDB(), name); err != nil {
		return err
	}
	a.emitEvent("data:changed")
	return nil
}

// --- Notebooks (#66 part A) ---
//
// SQL execution for a notebook cell reuses RunA2UIQuery — a notebook's sql
// cell is a read query, the same live binding an A2UI page's data-bound
// component uses (same sandbox-gated classifier, same read-only-only gate).
// Writes belong in the Query Explorer or the table browser, not in a
// notebook cell; RunA2UIQuery already rejects a write with
// errReadOnlyBindingDenied, and the frontend surfaces that as "Notebook
// cells are read-only queries — use the Query editor to change data."

// NotebooksChangedEvent is the payload of every "notebooks:changed" Wails
// event (review fix, finding 2). AppendedCellIDs is the load-bearing field:
// an open NotebookView.vue merges in ONLY cell ids listed here — never "any
// remote id we don't already have locally" (the pre-fix behavior), which
// could resurrect a cell the user had just deleted locally but not yet
// saved. Every emitter that is NOT "an agent tool appended a specific new
// cell" (CreateNotebook, SaveNotebook, RenameNotebook, DeleteNotebook,
// ImportNotebook) omits AppendedCellIDs entirely — the frontend treats an
// event with no AppendedCellIDs as "not an append, nothing to merge" (this
// is also how NotebookView tells its OWN SaveNotebook echo apart from a
// remote append: its own save never sets this field).
type NotebooksChangedEvent struct {
	NotebookID      string   `json:"notebookId"`
	AppendedCellIDs []string `json:"appendedCellIds,omitempty"`
}

// emitNotebooksChanged emits "notebooks:changed" with the shared payload
// shape — see NotebooksChangedEvent.
func (a *App) emitNotebooksChanged(notebookID string, appendedCellIDs []string) {
	a.emitEvent("notebooks:changed", NotebooksChangedEvent{NotebookID: notebookID, AppendedCellIDs: appendedCellIDs})
}

// ListNotebooks returns every notebook's summary, newest-updated first.
func (a *App) ListNotebooks() ([]notebooks.Summary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	return notebooks.List(a.container.SQLiteDB())
}

// GetNotebook loads one notebook, rebuilt through notebooks.Coerce.
func (a *App) GetNotebook(id string) (*notebooks.Notebook, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	nb, warnings, err := notebooks.Get(a.container.SQLiteDB(), id)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		a.logger.Warn("notebook coercion warning", "notebook_id", id, "warning", w)
	}
	return &nb, nil
}

// SetActiveNotebook records which notebook is currently open in
// NotebookView.vue (or clears it with ""), for the chat/MCP agent's
// add_query_cell/create_dashboard tools (#67 deferred part) — "append to the
// notebook currently open in Studio" needs the frontend to say which one
// that is; there is no other way for a Go binding to know. The view calls
// this on mount, on switching notebooks, and with "" on unmount/close.
func (a *App) SetActiveNotebook(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeNotebookID = id
}

// CreateNotebook creates a new, empty notebook and returns it.
func (a *App) CreateNotebook(title string) (*notebooks.Notebook, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	nb, err := notebooks.Create(a.container.SQLiteDB(), title)
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &nb, nil
}

// NotebookSaveResult wraps a saved/imported notebook with any warnings
// notebooks.Coerce produced — a save can silently drop or clamp fields (an
// unknown cell type, an out-of-range view, an invalid a2ui page), and the
// caller needs to know rather than have it happen invisibly.
type NotebookSaveResult struct {
	Notebook notebooks.Notebook `json:"notebook"`
	Warnings []string           `json:"warnings"`
}

// SaveNotebook rebuilds raw (the frontend's in-memory notebook, JSON-decoded
// into a plain map by Wails) through notebooks.Coerce and upserts it. This is
// the ONE write path for a notebook's title/cells — the frontend's add/move/
// delete/collapse cell operations all mutate its local copy and call this
// with the whole document, matching the "outputs are never persisted, only
// source/view/page are" contract (Coerce only ever keeps those fields).
// sessionID must be the value GetFileInfo().SessionID returned at the time
// the caller LOADED the notebook it is now saving (see App.sessionID's doc
// comment) — never re-fetched at save time, or this check couldn't catch
// the bug it exists for (a save that fires after the file underneath it was
// already switched). A mismatch means the file was closed/reopened/switched
// since this notebook was loaded, so the write is rejected rather than
// silently landing in the wrong container.
func (a *App) SaveNotebook(raw map[string]any, sessionID string) (*NotebookSaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if sessionID != a.sessionID {
		return nil, fmt.Errorf("stale file session: the open file changed since this notebook was loaded; reopen it and try again")
	}
	nb, warnings, err := notebooks.Save(a.container.SQLiteDB(), raw)
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &NotebookSaveResult{Notebook: nb, Warnings: warnings}, nil
}

// RenameNotebook updates a notebook's title without touching its cells.
func (a *App) RenameNotebook(id, title string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if err := notebooks.Rename(a.container.SQLiteDB(), id, title); err != nil {
		return err
	}
	a.emitNotebooksChanged(id, nil)
	return nil
}

// DeleteNotebook removes a notebook by id.
func (a *App) DeleteNotebook(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if err := notebooks.Delete(a.container.SQLiteDB(), id); err != nil {
		return err
	}
	a.emitNotebooksChanged(id, nil)
	return nil
}

// notebookExportFile is the shape ExportNotebook writes and ImportNotebook
// reads — matching model.js's toFile()/coerceNotebook's accepted wrapper
// shape ({kind, schemaVersion, notebook}), adapted to this product's file
// kind (notebooks.FileKind).
type notebookExportFile struct {
	Kind          string             `json:"kind"`
	SchemaVersion int                `json:"schemaVersion"`
	ExportedAt    string             `json:"exportedAt"`
	Notebook      notebooks.Notebook `json:"notebook"`
}

// ExportNotebook writes one notebook to a JSON file. If dst is empty, a save
// dialog picks the destination; dst == "" and a cancelled dialog both return
// ("", nil), matching ExportTableCSV's cancel semantics.
func (a *App) ExportNotebook(id, dst string) (string, error) {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return "", fmt.Errorf("no file open")
	}
	nb, warnings, err := notebooks.Get(a.container.SQLiteDB(), id)
	a.mu.RUnlock()
	if err != nil {
		return "", err
	}
	for _, w := range warnings {
		a.logger.Warn("notebook coercion warning on export", "notebook_id", id, "warning", w)
	}

	path := dst
	if path == "" {
		path, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Export Notebook",
			DefaultFilename: nb.Title + ".json",
			Filters: []runtime.FileFilter{
				{DisplayName: "Vibe Notebook (*.json)", Pattern: "*.json"},
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to open save dialog: %w", err)
		}
		if path == "" {
			return "", nil
		}
	}

	file := notebookExportFile{
		Kind: notebooks.FileKind, SchemaVersion: notebooks.SchemaVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), Notebook: nb,
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal notebook: %w", err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		return "", fmt.Errorf("failed to write notebook file: %w", err)
	}
	return path, nil
}

// SelectNotebookFile opens a file dialog for choosing an exported notebook
// JSON file — ImportNotebook's picker step.
func (a *App) SelectNotebookFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import Notebook",
		Filters: []runtime.FileFilter{
			{DisplayName: "Vibe Notebook (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// ImportNotebook reads path (an ExportNotebook file) and imports it as a
// brand-new notebook, through notebooks.Coerce like every other load. Import
// never overwrites a notebook already in this container — the file's own id/
// updatedAt are stripped before Coerce so a fresh id is always assigned,
// matching model.js's importFile ("Import always lands as a new notebook").
// maxImportNotebookFileBytes caps an imported notebook file at 8MB (review
// fix, step 4a #7c) — checked via os.Stat BEFORE the file is read, so a
// multi-hundred-MB file (or a symlink/fifo pointed at one) never gets
// fully loaded into memory just to be rejected.
const maxImportNotebookFileBytes = 8 * 1024 * 1024

func (a *App) ImportNotebook(path string) (*NotebookSaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file: %w", err)
	}
	if info.Size() > maxImportNotebookFileBytes {
		return nil, fmt.Errorf("notebook file is %d bytes, exceeding the %d byte import limit", info.Size(), maxImportNotebookFileBytes)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file: %w", err)
	}
	var wrapper map[string]any
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid notebook file: %w", err)
	}
	// ImportNotebook specifically (unlike Storage.Get/SaveNotebook, which
	// stay permissive about a bare notebook object with no "kind") requires
	// the file to be self-identified as a Vibe notebook export — review fix,
	// step 4a #7b: without this, importing an arbitrary JSON file that
	// happens to parse (e.g. package.json) would silently either fail deep
	// inside Coerce with a confusing error, or in a pathological case
	// coincidentally look enough like a notebook to import garbage.
	if kind, _ := wrapper["kind"].(string); kind != notebooks.FileKind {
		return nil, fmt.Errorf("not a Vibe notebook file (expected \"kind\": %q, got %q)", notebooks.FileKind, kind)
	}
	if nested, ok := wrapper["notebook"].(map[string]any); ok {
		delete(nested, "id")
		delete(nested, "updatedAt")
	} else {
		delete(wrapper, "id")
		delete(wrapper, "updatedAt")
	}

	nb, warnings, err := notebooks.Save(a.container.SQLiteDB(), wrapper)
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &NotebookSaveResult{Notebook: nb, Warnings: warnings}, nil
}

// --- Tables ---

// ListTables returns all user tables (non _vibe_ tables).
func (a *App) ListTables() ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	rows, err := db.Query("SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan table: %w", err)
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// ColumnInfo describes a table column.
type ColumnInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"not_null"`
	PK      bool   `json:"pk"`
	// FK is read-only output from DescribeTable (see engine.ColumnInfo.FK);
	// CreateTable/AddColumn ignore it on input.
	FK bool `json:"fk"`
}

// TableInfo describes a table's schema.
type TableInfo struct {
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns"`
}

// DescribeTable returns schema info for a table.
func (a *App) DescribeTable(name string) (*TableInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	if !isValidIdentifier(name) {
		return nil, fmt.Errorf("invalid table name")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	// Shared introspection (internal/schema): fills real PRIMARY KEY membership
	// from duckdb_constraints() — the previous inline query always reported
	// PK=false, leaving the schema tree / ER diagram to guess by column name.
	cols, err := engine.TableColumns(db, name)
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	info := &TableInfo{Name: name}
	for _, c := range cols {
		info.Columns = append(info.Columns, ColumnInfo{
			Name:    c.Name,
			Type:    c.Type,
			NotNull: c.NotNull,
			PK:      c.PK,
			FK:      c.FK,
		})
	}
	return info, nil
}

// TableStat is one entry in the Query Explorer's schema tree: a table plus its
// live row count (W5).
type TableStat struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// ListTableStats returns every user table with its row count, for the Query
// Explorer schema tree (W5).
func (a *App) ListTableStats() ([]TableStat, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	rows, err := db.Query(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tables: %w", err)
	}

	out := make([]TableStat, 0, len(names))
	for _, n := range names {
		var count int64
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", engine.QuoteIdent(n))).Scan(&count); err != nil {
			return nil, fmt.Errorf("failed to count rows of %q: %w", n, err)
		}
		out = append(out, TableStat{Name: n, Rows: count})
	}
	return out, nil
}

// ExportQueryResults writes the result of a SQL query to CSV or Parquet via
// DuckDB COPY (W5). format is "csv" or "parquet". dst is the destination path;
// when empty, a native save dialog is shown (the UI path) — a non-empty dst
// writes directly (scriptable/testable, mirroring ExportTableXLSX). Returns
// the written path, or "" if the dialog was cancelled.
func (a *App) ExportQueryResults(sqlStr, format, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if strings.TrimSpace(sqlStr) == "" {
		return "", fmt.Errorf("no query to export")
	}

	var ext, copyOpts string
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		ext, copyOpts = ".csv", "FORMAT csv, HEADER true"
	case "parquet":
		ext, copyOpts = ".parquet", "FORMAT parquet"
	default:
		return "", fmt.Errorf("unsupported export format %q (want csv or parquet)", format)
	}

	if dst == "" {
		path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Export query results",
			DefaultFilename: "query" + ext,
			Filters: []runtime.FileFilter{
				{DisplayName: strings.ToUpper(format) + " Files (*" + ext + ")", Pattern: "*" + ext},
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to open save dialog: %w", err)
		}
		if path == "" {
			return "", nil
		}
		dst = path
	}
	if !strings.HasSuffix(strings.ToLower(dst), ext) {
		dst += ext
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	copySQL := fmt.Sprintf("COPY (%s) TO '%s' WITH (%s)",
		sqlStr, strings.ReplaceAll(dst, "'", "''"), copyOpts)
	if _, err := ddb.Exec(copySQL); err != nil {
		return "", fmt.Errorf("failed to export query results: %w", err)
	}
	a.logger.Info("exported query results", "format", format, "path", dst)
	return dst, nil
}

// TableData holds paginated table data.
type TableData struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalRows  int      `json:"total_rows"`
	TotalPages int      `json:"total_pages"`
}

// GetTableData returns paginated data from a table.
func (a *App) GetTableData(name string, page, pageSize int, sortCol, sortDir string) (*TableData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	if !isValidIdentifier(name) {
		return nil, fmt.Errorf("invalid table name")
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	var totalRows int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", engine.QuoteIdent(name))).Scan(&totalRows); err != nil {
		return nil, fmt.Errorf("failed to count rows: %w", err)
	}

	orderClause := ""
	if sortCol != "" && isValidIdentifier(sortCol) {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", engine.QuoteIdent(sortCol), dir)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %s%s LIMIT ? OFFSET ?", engine.QuoteIdent(name), orderClause)
	rows, err := db.Query(query, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var data [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		// Convert []byte to string for JSON serialization; normalize driver
		// types (DECIMAL, HUGEINT) so they don't marshal as raw structs (#42).
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			} else {
				values[i] = engine.NormalizeValue(v)
			}
		}
		data = append(data, values)
	}

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &TableData{
		Columns:    columns,
		Rows:       data,
		Page:       page,
		PageSize:   pageSize,
		TotalRows:  totalRows,
		TotalPages: totalPages,
	}
	if err := jsonRoundTripGuard(result); err != nil {
		return nil, err
	}
	return result, nil
}

// FilterCondition is one structured datasheet filter clause (plan §2.3). It is
// the SQL-free unit behind Filter-by-Selection (a single eq) and Filter-by-Form
// (multiple conditions). Op is one of eq/neq/lt/gt/lte/gte/like/is_null/
// is_not_null; Value is ignored for the is_null/is_not_null operators.
type FilterCondition struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// FilterSpec is a set of FilterConditions joined by a single combinator
// ("AND" or "OR"). It is translated to a parameterized WHERE clause.
type FilterSpec struct {
	Combinator string            `json:"combinator"`
	Conditions []FilterCondition `json:"conditions"`
}

// GetTableDataFiltered returns paginated, structured-filter-driven table data
// plus an accurate total-row count for the record-count bar (plan §2.3, P2.5).
//
// Translation rules:
//   - each condition's column is validated against the table's real columns
//     (case-sensitive ident allowlist) — no SQL injection via identifier
//   - values are bound as parameters (?), never interpolated — no injection via
//     value (a single quote matches literally)
//   - is_null / is_not_null are value-less and bind no parameter
//   - conditions join with the spec combinator (AND/OR; defaults to AND)
func (a *App) GetTableDataFiltered(name string, spec FilterSpec, page, pageSize int, sortCol, sortDir string) (*TableData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return nil, fmt.Errorf("invalid table name")
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Build an allowlist of real columns to validate filter/sort identifiers.
	cols, err := engine.TableColumns(db, name)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect columns for filter: %w", err)
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c.Name] = true
	}

	whereClause, args, err := buildFilterWhere(spec, known)
	if err != nil {
		return nil, err
	}

	var totalRows int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", engine.QuoteIdent(name), whereClause)
	if err := db.QueryRow(countQuery, args...).Scan(&totalRows); err != nil {
		return nil, fmt.Errorf("failed to count filtered rows: %w", err)
	}

	orderClause := ""
	if sortCol != "" && isValidIdentifier(sortCol) && known[sortCol] {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", engine.QuoteIdent(sortCol), dir)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %s%s%s LIMIT ? OFFSET ?", engine.QuoteIdent(name), whereClause, orderClause)
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query filtered table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var data [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			} else {
				values[i] = engine.NormalizeValue(v)
			}
		}
		data = append(data, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate filtered rows: %w", err)
	}

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	result := &TableData{
		Columns:    columns,
		Rows:       data,
		Page:       page,
		PageSize:   pageSize,
		TotalRows:  totalRows,
		TotalPages: totalPages,
	}
	if err := jsonRoundTripGuard(result); err != nil {
		return nil, err
	}
	return result, nil
}

// filterOps maps a FilterCondition.Op to its SQL comparison operator. Value-less
// operators (is_null/is_not_null) are handled separately.
var filterOps = map[string]string{
	"eq":   "=",
	"neq":  "<>",
	"lt":   "<",
	"gt":   ">",
	"lte":  "<=",
	"gte":  ">=",
	"like": "LIKE",
}

// buildFilterWhere translates a FilterSpec into a parameterized WHERE clause
// (leading " WHERE ...", or "" when there are no conditions) plus its bound
// args. Column identifiers are validated against the known-columns allowlist;
// values are always bound, never interpolated.
func buildFilterWhere(spec FilterSpec, known map[string]bool) (string, []any, error) {
	if len(spec.Conditions) == 0 {
		return "", nil, nil
	}

	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	var clauses []string
	var args []any
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", nil, fmt.Errorf("unknown filter column %q", c.Column)
		}
		switch c.Op {
		case "is_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NULL", engine.QuoteIdent(c.Column)))
		case "is_not_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NOT NULL", engine.QuoteIdent(c.Column)))
		default:
			sqlOp, ok := filterOps[c.Op]
			if !ok {
				return "", nil, fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, fmt.Sprintf("%s %s ?", engine.QuoteIdent(c.Column), sqlOp))
			args = append(args, c.Value)
		}
	}

	return " WHERE " + strings.Join(clauses, " "+combinator+" "), args, nil
}

// InsertRow inserts a row into a table, coercing all-string form inputs to the
// column's type (via information_schema) and enforcing the Access-style
// _vibe_field_meta required + validation_rule constraints server-side. On a
// validation failure it returns a field-keyed *WriteError so the form UI can
// highlight the offending controls instead of surfacing a raw SQL string.
func (a *App) InsertRow(table string, values map[string]any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ddb, colTypes, metas, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := insertRowTx(tx, colTypes, metas, table, values)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit insert: %w", err)
	}
	return res, nil
}

// UpdateRow updates a row in a table by primary key, applying the same coercion
// and validation as InsertRow. A 0-affected-row outcome (PK mismatch) is
// surfaced as an error rather than a silent no-op.
func (a *App) UpdateRow(table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !isValidIdentifier(pkCol) {
		return nil, fmt.Errorf("invalid primary key column")
	}

	ddb, colTypes, metas, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := updateRowTx(tx, colTypes, metas, table, pkCol, pkVal, values)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit update: %w", err)
	}
	return res, nil
}

// prepareWrite validates the open container + table and loads the column types
// and field metadata needed by the transactional write helpers.
func (a *App) prepareWrite(table string) (*sql.DB, map[string]string, map[string]crud.FieldMeta, error) {
	if a.container == nil {
		return nil, nil, nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return nil, nil, nil, fmt.Errorf("invalid table name")
	}
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return nil, nil, nil, fmt.Errorf("DuckDB not available")
	}
	colTypes, err := columnTypes(ddb, table)
	if err != nil {
		return nil, nil, nil, err
	}
	metas, err := fieldMetaMap(crud.NewFieldMetaStore(a.container.SQLiteDB()), table)
	if err != nil {
		return nil, nil, nil, err
	}
	return ddb, colTypes, metas, nil
}

// DeleteRow deletes a row from a table by primary key. Consistent with
// InsertRow/UpdateRow, it validates the pk column identifier, runs the DELETE
// inside a transaction with rollback on error, coerces pkVal to the PK column's
// type, and surfaces a 0-affected-row outcome (PK mismatch) as a structured
// *WriteError rather than a raw SQL string.
func (a *App) DeleteRow(table, pkCol string, pkVal any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !isValidIdentifier(pkCol) {
		return nil, fmt.Errorf("invalid primary key column")
	}

	ddb, colTypes, _, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := deleteRowTx(tx, colTypes, table, pkCol, pkVal)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit delete: %w", err)
	}
	return res, nil
}

// isAutoNumberType reports whether a column type is an integer family member
// that should auto-number when used as a primary key. DuckDB has no implicit
// AUTOINCREMENT — without a sequence-backed DEFAULT, a form insert that omits
// the id fails with "NOT NULL constraint failed".
func isAutoNumberType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "BIGINT", "INTEGER", "INT", "INT2", "INT4", "INT8", "SMALLINT", "TINYINT",
		"HUGEINT", "UBIGINT", "UINTEGER", "USMALLINT", "UTINYINT":
		return true
	}
	return false
}

// pkSequenceName is the deterministic sequence backing an auto-number primary
// key. Both identifiers are validated before this is interpolated into DDL.
func pkSequenceName(table, column string) string {
	return fmt.Sprintf("_vibe_seq_%s_%s", table, column)
}

// CreateTable creates a new table with the given columns. Integer primary-key
// columns are made auto-numbering via a backing sequence (the Access AutoNumber
// behavior) so data-entry forms can omit the id.
func (a *App) CreateTable(name string, columns []ColumnInfo) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid table name")
	}
	for _, col := range columns {
		if !isValidIdentifier(col.Name) {
			return fmt.Errorf("invalid column name %q", col.Name)
		}
	}

	var colDefs, seqDDL []string
	for _, col := range columns {
		def := fmt.Sprintf("%s %s", engine.QuoteIdent(col.Name), col.Type)
		if col.PK && isAutoNumberType(col.Type) {
			seq := pkSequenceName(name, col.Name)
			seqDDL = append(seqDDL, fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s START 1", engine.QuoteIdent(seq)))
			def += fmt.Sprintf(" DEFAULT nextval('%s')", seq)
		}
		if col.PK {
			def += " PRIMARY KEY"
		}
		if col.NotNull {
			def += " NOT NULL"
		}
		colDefs = append(colDefs, def)
	}

	query := fmt.Sprintf("CREATE TABLE %s (%s)", engine.QuoteIdent(name), strings.Join(colDefs, ", "))
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	for _, ddl := range seqDDL {
		if _, err := ddb.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create id sequence: %w", err)
		}
	}
	_, err := ddb.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	a.emitEvent("data:changed")
	return nil
}

// DropTable drops a table.
func (a *App) DropTable(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid table name")
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err := ddb.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", engine.QuoteIdent(name)))
	if err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}
	a.emitEvent("data:changed")
	return nil
}

// --- Queries ---

// QueryResult holds the results of an executed query.
type QueryResult struct {
	Columns  []ColumnInfo `json:"columns"`
	Rows     [][]any      `json:"rows"`
	Duration string       `json:"duration"`
	RowCount int          `json:"row_count"`
}

// ExecuteQuery runs a SQL query on DuckDB.
// jsonRoundTripGuard is a last-resort, defence-in-depth check (review fix,
// #67 step 2 round 3): Wails marshals a binding's return value to JSON
// itself on the way back to the frontend, and an unmarshalable value there
// does not surface as an ordinary error to the JS caller — it took the
// ENTIRE Studio process down, confirmed live (round 1 review: a raw NaN
// float reaching a binding logged "FAT | json: unsupported value: NaN" and
// exited the backend). engine.NormalizeValue now recursively sanitizes
// every known composite shape DuckDB can return, so this should never
// trigger in practice — but it is cheap (one json.Marshal, only ever
// discarding the bytes) and every row-returning binding calls it on its
// result immediately before returning, so a value NormalizeValue somehow
// missed (a future composite type, a code path that doesn't call it) still
// becomes an ordinary Go error surfaced to the one caller, never a crash of
// every open file/every other in-flight call.
func jsonRoundTripGuard(v any) error {
	if _, err := json.Marshal(v); err != nil {
		return fmt.Errorf("result contains a value that could not be serialized (%w) — this is almost certainly a bug in Vibe, not your query; please report it", err)
	}
	return nil
}

func (a *App) ExecuteQuery(sqlStr string) (*QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	start := time.Now()
	rs, err := a.engine.Execute(context.Background(), sqlStr, nil)
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	result := &QueryResult{
		Duration: duration.String(),
		RowCount: rs.RowCount(),
	}

	for _, col := range rs.Columns {
		result.Columns = append(result.Columns, ColumnInfo{
			Name: col.Name,
			Type: col.Type,
		})
	}

	// Convert []byte to string for JSON.
	for _, row := range rs.Rows {
		cleaned := make([]any, len(row))
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				cleaned[i] = string(b)
			} else {
				cleaned[i] = v
			}
		}
		result.Rows = append(result.Rows, cleaned)
	}

	if err := jsonRoundTripGuard(result); err != nil {
		return nil, err
	}
	return result, nil
}

// a2uiQueryRowCap bounds how many rows a single A2UI data-bound component
// (stat/table/barChart/lineChart) will render — independent of, and tighter
// than, the agent's run_query MaxQueryRows, since a component's whole point
// is a compact chart/stat/table inline in a chat reply or notebook cell, not
// a full result grid.
const a2uiQueryRowCap = 500

// a2uiQueryTimeout bounds how long one RunA2UIQuery call may run before its
// context is cancelled, interrupting the underlying DuckDB query (review
// fix, step 4a #1 — a live binding must never be able to hang the file's
// one DuckDB connection for other bindings/queries behind it).
const a2uiQueryTimeout = 30 * time.Second

// A2UIQueryResult is RunA2UIQuery's result shape: just columns + rows, the
// minimum the Vue renderer's data-bound components need (see toPoints/
// buildTable in the ported renderer).
type A2UIQueryResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	// Truncated is true when the query returned more than a2uiQueryRowCap
	// rows and was cut off — the cap is now pushed into the query itself
	// (RunReadOnlySQL's LIMIT push-down) rather than fetched-then-sliced,
	// so this reports whether that LIMIT actually cut anything.
	Truncated bool `json:"truncated"`
}

// RunA2UIQuery executes one A2UI component's "sql" binding. It is called by
// the Vue renderer (studio/frontend/src/a2ui/) once per data-bound
// component when an A2UI page renders, live, on every mount.
//
// This SQL was authored by the model, not typed by the user in the query
// editor — unlike ExecuteQuery (the query editor's binding, deliberately
// unrestricted for the human at the keyboard), RunA2UIQuery must go through
// the sandbox classifier, not a bypass. It does NOT reuse run_query's own
// JSON-RPC dispatch (review fix, #67 step 2): run_query's checkSQLClass lets
// read-write/full execute a write, which is correct for an agent's one-shot
// tool call but wrong here — a "sql" binding re-runs every time the page
// renders, so a write there would fire on every render, not once. Instead it
// calls agents.Handler.RunReadOnlySQL, which reuses the exact same
// classifySQL (no second, drifted copy of the classifier) but gates the
// result with checkSQLClassReadOnly: sqlClassRead only, regardless of
// sandbox mode.
func (a *App) RunA2UIQuery(sqlStr string) (*A2UIQueryResult, error) {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return nil, fmt.Errorf("no file open")
	}
	sandbox := a.metaOr("agent_sandbox", "full")
	sc := &agents.SandboxConfig{Mode: agents.SandboxMode(sandbox), MaxQueryRows: 10000}
	// ceiling = ModeFull: RunReadOnlySQL (below) forces read-only regardless
	// of sc.Mode/ceiling, via its own checkSQLClassReadOnly gate — see this
	// function's doc comment — so no additional cap is needed here.
	handler := a.wrapAgentHandler(sc, agents.ModeFull)
	a.mu.RUnlock()

	// Row cap enforced server side (review fix) and pushed down into the
	// query itself via LIMIT (review fix, round 4) rather than fetched in
	// full then sliced — the frontend never gets a chance to render more
	// than this many rows for one component, and a query like
	// `SELECT * FROM range(100000000)` never materializes more than
	// a2uiQueryRowCap+1 rows in the first place.
	//
	// Per-query timeout (review fix, step 4a #1): a2uiQueryTimeout bounds
	// how long ANY one live binding may run. Without it, a notebook packed
	// with many data-bound components (even after the Coerce-time cap) or
	// an accidentally expensive query (e.g. a cross join) could hang this
	// call indefinitely — and since this DB connection is shared, block
	// every OTHER binding behind it too. go-duckdb's QueryContext honors
	// ctx cancellation by interrupting the running query on the DuckDB
	// connection, so this deadline actually stops the query, not just the
	// Go-side wait.
	ctx, cancel := context.WithTimeout(a.ctx, a2uiQueryTimeout)
	defer cancel()
	rs, truncated, err := handler.RunReadOnlySQL(ctx, sqlStr, nil, a2uiQueryRowCap)
	if err != nil {
		return nil, err
	}

	out := &A2UIQueryResult{Truncated: truncated}
	for _, c := range rs.Columns {
		out.Columns = append(out.Columns, c.Name)
	}
	out.Rows = rs.Rows
	if err := jsonRoundTripGuard(out); err != nil {
		return nil, err
	}
	return out, nil
}

// QuerySummary holds a query list entry.
type QuerySummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Engine string `json:"engine"`
}

// ListQueries returns all saved queries.
func (a *App) ListQueries() ([]QuerySummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	qs, err := a.registry.List(context.Background())
	if err != nil {
		return nil, err
	}

	result := make([]QuerySummary, 0, len(qs))
	for _, q := range qs {
		result = append(result, QuerySummary{
			ID:     q.ID,
			Title:  q.Title,
			Engine: q.Engine,
		})
	}
	return result, nil
}

// GetQuery returns a saved query by ID.
func (a *App) GetQuery(id string) (*queries.Query, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	return a.registry.Get(context.Background(), id)
}

// SaveQuery creates or updates a saved query.
func (a *App) SaveQuery(id, title, sqlStr, description string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	if id == "" {
		id = uuid.New().String()
	}

	q := &queries.Query{
		ID:         id,
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}

	// Try update first, create if not found.
	err := a.registry.Update(context.Background(), q)
	if err != nil {
		return a.registry.Create(context.Background(), q)
	}
	return nil
}

// PreviewFilterSQL renders a FilterSpec (plus optional selected columns from the
// QBE grid) into a deterministic SQL string WITHOUT persisting it (plan §2.4,
// P2.6). The generated SQL is the single rendering path shared with
// SaveFilterAsQuery, so the preview shown to the builder is byte-for-byte the
// SQL that gets stored.
func (a *App) PreviewFilterSQL(name string, spec FilterSpec, columns []string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.generateFilterSQL(name, spec, columns)
}

// SaveFilterAsQuery renders a FilterSpec into deterministic SQL and persists it
// as a named query via the existing queries.Registry, returning the new query id
// (plan §2.4, P2.6). It reuses the same rendering path as PreviewFilterSQL so the
// stored SQL equals the previewed SQL.
func (a *App) SaveFilterAsQuery(name string, spec FilterSpec, columns []string, title string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sqlStr, err := a.generateFilterSQL(name, spec, columns)
	if err != nil {
		return "", err
	}

	id := uuid.New().String()
	q := &queries.Query{
		ID:         id,
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := a.registry.Create(context.Background(), q); err != nil {
		return "", fmt.Errorf("failed to save filter as query: %w", err)
	}
	a.logger.Info("saved filter as named query", "id", id, "table", name, "title", title)
	return id, nil
}

// generateFilterSQL is the single, deterministic FilterSpec → SQL renderer behind
// both PreviewFilterSQL and SaveFilterAsQuery. Unlike GetTableDataFiltered (which
// binds parameters at execution time), a saved query is a standalone SQL string,
// so literal values are safely inlined here. Column identifiers (filter columns
// and the optional projection) are validated against the table's real columns —
// no SQL injection via identifier or value. The caller must hold a.mu.
func (a *App) generateFilterSQL(name string, spec FilterSpec, columns []string) (string, error) {
	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if !isValidIdentifier(name) {
		return "", fmt.Errorf("invalid table name")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	cols, err := engine.TableColumns(db, name)
	if err != nil {
		return "", fmt.Errorf("failed to introspect columns for filter: %w", err)
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c.Name] = true
	}

	// Projection: SELECT * unless the QBE grid selected explicit columns.
	projection := "*"
	if len(columns) > 0 {
		parts := make([]string, 0, len(columns))
		for _, col := range columns {
			if !known[col] {
				return "", fmt.Errorf("unknown selected column %q", col)
			}
			parts = append(parts, engine.QuoteIdent(col))
		}
		projection = strings.Join(parts, ", ")
	}

	whereClause, err := buildFilterWhereInline(spec, known)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT %s FROM %s%s", projection, engine.QuoteIdent(name), whereClause), nil
}

// buildFilterWhereInline mirrors buildFilterWhere but inlines literal values into
// the SQL (for a persistable, standalone query string) instead of binding them as
// parameters. Identifiers are validated against the allowlist; string literals are
// single-quote escaped so a value can never break out of the literal.
func buildFilterWhereInline(spec FilterSpec, known map[string]bool) (string, error) {
	if len(spec.Conditions) == 0 {
		return "", nil
	}

	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	var clauses []string
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", fmt.Errorf("unknown filter column %q", c.Column)
		}
		switch c.Op {
		case "is_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NULL", engine.QuoteIdent(c.Column)))
		case "is_not_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NOT NULL", engine.QuoteIdent(c.Column)))
		default:
			sqlOp, ok := filterOps[c.Op]
			if !ok {
				return "", fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, fmt.Sprintf("%s %s %s", engine.QuoteIdent(c.Column), sqlOp, sqlLiteral(c.Value)))
		}
	}

	return " WHERE " + strings.Join(clauses, " "+combinator+" "), nil
}

// sqlLiteral renders a FilterCondition value as a safe inline SQL literal. Numeric
// and boolean values render bare; everything else (including string-typed numeric
// inputs from the all-string form path) renders as a single-quote-escaped string
// literal, which DuckDB coerces to the column type on comparison.
func sqlLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", t)
	case float32, float64:
		return fmt.Sprintf("%v", t)
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", t), "'", "''") + "'"
	}
}

// DeleteQuery deletes a saved query.
func (a *App) DeleteQuery(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}

	return a.registry.Delete(context.Background(), id)
}

// --- Dashboard ---

// DashboardData holds overview stats.
type DashboardData struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	NotebookCount int    `json:"notebook_count"`
	TableCount    int    `json:"table_count"`
	QueryCount    int    `json:"query_count"`
	FileSize      int64  `json:"file_size"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// GetDashboard returns dashboard overview data.
func (a *App) GetDashboard() (*DashboardData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	d := &DashboardData{}

	if v, err := a.container.Meta("title"); err == nil {
		d.Title = v
	}
	if v, err := a.container.Meta("description"); err == nil {
		d.Description = v
	}
	if v, err := a.container.Meta("created_at"); err == nil {
		d.CreatedAt = v
	}
	if v, err := a.container.Meta("updated_at"); err == nil {
		d.UpdatedAt = v
	}

	db := a.container.SQLiteDB()
	db.QueryRow("SELECT COUNT(*) FROM _vibe_notebooks").Scan(&d.NotebookCount) //nolint:errcheck
	db.QueryRow("SELECT COUNT(*) FROM _vibe_queries").Scan(&d.QueryCount)      //nolint:errcheck

	if duckDB := a.container.DuckDBDB(); duckDB != nil {
		duckDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main'").Scan(&d.TableCount) //nolint:errcheck
	}

	if fi, err := os.Stat(a.filePath); err == nil {
		d.FileSize = fi.Size()
	}

	return d, nil
}

// --- Agent ---

// AgentResult holds the result of an agent query.
type AgentResult struct {
	SQL      string   `json:"sql,omitempty"`
	Columns  []string `json:"columns,omitempty"`
	Rows     [][]any  `json:"rows,omitempty"`
	Analysis string   `json:"analysis,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// SendAgentQuery executes a SQL query directly (MVP — no NL support yet).
func (a *App) SendAgentQuery(query string) (*AgentResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	// SendAgentQuery runs arbitrary raw SQL (DDL/DML) via a.engine.Execute, so it
	// is a Studio-only privileged surface exactly like ExecuteQuery and must be
	// capability-gated in App Mode (P4.4).

	rs, err := a.engine.Execute(context.Background(), query, nil)
	if err != nil {
		return &AgentResult{Error: err.Error()}, nil
	}

	result := &AgentResult{SQL: query}
	for _, col := range rs.Columns {
		result.Columns = append(result.Columns, col.Name)
	}
	for _, row := range rs.Rows {
		cleaned := make([]any, len(row))
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				cleaned[i] = string(b)
			} else {
				cleaned[i] = v
			}
		}
		result.Rows = append(result.Rows, cleaned)
	}
	return result, nil
}

// GetAgentStatus returns agent status info, including the configured LLM
// endpoint so the agent panel can show what it's talking to.
func (a *App) GetAgentStatus() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	status := map[string]any{
		"available": a.container != nil,
		"mode":      a.metaOr("agent_sandbox", "full"),
		"provider":  a.llmCfg.Provider,
		"endpoint":  a.llmCfg.Endpoint,
		"model":     a.llmCfg.Model,
	}
	return status
}

// metaOr reads a _vibe_meta key, returning def when no file is open or the key
// is absent (e.g. files created before the key existed). Caller holds a.mu.
func (a *App) metaOr(key, def string) string {
	if a.container == nil {
		return def
	}
	v, err := a.container.Meta(key)
	if err != nil {
		return def
	}
	return v
}

// AgentConfig surfaces the agent configuration to the UI. Provider/model/endpoint
// are app-global and viper-driven (read-only here — configured via ~/.vibe.yaml
// or VIBE_LLM_* env); only the sandbox is a per-.vibe security setting.
type AgentConfig struct {
	Provider string `json:"provider"` // read-only (viper-driven)
	Model    string `json:"model"`    // read-only (viper-driven)
	Endpoint string `json:"endpoint"` // read-only (viper-driven)
	Sandbox  string `json:"sandbox"`  // editable per-file
}

// GetAgentConfig returns the viper-driven provider/model/endpoint plus the
// per-file sandbox.
func (a *App) GetAgentConfig() (*AgentConfig, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	return &AgentConfig{
		Provider: a.llmCfg.Provider,
		Model:    a.llmCfg.Model,
		Endpoint: a.llmCfg.Endpoint,
		Sandbox:  a.metaOr("agent_sandbox", "full"),
	}, nil
}

// SetAgentConfig persists the per-file agent sandbox. Provider/model/endpoint are
// app-global viper config and are not settable here. Studio-only.
func (a *App) SetAgentConfig(cfg AgentConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if cfg.Sandbox != "" {
		if err := a.container.SetMeta("agent_sandbox", cfg.Sandbox); err != nil {
			return fmt.Errorf("failed to set agent_sandbox: %w", err)
		}
	}
	return nil
}

// AgentToolStep is one tool invocation the agent made during a chat turn.
type AgentToolStep struct {
	Tool    string `json:"tool"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	IsError bool   `json:"isError,omitempty"`
}

// AgentChatResult is the panel-facing result of one agent turn.
type AgentChatResult struct {
	Text  string          `json:"text"`
	Steps []AgentToolStep `json:"steps"`
}

// AgentChat runs one turn of the conversational wizard agent against the
// configured OpenAI-compatible LLM endpoint, driving a real tool-calling loop
// (internal/llm). pinnedContext is optional context the user pinned in the
// panel. Studio-only: the agent can run SQL and mutate schema.
//
// The config snapshot + capability guard happen under a.mu, which is released
// before the (slow, network-bound) LLM call so the runner's own tool
// dispatch loop isn't held up waiting on it. Each individual tool call the
// agent makes re-takes a.mu itself — see agentToolHandler — instead of
// running the whole multi-step turn unlocked.
func (a *App) AgentChat(message, pinnedContext string) (*AgentChatResult, error) {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return nil, fmt.Errorf("no file open")
	}
	cfg := llm.Config{
		Provider: a.llmCfg.Provider,
		Endpoint: a.llmCfg.Endpoint,
		Model:    a.llmCfg.Model,
		APIKey:   a.llmCfg.APIKey,
		Sandbox:  a.metaOr("agent_sandbox", "full"),
	}
	// Chat has no cap of its own beyond whatever the file's agent_sandbox
	// setting allows — ceiling = ModeFull lets Tighter (in
	// agentToolHandler.Handle) resolve to the file's live mode on every
	// dispatch, same as before this fix.
	handler := a.newAgentToolHandler(agents.ModeFull)
	a.mu.RUnlock()

	runner := llm.NewRunner(handler, a.logger)
	res, err := runner.Chat(a.ctx, cfg, message, pinnedContext)
	if err != nil {
		return nil, err
	}
	out := &AgentChatResult{Text: res.Text}
	for _, s := range res.Steps {
		out.Steps = append(out.Steps, AgentToolStep{Tool: s.Tool, Input: s.Input, Output: s.Output, IsError: s.IsError})
	}
	return out, nil
}

// AgentTurn is one prior message of conversation history passed from the panel
// for multi-turn memory.
type AgentTurn struct {
	Role string `json:"role"` // "user" | "agent"
	Text string `json:"text"`
}

// AgentChatStream runs a streaming agent turn. As the agent works it emits
// "agent:stream" Wails events tagged with turnID (kind: delta | tool |
// tool_done | final | error) so the panel can show live progress and tool use.
// history is the prior transcript for multi-turn memory. Studio-only.
func (a *App) AgentChatStream(turnID, message, pinnedContext string, history []AgentTurn) (*AgentChatResult, error) {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return nil, fmt.Errorf("no file open")
	}
	cfg := llm.Config{
		Provider: a.llmCfg.Provider,
		Endpoint: a.llmCfg.Endpoint,
		Model:    a.llmCfg.Model,
		APIKey:   a.llmCfg.APIKey,
		Sandbox:  a.metaOr("agent_sandbox", "full"),
	}
	// Chat has no cap of its own beyond whatever the file's agent_sandbox
	// setting allows — see AgentChat's identical comment.
	handler := a.newAgentToolHandler(agents.ModeFull)
	a.mu.RUnlock()

	hist := make([]llm.Turn, 0, len(history))
	for _, h := range history {
		hist = append(hist, llm.Turn{Role: h.Role, Text: h.Text})
	}

	emit := func(ev llm.StreamEvent) {
		a.emitEvent("agent:stream", map[string]any{
			"turnId":  turnID,
			"kind":    ev.Kind,
			"text":    ev.Text,
			"id":      ev.ID,
			"tool":    ev.Tool,
			"input":   ev.Input,
			"output":  ev.Output,
			"isError": ev.IsError,
		})
	}

	runner := llm.NewRunner(handler, a.logger)
	res, err := runner.ChatStream(a.ctx, cfg, message, pinnedContext, hist, emit)
	if err != nil {
		return nil, err
	}
	out := &AgentChatResult{Text: res.Text}
	for _, s := range res.Steps {
		out.Steps = append(out.Steps, AgentToolStep{Tool: s.Tool, Input: s.Input, Output: s.Output, IsError: s.IsError})
	}
	return out, nil
}

// TaskCellArg / TaskTurnArg / AgentTaskRequest mirror internal/llm's
// TaskCell/TaskTurn/TaskRequest as Wails-bound JSON shapes (Wails generates
// TS types from these, not from internal/llm's — keeping the request/
// response Go types in studio/app.go, like every other binding here, rather
// than exposing internal/llm's types to the frontend directly).
type TaskCellArg struct {
	Type   string `json:"type"`
	SQL    string `json:"sql,omitempty"`
	View   string `json:"view,omitempty"`
	Label  string `json:"label,omitempty"`
	Source string `json:"source,omitempty"`
}

type TaskTurnArg struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// AgentTaskRequest is AgentTask's input. taskID is caller-generated (the
// dialog mints one per open, not per turn — a REFINE/ANSWER-AND-RETRY
// follow-up within the same dialog session reuses it) and is both the
// "agent:task" event-stream correlation key and the CancelAgentTask key.
type AgentTaskRequest struct {
	TaskID          string        `json:"taskId"`
	Mode            string        `json:"mode"`
	Description     string        `json:"description"`
	Cell            *TaskCellArg  `json:"cell,omitempty"`
	Tables          []string      `json:"tables,omitempty"`
	NotebookSummary []string      `json:"notebookSummary,omitempty"`
	FollowUp        []TaskTurnArg `json:"followUp,omitempty"`
}

// AgentTask runs one task-mode turn (#66 part B): a bounded, single-purpose
// loop that authors one notebook cell (create or edit) and returns a
// TaskEnvelope — never free chat prose. Distinct from AgentChat/
// AgentChatStream in every dimension that matters for isolation: its own
// tool set (internal/agents.ModeTask — preview_query/write_cell/
// need_more_info, task-only), its own system prompt (no a2ui block, the
// UNFILTERED skills list so task-* skills are visible), its own event name
// ("agent:task", never "agent:stream" — so a concurrently open chat panel's
// stream can never collide with or be mistaken for a task dialog's), and its
// own cancellation (taskCancel, keyed by TaskID — closing the dialog calls
// CancelAgentTask, not anything that could also cancel a chat turn).
func (a *App) AgentTask(req AgentTaskRequest) (*llm.TaskEnvelope, error) {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return nil, fmt.Errorf("no file open")
	}
	cfg := llm.Config{
		Provider: a.llmCfg.Provider,
		Endpoint: a.llmCfg.Endpoint,
		Model:    a.llmCfg.Model,
		APIKey:   a.llmCfg.APIKey,
		// Task mode ALWAYS runs read-only, regardless of the file's own
		// agent_sandbox setting (review fix, CRITICAL: previously this used
		// the file's sandbox — often "full" — which combined with
		// ListToolsForMode(ModeTask) exposing every unrestricted tool let a
		// task turn call drop_table directly from prompt text). Task mode's
		// whole contract is "propose, never mutate" — write_cell only
		// records a proposal the user must still click Apply on — so nothing
		// it does should ever need more than read access, and forcing
		// read-only here is defense in depth even though
		// agents.TaskAllowedToolNames already excludes every write tool by
		// name.
		Sandbox: llm.SandboxReadOnly,
	}
	// ceiling = ModeReadOnly, NOT cfg.Sandbox/agents.SandboxMode(cfg.Sandbox)
	// — this is the ceiling that must survive every later per-dispatch
	// SetSandboxMode refresh in agentToolHandler.Handle (see its doc
	// comment). Passing anything file-mode-derived here would let that
	// refresh overwrite task mode's "always read-only" contract on the
	// handler's very first tool call, exactly the regression a reviewer
	// caught (a task turn against a "full" file ran CREATE TABLE again).
	handler := a.newAgentToolHandler(agents.ModeReadOnly)
	a.mu.RUnlock()

	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("taskId is required")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.taskCancelMu.Lock()
	if a.taskCancel == nil {
		a.taskCancel = make(map[string]context.CancelFunc)
	}
	a.taskCancel[taskID] = cancel
	a.taskCancelMu.Unlock()
	defer func() {
		a.taskCancelMu.Lock()
		delete(a.taskCancel, taskID)
		a.taskCancelMu.Unlock()
		cancel()
	}()

	var cell *llm.TaskCell
	if req.Cell != nil {
		cell = &llm.TaskCell{Type: req.Cell.Type, SQL: req.Cell.SQL, View: req.Cell.View, Label: req.Cell.Label, Source: req.Cell.Source}
	}
	followUp := make([]llm.TaskTurn, 0, len(req.FollowUp))
	for _, t := range req.FollowUp {
		followUp = append(followUp, llm.TaskTurn{Role: t.Role, Text: t.Text})
	}

	emit := func(ev llm.TaskEvent) {
		a.emitEvent("agent:task", map[string]any{
			"taskId":  taskID,
			"kind":    ev.Kind,
			"text":    ev.Text,
			"id":      ev.ID,
			"tool":    ev.Tool,
			"input":   ev.Input,
			"output":  ev.Output,
			"isError": ev.IsError,
		})
	}

	runner := llm.NewRunner(handler, a.logger)
	envelope, err := runner.Task(ctx, cfg, llm.TaskRequest{
		Mode: req.Mode, Description: req.Description, Cell: cell,
		Tables: req.Tables, NotebookSummary: req.NotebookSummary, FollowUp: followUp,
	}, emit)
	if err != nil {
		return nil, err
	}
	return &envelope, nil
}

// CancelAgentTask stops the AgentTask call running under taskID, if any —
// called when the AI-cell dialog closes or a new request supersedes an
// in-flight one. A no-op if the task already finished or taskID is unknown
// (the dialog closing after the turn already resolved is the common case,
// not an error).
func (a *App) CancelAgentTask(taskID string) {
	a.taskCancelMu.Lock()
	defer a.taskCancelMu.Unlock()
	if cancel, ok := a.taskCancel[taskID]; ok {
		cancel()
		delete(a.taskCancel, taskID)
	}
}

// newAgentToolHandler builds the tool-dispatch Handler that backs a single
// Studio chat turn (AgentChat/AgentChatStream) or a task turn (AgentTask),
// gated by BOTH the file's live "agent_sandbox" setting AND the given
// ceiling — see agentToolHandler.ceiling's doc comment for why a ceiling is
// needed at all (AgentTask's "always read-only" contract must survive the
// per-dispatch file-mode refresh). The underlying *agents.Handler is the
// same type StartMCPServer wires up for external MCP clients
// (internal/agents.NewHandler + engineAgentAdapter), so every caller
// dispatches every tool call through one registry with one set of
// sandbox/approval rules (#63). It is wrapped in agentToolHandler so each
// individual tool call — not just this construction — is locked and
// container-identity-checked (review fix #4). Callers must hold at least
// a.mu.RLock and have already checked a.container != nil.
func (a *App) newAgentToolHandler(ceiling agents.SandboxMode) llm.Handler {
	fileMode := agents.SandboxMode(a.metaOr("agent_sandbox", "full"))
	sc := &agents.SandboxConfig{Mode: agents.Tighter(ceiling, fileMode), MaxQueryRows: 10000}
	return a.wrapAgentHandler(sc, ceiling)
}

// wrapAgentHandler builds an *agents.Handler for the given sandbox and wraps
// it in agentToolHandler (see below) with the given ceiling. Shared by
// newAgentToolHandler (Studio chat/task) and StartMCPServer/RunA2UIQuery
// (their own callers) so every dispatch path gets the same per-call
// locking/container-identity check AND the same ceiling-clamped sandbox
// refresh. Callers must hold at least a.mu.RLock (StartMCPServer holds
// a.mu.Lock, which also suffices) and have already checked
// a.container != nil.
func (a *App) wrapAgentHandler(sc *agents.SandboxConfig, ceiling agents.SandboxMode) *agentToolHandler {
	inner := agents.NewHandler(a.container, &engineAgentAdapter{mgr: a.engine}, sc, a.logger)
	inner.SetActiveNotebook(a.activeNotebookID)
	return &agentToolHandler{app: a, container: a.container, inner: inner, ceiling: ceiling}
}

// agentToolHandler wraps an *agents.Handler with App's file-lifecycle lock.
// internal/agents.Handler methods talk directly to the container's raw
// *sql.DB (via ContainerAccess/EngineAccess), bypassing every other App
// method's a.mu locking entirely. Without this wrapper, a chat turn that
// built its handler once (under a brief RLock) then ran several tool calls
// unlocked could race a concurrent CloseFile/OpenFile/Save — each of which
// takes a.mu.Lock() to swap or nil out a.container — and end up querying a
// closed or already-replaced container mid-turn.
//
// Each dispatch here re-takes a.mu.RLock for exactly the duration of that one
// tool call (not the whole multi-step turn, so a slow model thinking between
// tool calls never blocks Save/Close), and rechecks a.container is still the
// exact *container.Container this handler was built for, erroring instead of
// silently operating on a stale one if the file was closed or replaced
// mid-turn. On a successful data/schema/content-category tool call it also
// notifies the frontend so Dashboard/Tables/etc. refresh instead of going
// stale (review fixes #4 and #6).
type agentToolHandler struct {
	app       *App
	container *container.Container
	inner     *agents.Handler
	// ceiling is the MOST PERMISSIVE sandbox mode this handler may ever run
	// at, regardless of what the file's own "agent_sandbox" setting allows.
	// Every dispatch computes agents.Tighter(ceiling, fileMode) fresh — see
	// Handle/RunReadOnlySQL — rather than just re-reading fileMode alone.
	//
	// This exists because of a real security regression a reviewer caught:
	// once dispatch started re-reading the live file sandbox on every call
	// (to fix MCP running at a hard-coded ModeFull), that SAME refresh
	// silently OVERWROTE AgentTask's "always read-only, regardless of the
	// file's setting" contract on its very first tool call — a task turn
	// against a "full" file could run_query DDL/create_table again, live-
	// proven with a task that CREATE TABLE'd a helper table. AgentTask sets
	// ceiling = ModeReadOnly; AgentChat/AgentChatStream/StartMCPServer/
	// RunA2UIQuery set ceiling = ModeFull (no additional cap beyond
	// whatever the file itself allows, matching their existing contract).
	ceiling agents.SandboxMode
}

// SetMode forwards to the wrapped *agents.Handler, satisfying internal/llm's
// modeSetter interface so Runner.Task can flip this handler into task mode
// (agents.ModeTask) for the duration of one AgentTask call — see
// setHandlerMode in internal/llm/task.go. AgentChat/AgentChatStream never
// call this, so their handler stays at its zero-value mode (task-only tools
// denied) unless something explicitly opts in.
func (h *agentToolHandler) SetMode(mode string) { h.inner.SetMode(mode) }

func (h *agentToolHandler) Handle(ctx context.Context, req *agents.Request) *agents.Response {
	h.app.mu.RLock()
	defer h.app.mu.RUnlock()

	if h.app.container != h.container {
		return agents.NewErrorResponse(req.ID, agents.ErrInternal, "the file was closed or replaced during this agent turn; reopen it and try again")
	}

	// Re-read the live agent_sandbox setting on every dispatch so a sandbox
	// change via SetAgentConfig takes effect on this handler's very next
	// call, not just for handlers built after the change (security fix: MCP
	// previously ran at a hard-coded agents.ModeFull regardless of the
	// file's configured sandbox — StartMCPServer builds ONE agentToolHandler
	// for the server's whole lifetime, so it never re-read the setting).
	// Clamped through h.ceiling (agents.Tighter) so this refresh can never
	// grant a handler more than its ceiling allows — see ceiling's doc
	// comment for the AgentTask regression this closes. SetSandboxMode
	// swaps an atomic pointer inside h.inner rather than mutating a shared
	// *SandboxConfig's fields in place, so this is safe under concurrent
	// MCP calls (go test -race covers it).
	fileMode := agents.SandboxMode(h.app.metaOr("agent_sandbox", "full"))
	h.inner.SetSandboxMode(agents.Tighter(h.ceiling, fileMode))

	// Re-read the live active notebook on every dispatch, not just at
	// construction time: StartMCPServer builds ONE agentToolHandler for the
	// server's whole lifetime (unlike AgentChat/AgentTask, which build a
	// fresh one per turn), so an MCP client's add_query_cell/create_dashboard
	// call minutes after the server started must still see whichever
	// notebook is open in Studio right now, not whichever was open when the
	// server was started (or none).
	h.inner.SetActiveNotebook(h.app.activeNotebookID)
	resp := h.inner.Handle(ctx, req)
	if resp.Error == nil {
		h.app.notifyAgentToolChange(req.Method, resp.Result)
	}
	return resp
}

// RunReadOnlySQL is RunA2UIQuery's dispatch path: the same file-lifecycle
// lock and container-identity check Handle gives every JSON-RPC tool call,
// for agents.Handler.RunReadOnlySQL (a data-bound component's "sql", always
// gated to reads regardless of sandbox mode — see RunA2UIQuery). It never
// calls notifyAgentToolChange: RunReadOnlySQL can only ever read.
func (h *agentToolHandler) RunReadOnlySQL(ctx context.Context, sqlStr string, params map[string]any, maxRows int) (rs *engine.ResultSet, truncated bool, err error) {
	h.app.mu.RLock()
	defer h.app.mu.RUnlock()

	if h.app.container != h.container {
		return nil, false, fmt.Errorf("the file was closed or replaced during this agent turn; reopen it and try again")
	}
	fileMode := agents.SandboxMode(h.app.metaOr("agent_sandbox", "full"))
	h.inner.SetSandboxMode(agents.Tighter(h.ceiling, fileMode))
	return h.inner.RunReadOnlySQL(ctx, sqlStr, params, maxRows)
}

// notifyAgentToolChange emits "data:changed" after a successful agent tool
// call in the data/schema/content categories, so views listening for that event
// (Dashboard, Tables, ...) refresh to reflect what the agent just did — the
// same event every other mutating App binding already emits (review fix #6:
// agent-driven writes previously left the UI stale until a manual refresh).
func (a *App) notifyAgentToolChange(method string, result any) {
	td := agents.GetTool(method)
	if td == nil {
		return
	}
	switch td.Category {
	case "data", "schema", "content":
		a.emitEvent("data:changed")
	}
	// add_query_cell/create_dashboard (#67 deferred part) write a new cell to
	// _vibe_notebooks, not the live DuckDB data/schema "data:changed" covers
	// — an open NotebookView only reloads/merges on "notebooks:changed" (the
	// same event CreateNotebook/SaveNotebook/RenameNotebook/DeleteNotebook/
	// ImportNotebook already emit), so without this an agent-appended cell
	// never appears in an already-open notebook until the user manually
	// reopens it. result is the tool's own JSON-RPC result
	// (handleAddQueryCell/handleCreateDashboard's {notebook_id, cell_id,
	// title, warnings} map) — extracted here so the emitted event carries
	// the SPECIFIC appended cell id (review fix, finding 2): the frontend
	// merges only that id, never "every remote id it doesn't already have",
	// which is what let a locally-deleted cell come back.
	switch method {
	case "add_query_cell", "create_dashboard":
		resultMap, _ := result.(map[string]any)
		notebookID, _ := resultMap["notebook_id"].(string)
		cellID, _ := resultMap["cell_id"].(string)
		if notebookID == "" || cellID == "" {
			a.logger.Warn("agent tool change: missing notebook_id/cell_id in result, not emitting notebooks:changed", "method", method)
			return
		}
		a.emitNotebooksChanged(notebookID, []string{cellID})
	}
}

// --- Import/Export ---

// SelectCSVFile opens a file dialog to select a CSV file.
func (a *App) SelectCSVFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import CSV",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// SpreadsheetColumn describes a single previewed spreadsheet column: its
// detected header name and a suggested DuckDB type for the mapping step.
// STUB for P6.3 (Import Spreadsheet Wizard backend) — not yet implemented.
type SpreadsheetColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SpreadsheetPreview is the result of PreviewSpreadsheet: the workbook's sheet
// list plus the detected columns and sample rows for the requested sheet.
// STUB for P6.3 — not yet implemented.
type SpreadsheetPreview struct {
	Sheets  []string            `json:"sheets"`
	Sheet   string              `json:"sheet"`
	Columns []SpreadsheetColumn `json:"columns"`
	Rows    [][]any             `json:"rows"`
}

// ImportColumnMapping maps a source spreadsheet column to a target table column
// and DuckDB type. STUB for P6.3 — not yet implemented.
type ImportColumnMapping struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// ImportSpreadsheetOptions parameterizes ImportSpreadsheet: source file, sheet,
// header detection, target table, mode ("create" or "append"), and the
// column-type mapping. STUB for P6.3 — not yet implemented.
type ImportSpreadsheetOptions struct {
	Path      string                `json:"path"`
	Sheet     string                `json:"sheet"`
	HasHeader bool                  `json:"has_header"`
	TableName string                `json:"table_name"`
	Mode      string                `json:"mode"`
	Columns   []ImportColumnMapping `json:"columns"`
}

// SelectSpreadsheetFile opens a file dialog for choosing an .xlsx workbook.
// STUB for P6.3 — not yet implemented.
func (a *App) SelectSpreadsheetFile() (string, error) {
	return "", fmt.Errorf("SelectSpreadsheetFile not implemented")
}

// PreviewSpreadsheet returns the workbook's sheet list, the detected headers,
// suggested DuckDB column types, and a bounded sample of rows for the requested
// sheet. It drives the Import Spreadsheet Wizard's sheet-picker and
// field-type-mapping steps (parity plan §6.3).
func (a *App) PreviewSpreadsheet(path, sheet string, hasHeader bool) (*SpreadsheetPreview, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	sheets, err := importer.ListXLSXSheets(path)
	if err != nil {
		return nil, fmt.Errorf("failed to list spreadsheet sheets: %w", err)
	}

	prev, err := importer.PreviewXLSX(db, path, sheet, hasHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to preview spreadsheet: %w", err)
	}

	cols := make([]SpreadsheetColumn, 0, len(prev.Columns))
	for _, c := range prev.Columns {
		cols = append(cols, SpreadsheetColumn{Name: c.Name, Type: c.Type})
	}

	return &SpreadsheetPreview{
		Sheets:  sheets,
		Sheet:   sheet,
		Columns: cols,
		Rows:    prev.Rows,
	}, nil
}

// ImportSpreadsheet loads a spreadsheet sheet into a table per the supplied
// options. Mode "create" builds a new table using the caller's column-type
// mapping (target name + DuckDB type); mode "append" loads rows into an
// existing table without recreating it. Rows are read positionally in the
// source-column order of the mapping (parity plan §6.3).
func (a *App) ImportSpreadsheet(opts ImportSpreadsheetOptions) (*ImportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(opts.TableName) {
		return nil, fmt.Errorf("invalid table name")
	}
	if len(opts.Columns) == 0 {
		return nil, fmt.Errorf("no column mapping provided")
	}
	mode := opts.Mode
	if mode == "" {
		mode = "create"
	}
	if mode != "create" && mode != "append" {
		return nil, fmt.Errorf("invalid mode %q (want create or append)", mode)
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Read the full sheet through the excel extension.
	srcCols, rows, err := importer.ReadXLSXSheet(db, opts.Path, opts.Sheet, opts.HasHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to read spreadsheet: %w", err)
	}

	// Map each mapping entry to a source-column index by name.
	srcIndex := make(map[string]int, len(srcCols))
	for i, c := range srcCols {
		srcIndex[c.Name] = i
	}
	type boundCol struct {
		target string
		typ    string
		srcIdx int
	}
	bound := make([]boundCol, 0, len(opts.Columns))
	for _, m := range opts.Columns {
		target := m.Target
		if target == "" {
			target = m.Source
		}
		if !isValidIdentifier(target) {
			return nil, fmt.Errorf("invalid target column name %q", m.Target)
		}
		typ := strings.TrimSpace(m.Type)
		if typ == "" {
			typ = "VARCHAR"
		}
		idx, ok := srcIndex[m.Source]
		if !ok {
			return nil, fmt.Errorf("source column %q not found in sheet", m.Source)
		}
		bound = append(bound, boundCol{target: target, typ: typ, srcIdx: idx})
	}

	var tableExists int
	db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main' AND table_name = $1", opts.TableName).Scan(&tableExists) //nolint:errcheck

	if mode == "create" {
		if tableExists > 0 {
			if _, err := db.Exec(fmt.Sprintf("DROP TABLE %s", engine.QuoteIdent(opts.TableName))); err != nil {
				return nil, fmt.Errorf("failed to drop existing table: %w", err)
			}
		}
		colDefs := make([]string, 0, len(bound))
		for _, b := range bound {
			colDefs = append(colDefs, fmt.Sprintf("%s %s", engine.QuoteIdent(b.target), b.typ))
		}
		createSQL := fmt.Sprintf("CREATE TABLE %s (%s)", engine.QuoteIdent(opts.TableName), strings.Join(colDefs, ", "))
		if _, err := db.Exec(createSQL); err != nil {
			return nil, fmt.Errorf("failed to create table: %w", err)
		}
	} else if tableExists == 0 {
		return nil, fmt.Errorf("cannot append: table %q does not exist", opts.TableName)
	}

	colNames := make([]string, len(bound))
	placeholders := make([]string, len(bound))
	for i, b := range bound {
		colNames[i] = engine.QuoteIdent(b.target)
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", engine.QuoteIdent(opts.TableName),
		strings.Join(colNames, ", "), strings.Join(placeholders, ", "))

	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare insert: %w", err)
	}
	defer stmt.Close()

	// Insert each row independently so a single constraint violation (e.g. an
	// appended child row whose foreign-key parent is missing) is rejected without
	// poisoning the rest of the import. DuckDB aborts a transaction on a
	// constraint error, so we cannot batch valid rows alongside a violating one.
	imported := 0
	rejected := 0
	for _, row := range rows {
		args := make([]any, len(bound))
		for i, b := range bound {
			if b.srcIdx < len(row) {
				args[i] = row[b.srcIdx]
			}
		}
		if _, err := stmt.Exec(args...); err != nil {
			rejected++
			a.logger.Warn("skipping row that violates a table constraint during import",
				"table", opts.TableName, "sheet", opts.Sheet, "error", err)
			continue
		}
		imported++
	}

	a.logger.Info("imported spreadsheet",
		"table", opts.TableName, "sheet", opts.Sheet, "mode", mode,
		"rows", imported, "rejected", rejected)

	// Skills (#62): write/refresh a workspace-<table> skill with the actual
	// stored schema (row count, column types, null rates, sample values) so
	// the agent has grounded facts about this table without a model call.
	// Best-effort: a failure here must not fail the import itself.
	if err := agents.WriteImportSkill(a.container.SQLiteDB(), db, opts.TableName); err != nil {
		a.logger.Warn("failed to write import skill", "table", opts.TableName, "error", err)
	}

	a.emitEvent("data:changed")
	return &ImportResult{RowsImported: imported, TableName: opts.TableName}, nil
}

// ImportResult holds CSV import results.
type ImportResult struct {
	RowsImported int    `json:"rows_imported"`
	TableName    string `json:"table_name"`
}

// ImportCSV imports a CSV file into a table.
func (a *App) ImportCSV(csvPath, tableName string) (*ImportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	if !isValidIdentifier(tableName) {
		return nil, fmt.Errorf("invalid table name")
	}

	if _, err := os.Stat(csvPath); err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	db := a.container.DuckDBDB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Delegate parsing AND type inference to DuckDB's CSV sniffer. The previous
	// hand-rolled importer split lines on bare commas (breaking quoted fields)
	// and created every column as TEXT, which made numeric aggregates fail and
	// left imported tables unable to participate in relationships.
	escapedPath := strings.ReplaceAll(csvPath, "'", "''")
	readCSV := fmt.Sprintf("read_csv_auto('%s', header=true)", escapedPath)

	var exists int
	db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main' AND table_name = $1", tableName).Scan(&exists) //nolint:errcheck

	var imported int
	if exists == 0 {
		if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", engine.QuoteIdent(tableName), readCSV)); err != nil {
			return nil, fmt.Errorf("failed to import CSV: %w", err)
		}
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", engine.QuoteIdent(tableName))).Scan(&imported); err != nil {
			return nil, fmt.Errorf("failed to count imported rows: %w", err)
		}
	} else {
		// Append into the existing table, matching columns by name so column
		// order in the file doesn't matter.
		res, err := db.Exec(fmt.Sprintf("INSERT INTO %s BY NAME SELECT * FROM %s", engine.QuoteIdent(tableName), readCSV))
		if err != nil {
			return nil, fmt.Errorf("failed to import CSV: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			imported = int(n)
		}
	}

	// Skills (#62): write/refresh a workspace-<table> skill with the actual
	// stored schema. Best-effort — a failure here must not fail the import.
	if err := agents.WriteImportSkill(a.container.SQLiteDB(), db, tableName); err != nil {
		a.logger.Warn("failed to write import skill", "table", tableName, "error", err)
	}

	a.emitEvent("data:changed")
	return &ImportResult{RowsImported: imported, TableName: tableName}, nil
}

// ExportTableCSV exports a table as CSV via save dialog.
func (a *App) ExportTableCSV(tableName string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if !isValidIdentifier(tableName) {
		return "", fmt.Errorf("invalid table name")
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export CSV",
		DefaultFilename: tableName + ".csv",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	rows, err := ddb.Query(fmt.Sprintf("SELECT * FROM %s", engine.QuoteIdent(tableName)))
	if err != nil {
		return "", fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	var b strings.Builder
	b.WriteString(strings.Join(columns, ","))
	b.WriteString("\n")

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}
		fields := make([]string, len(columns))
		for i, v := range values {
			fields[i] = fmt.Sprintf("%v", engine.NormalizeValue(v))
		}
		b.WriteString(strings.Join(fields, ","))
		b.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write CSV: %w", err)
	}
	return path, nil
}

// ExportTableJSON exports a table as JSON via save dialog.
// ExportTableJSON is the "JSON export" row-returning binding
// jsonRoundTripGuard's doc comment refers to — it already marshals to JSON
// itself (json.MarshalIndent below) and checks the error before writing or
// returning, rather than handing a raw Go value back to Wails to marshal on
// its own, so it was already safe against the crash jsonRoundTripGuard
// guards the other row-returning bindings against; no separate call needed
// here.
func (a *App) ExportTableJSON(tableName string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if !isValidIdentifier(tableName) {
		return "", fmt.Errorf("invalid table name")
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export JSON",
		DefaultFilename: tableName + ".json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	rows, err := ddb.Query(fmt.Sprintf("SELECT * FROM %s", engine.QuoteIdent(tableName)))
	if err != nil {
		return "", fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}
		row := make(map[string]any)
		for i, col := range columns {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			} else {
				v = engine.NormalizeValue(v)
			}
			row[col] = v
		}
		result = append(result, row)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write JSON: %w", err)
	}
	return path, nil
}

// --- Metadata ---

// GetAllMeta returns all metadata entries.
func (a *App) GetAllMeta() (map[string]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	db := a.container.SQLiteDB()
	rows, err := db.Query("SELECT key, value FROM _vibe_meta ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("failed to list meta: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("failed to scan meta: %w", err)
		}
		result[k] = v
	}
	return result, rows.Err()
}

// SetMeta sets a metadata key/value.
func (a *App) SetMeta(key, value string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	return a.container.SetMeta(key, value)
}

// --- MCP Server ---

// engineAgentAdapter wraps engine.Manager to satisfy agents.EngineAccess.
type engineAgentAdapter struct {
	mgr *engine.Manager
}

func (ea *engineAgentAdapter) Execute(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error) {
	return ea.mgr.Execute(ctx, sql, params)
}

// StartMCPServer starts the MCP server for the currently open file.
func (a *App) StartMCPServer() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if a.mcpServer != nil {
		return fmt.Sprintf("http://%s/sse", a.mcpServer.Addr()), nil
	}

	sandbox := &agents.SandboxConfig{Mode: agents.SandboxMode(a.metaOr("agent_sandbox", "full")), MaxQueryRows: 10000}
	// ceiling = ModeFull: an external MCP client gets whatever the file's
	// own agent_sandbox setting allows, no additional cap beyond it — see
	// agentToolHandler.ceiling's doc comment.
	handler := a.wrapAgentHandler(sandbox, agents.ModeFull)

	srv := vibemcp.New(handler, a.logger)
	if err := srv.Start("127.0.0.1:0"); err != nil {
		return "", fmt.Errorf("failed to start MCP server: %w", err)
	}

	a.mcpServer = srv
	url := fmt.Sprintf("http://%s/sse", srv.Addr())
	a.logger.Info("MCP server started", "url", url)
	return url, nil
}

// StopMCPServer stops the MCP server.
func (a *App) StopMCPServer() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mcpServer == nil {
		return nil
	}
	err := a.mcpServer.Stop()
	a.mcpServer = nil
	return err
}

// GetMCPStatus returns the current MCP server status, including the
// per-session bearer token (#64/#70) an external MCP client must send as
// `Authorization: Bearer <token>` (or `?token=` for a transport that can't
// set headers) — the server rejects every request without it. Shown in
// SettingsView.vue's MCP panel with a copy button, alongside the URL.
func (a *App) GetMCPStatus() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.mcpServer != nil {
		return map[string]any{
			"running": true,
			"url":     fmt.Sprintf("http://%s/sse", a.mcpServer.Addr()),
			"token":   a.mcpServer.Token(),
		}
	}
	return map[string]any{
		"running": false,
		"url":     "",
		"token":   "",
	}
}

// --- Helpers ---

func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	if strings.HasPrefix(name, "_vibe_") {
		return false
	}
	return true
}

// copyToXLSX writes the result of the given SELECT source to an .xlsx via the
// DuckDB excel extension, falling back to CSV when the extension is unavailable.
// It returns the written path (.xlsx or .csv). Caller must hold a.mu (R or W).
func (a *App) copyToXLSX(source, dst string) (string, error) {
	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	// Best-effort load of the excel extension (bundled/autoloadable in this
	// build). If it is unavailable we fall back to CSV.
	_, _ = ddb.ExecContext(a.ctx, "LOAD excel")

	// COPY (query) TO 'file.xlsx' WITH (FORMAT xlsx). Escape single quotes in the
	// destination path to keep the SQL literal well-formed.
	xlsxPath := dst
	if !strings.HasSuffix(strings.ToLower(xlsxPath), ".xlsx") {
		xlsxPath += ".xlsx"
	}
	copyXLSX := fmt.Sprintf("COPY (%s) TO '%s' WITH (FORMAT xlsx, HEADER true)",
		source, strings.ReplaceAll(xlsxPath, "'", "''"))
	if _, err := ddb.ExecContext(a.ctx, copyXLSX); err == nil {
		return xlsxPath, nil
	} else {
		a.logger.Warn("xlsx export unavailable, falling back to CSV", "error", err)
	}

	// CSV fallback.
	csvPath := strings.TrimSuffix(dst, ".xlsx") + ".csv"
	copyCSV := fmt.Sprintf("COPY (%s) TO '%s' WITH (FORMAT csv, HEADER true)",
		source, strings.ReplaceAll(csvPath, "'", "''"))
	if _, err := ddb.ExecContext(a.ctx, copyCSV); err != nil {
		return "", err
	}
	return csvPath, nil
}

// ExportTableXLSX exports an arbitrary table to an .xlsx via the DuckDB excel
// extension (CSV fallback if unavailable), returning the written path
// (plan §6.2 / P6.5).
func (a *App) ExportTableXLSX(tableName, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}
	if tableName == "" {
		return "", fmt.Errorf("table name is required")
	}

	// Quote the identifier to handle reserved words / case; escape embedded quotes.
	source := fmt.Sprintf(`SELECT * FROM "%s"`, strings.ReplaceAll(tableName, `"`, `""`))
	path, err := a.copyToXLSX(source, dst)
	if err != nil {
		return "", fmt.Errorf("failed to export table %q to spreadsheet: %w", tableName, err)
	}
	a.logger.Info("exported table spreadsheet", "table", tableName, "path", path)
	return path, nil
}

// ExportQueryXLSX exports a saved query's result to an .xlsx via the DuckDB excel
// extension (CSV fallback if unavailable), returning the written path
// (plan §6.2 / P6.5).
func (a *App) ExportQueryXLSX(queryID, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return "", fmt.Errorf("no file open")
	}

	q, err := a.registry.Get(context.Background(), queryID)
	if err != nil {
		return "", fmt.Errorf("failed to load query %q: %w", queryID, err)
	}

	path, err := a.copyToXLSX(q.SQL, dst)
	if err != nil {
		return "", fmt.Errorf("failed to export query %q to spreadsheet: %w", queryID, err)
	}
	a.logger.Info("exported query spreadsheet", "query", queryID, "path", path)
	return path, nil
}

// AnalyzeQueryInExcel is the one-click "Analyze in Excel/Sheets" alias for
// ExportQueryXLSX (plan §6.2 / P6.5).
func (a *App) AnalyzeQueryInExcel(queryID, dst string) (string, error) {
	return a.ExportQueryXLSX(queryID, dst)
}
