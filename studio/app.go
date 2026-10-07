package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/bundle"
	"github.com/sandwich-labs/vibe-app/internal/config"
	"github.com/sandwich-labs/vibe-app/internal/crud"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/hub"
	"github.com/sandwich-labs/vibe-app/internal/importer"
	"github.com/sandwich-labs/vibe-app/internal/llm"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/prefs"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/secrets"
	"github.com/sandwich-labs/vibe-app/internal/session"
	"github.com/sandwich-labs/vibe-app/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the main application struct bound to the Wails frontend.
//
// Studio holds one workspace at a time (#111): the folder (ws), the DuckDB hub
// built over its connections (sess), the hub lock that makes this process the
// only writer (hold, nil when opened read-only), and, when switched on, the
// MCP server others reach the workspace through (wmcp). Notebooks, queries and
// skills are plain files behind ws; data writes go straight to the source.
type App struct {
	ctx    context.Context
	logger *slog.Logger
	mu     sync.RWMutex

	ws     workspace.Workspace
	sess   session.Session
	engine *engine.Manager
	hold   *hub.Hold
	wmcp   *hub.WorkspaceMCP
	// mcpTool is the wrapped handler wmcp serves (what takes a.mu and shows the
	// approval cards); nil while MCP is off.
	mcpTool *agentToolHandler
	watcher *workspace.Watcher
	// watchDone is closed by closeWorkspaceLocked to end the watcher forwarder.
	watchDone chan struct{}
	// issues are the connections that could not be set up (redacted messages).
	issues []workspace.SessionIssue
	// readOnly is true when another process owns the hub: stores stay editable
	// (they are files), local DuckDB files are not reachable. holder says who.
	readOnly bool
	holder   *hub.Holder
	// duplicateOf lists other known workspace folders that carry this one's id.
	duplicateOf []string

	// connSigs is, per connection, a signature of the definition and sign-in
	// the session was last registered from (connections.go syncConnections):
	// a change on disk or in the editor re-registers just that connection.
	connSigMu sync.Mutex
	// syncMu serialises whole syncs (an editor save and the file watcher can race).
	syncMu   sync.Mutex
	connSigs map[string]string

	// initial is what was named on the command line (or opened from the OS
	// before the window was ready): a folder, a .vibe bundle, or a note.
	// frontendReady flips when the frontend first asks for it; OS open
	// requests after that are sent as events (HandleOpenPath).
	launchMu      sync.Mutex
	initial       InitialWorkspace
	frontendReady bool

	// sessionID identifies the CURRENTLY open workspace. It is regenerated on
	// every successful open and cleared on close. A frontend view that reads
	// data at load time (NotebookView.vue's loadedSessionId) stamps itself
	// with the value in effect at that moment and sends it back with every
	// write (SaveNotebook); the backend rejects a write whose token does not
	// match the CURRENT sessionID. That closes a real data-corruption bug: a
	// still-mounted view of the PREVIOUS workspace let an in-flight debounced
	// save land in the NEW one after a switch. The token check is the one
	// guard a caller that skips the frontend's own flush cannot bypass.
	sessionID string

	// activeNotebookID is the id of the notebook currently open in
	// NotebookView.vue, if any — set via SetActiveNotebook. Consulted by
	// add_query_cell/create_dashboard's "append to the open notebook" fallback
	// via agents.Handler.SetActiveNotebook.
	activeNotebookID string

	// taskCancel holds one context.CancelFunc per in-flight AgentTask call,
	// keyed by the frontend-generated taskID — so closing the AI-cell dialog
	// (CancelAgentTask) can stop a task turn without touching a.ctx (the
	// whole app's lifecycle context) or any concurrently running chat turn.
	taskCancelMu sync.Mutex
	taskCancel   map[string]context.CancelFunc

	// emit publishes a frontend event. In production it is nil and emitEvent
	// falls back to the Wails runtime.EventsEmit (which requires the real Wails
	// lifecycle context). Tests inject a sink so the open lifecycle can be
	// asserted without a live Wails runtime.
	emit func(event string, data ...any)

	// prefs stores app-global user preferences (theme, appearance, the recent
	// workspaces list) outside any workspace — see internal/prefs. nil in tests
	// that don't need it.
	prefs *prefs.Store

	// llmCfg is the app-global, viper-driven LLM provider/model configuration
	// for the wizard agent (loaded at startup from ~/.vibe.yaml + VIBE_* env).
	llmCfg config.LLMConfig

	// secrets resolves credentials (keychain, env, prompt) and owns the
	// redactor that scrubs resolved secret values from logs and errors
	// (#114). keyCache holds the resolved LLM key briefly; see secrets.go.
	secrets  *secrets.Resolver
	keyCache llmKeyCache
	llmSnap  atomic.Pointer[config.LLMConfig]

	// quitConfirmed gates beforeClose: the FIRST window-close/quit attempt is
	// intercepted (OnBeforeClose returns true, preventing it) so the frontend
	// gets a chance to flush any pending debounced saves via the
	// "app:before-close" event; once it has (or a timeout fires), it calls
	// Quit(), which sets this flag so the NEXT beforeClose lets the close
	// through.
	quitConfirmed atomic.Bool

	// closePending is true while a close attempt is already in flight (the
	// frontend is flushing, or the watchdog is armed); a second attempt while
	// it is true quits immediately (see beforeClose).
	closePending atomic.Bool

	// closeWatchdog and quitFn are testing seams for beforeClose's timeout
	// logic: a hung webview never responds to "app:before-close", and the
	// window must still close. Production leaves both nil and beforeClose
	// lazily defaults them to a real time.AfterFunc and the real Quit().
	closeWatchdog func(d time.Duration, f func()) (stop func())
	quitFn        func()
	// watchdogStop stops the armed watchdog (HoldQuit, CancelQuit).
	watchdogMu   sync.Mutex
	watchdogStop func()

	// approvals is the one process-lifetime registry backing every pending
	// write-approval card across every open chat turn and the in-Studio MCP
	// server (#79) — see agentToolHandler.enableApprovals and
	// ResolveAgentApproval. It outlives any single workspace (a close or switch
	// cancels whatever is pending on it before the session goes away).
	approvals *agents.ApprovalQueue

	// locateVibe finds the `vibe` CLI for the Claude config snippets; tests
	// replace it (nil means hub.LocateVibe).
	locateVibe func() (string, error)

	// approvalTimeout bounds how long a pending approval card waits for an
	// answer before the tool call gives up. An App field, not a package
	// constant, so tests can shorten it instead of waiting out 5 minutes.
	approvalTimeout time.Duration

	// bundles holds .vibe bundles that were previewed and await their commit (#118).
	bundles bundleStore
}

// defaultApprovalTimeout is how long an approval card waits for the user
// before the tool call it's blocking reports "no answer, nothing changed".
const defaultApprovalTimeout = 5 * time.Minute

// closeWatchdogTimeout bounds how long beforeClose waits for the frontend to
// respond to "app:before-close" (flush pending saves, then call Quit())
// before quitting anyway — a hung/suspended webview must never be able to
// make the window impossible to close.
const closeWatchdogTimeout = 5 * time.Second

// NewApp creates a new App instance. The LLM config is loaded eagerly so it is
// available even before the Wails startup hook (and in tests).
func NewApp(logger *slog.Logger) *App {
	red := secrets.NewRedactor()
	logger = slog.New(secrets.NewRedactingHandler(logger.Handler(), red))
	app := &App{
		logger:          logger,
		secrets:         secrets.NewResolver(secrets.WithRedactor(red), secrets.WithResolverLogger(logger), secrets.WithEnv(workspace.SnapshotEnv())),
		llmCfg:          config.Load().LLM,
		approvals:       agents.NewApprovalQueue(),
		approvalTimeout: defaultApprovalTimeout,
	}
	app.setLLMSnapshot(app.llmCfg)
	if dir, err := prefs.DefaultDir(); err == nil {
		app.prefs = prefs.NewStore(dir)
	} else {
		logger.Warn("preferences disabled: no config dir", "error", err)
	}
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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
// set and otherwise the Wails runtime.
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

// shutdown runs when the process quits: MCP off, session closed, then the hub
// lock released last, so the next owner can open the DuckDB files the moment
// the lock is free.
func (a *App) shutdown(_ context.Context) {
	if a.approvals != nil {
		a.approvals.CancelAll()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closeWorkspaceLocked("")
}

// beforeClose is wired as Wails' OnBeforeClose option (studio/main.go). It
// intercepts the first window-close/quit attempt and returns true
// (preventing the close) while emitting "app:before-close" so the frontend can
// flush any pending debounced saves (e.g. a notebook's autosave). The
// frontend then calls Quit(); Quit() sets quitConfirmed so THIS same check
// lets the next close attempt through instead of looping forever.
//
// A one-shot watchdog timer (closeWatchdogTimeout) force-quits if the
// frontend never responds — a hung or suspended webview process must not be
// able to make the window impossible to close. A SECOND close attempt while
// one is already pending lets the close through immediately.
func (a *App) beforeClose(_ context.Context) (prevent bool) {
	if a.quitConfirmed.Load() {
		return false
	}

	if a.closePending.CompareAndSwap(false, true) {
		a.emitEvent("app:before-close", map[string]any{})
		watchdog := a.closeWatchdog
		if watchdog == nil {
			watchdog = func(d time.Duration, f func()) func() {
				t := time.AfterFunc(d, f)
				return func() { t.Stop() }
			}
		}
		stop := watchdog(closeWatchdogTimeout, a.Quit)
		a.watchdogMu.Lock()
		a.watchdogStop = stop
		a.watchdogMu.Unlock()
		return true
	}

	a.quitConfirmed.Store(true)
	return false
}

// Quit is called by the frontend once it has flushed (or timed out waiting
// on) every pending save in response to "app:before-close", and by
// beforeClose's own watchdog timer if the frontend never responds at all. It
// marks the close as confirmed and asks Wails to quit again, which this time
// reaches beforeClose with quitConfirmed already set.
func (a *App) Quit() {
	a.quitConfirmed.Store(true)
	quit := a.quitFn
	if quit == nil {
		quit = func() { runtime.Quit(a.ctx) }
	}
	quit()
}

// CancelQuit abandons a close attempt (the person chose to stay because a
// notebook could not be saved); the next close starts the flow again.
func (a *App) CancelQuit() {
	a.HoldQuit()
	a.closePending.Store(false)
}

// HoldQuit stops the hung-webview watchdog. The frontend calls it before it
// asks "Quit anyway?": a frontend that can show a question is not hung, and the
// timer must not quit the app while the person is deciding.
func (a *App) HoldQuit() {
	a.watchdogMu.Lock()
	stop := a.watchdogStop
	a.watchdogStop = nil
	a.watchdogMu.Unlock()
	if stop != nil {
		stop()
	}
}

// errNoWorkspace is what every binding that needs an open workspace returns
// when there is none.
var errNoWorkspace = errors.New("no workspace open")

// requireWorkspace is the one gate every workspace-bound binding runs first.
// The caller holds a.mu (read or write).
func (a *App) requireWorkspace() error {
	if a.ws == nil {
		return errNoWorkspace
	}
	return nil
}

// --- Skills (#62) ---

// SkillSummary is one skill as the Skills view lists it.
type SkillSummary struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	TableName    string `json:"table_name,omitempty"`
	Source       string `json:"source"` // built-in | user | import
	// Rev is the store's revision as listed; SaveSkill sends it back so a
	// save from a stale copy is a conflict (file-backed workspaces).
	Rev string `json:"rev,omitempty"`
}

// ListSkills returns every skill visible to chat: built-ins plus stored
// user/import skills that pass table gating (a table-bound skill only
// appears once its table exists and has rows). Mirrors exactly what the
// agent's list_skills tool would return, so the Skills view shows the same
// set the model can actually see.
func (a *App) ListSkills() ([]SkillSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	entries, err := agents.LoadSkills(a.ws.Skills(), a.sess, a.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to list skills: %w", err)
	}
	visible := agents.ChatVisibleSkills(entries)
	out := make([]SkillSummary, 0, len(visible))
	for _, s := range visible {
		out = append(out, SkillSummary{
			Name: s.Name, Description: s.Description, Instructions: s.Instructions,
			TableName: s.TableName, Source: s.Source, Rev: s.Rev,
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
func (a *App) SaveSkill(name, description, instructions, tableName, rev string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if _, err := agents.SaveUserSkillAt(a.ws.Skills(), a.sess, name, description, instructions, tableName, rev); err != nil {
		return conflictErr(err)
	}
	a.emitEvent("data:changed")
	return nil
}

// DeleteSkill removes a user- or import-authored skill. Built-ins can't be
// deleted (they aren't rows in _vibe_skills).
func (a *App) DeleteSkill(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := agents.DeleteSkill(a.ws.Skills(), name); err != nil {
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return a.ws.Notebooks().List(context.Background())
}

// GetNotebook loads one notebook, rebuilt through notebooks.Coerce.
func (a *App) GetNotebook(id string) (*notebooks.Notebook, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	nb, warnings, err := a.ws.Notebooks().Get(context.Background(), id)
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	nb, err := a.ws.Notebooks().Create(context.Background(), title)
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
// sessionID must be the value GetSessionID() returned at the time
// the caller LOADED the notebook it is now saving (see App.sessionID's doc
// comment) — never re-fetched at save time, or this check couldn't catch
// the bug it exists for (a save that fires after the file underneath it was
// already switched). A mismatch means the file was closed/reopened/switched
// since this notebook was loaded, so the write is rejected rather than
// silently landing in the wrong container.
func (a *App) SaveNotebook(raw map[string]any, sessionID string) (*NotebookSaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if sessionID != a.sessionID {
		return nil, fmt.Errorf("stale workspace session: the open workspace changed since this notebook was loaded; reopen it and try again")
	}
	nb, warnings, err := a.ws.Notebooks().Save(context.Background(), raw)
	if err != nil {
		return nil, conflictErr(err)
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &NotebookSaveResult{Notebook: nb, Warnings: warnings}, nil
}

// RenameNotebook updates a notebook's title without touching its cells.
func (a *App) RenameNotebook(id, title string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := a.ws.Notebooks().Rename(context.Background(), id, title); err != nil {
		return err
	}
	a.emitNotebooksChanged(id, nil)
	return nil
}

// DeleteNotebook removes a notebook by id.
func (a *App) DeleteNotebook(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := a.ws.Notebooks().Delete(context.Background(), id); err != nil {
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
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return "", err
	}
	nb, warnings, err := a.ws.Notebooks().Get(context.Background(), id)
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
// maxImportNotebookFileBytes caps an imported notebook file at the same
// per-entry limit a .vibe bundle applies (bundle.MaxEntryBytes, #118), so a
// single notebook and a bundled one share one validation path (review
// fix, step 4a #7c) — checked via os.Stat BEFORE the file is read, so a
// multi-hundred-MB file (or a symlink/fifo pointed at one) never gets
// fully loaded into memory just to be rejected.
const maxImportNotebookFileBytes = bundle.MaxEntryBytes

func (a *App) ImportNotebook(path string) (*NotebookSaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
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

	// The same checks a notebook in a bundle gets (bundle.CheckNotebook), then
	// the store's own Save.
	inner, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("invalid notebook file: %w", err)
	}
	if _, _, err := bundle.CheckNotebook(inner); err != nil {
		return nil, err
	}
	nb, warnings, err := a.ws.Notebooks().Save(context.Background(), wrapper)
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &NotebookSaveResult{Notebook: nb, Warnings: warnings}, nil
}

// --- Tables ---

// ctxOrBackground is a.ctx, or Background before startup / in tests.
func (a *App) ctxOrBackground() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// tableRefArg validates a table identifier arriving from the frontend: a bare
// name (default catalog) or schema.table / catalog.schema.table (an attached
// connection). Every part is validated and quoted when SQL is built.
//
// The returned reference is fully resolved (catalog, schema, table) through the
// session, so the SQL built from it never depends on the connection's current
// catalog or on a same-named temp table shadowing a bare name. An unknown
// connection ("system", "temp", a typo) is refused. The caller holds a.mu and
// has checked a.ws != nil.
func (a *App) tableRefArg(name string) (session.TableRef, error) {
	ref, err := engine.ParseTable(name)
	if err != nil {
		return ref, fmt.Errorf("invalid table name")
	}
	if sess := a.sess; sess != nil {
		resolved, rerr := sess.Resolve(a.ctxOrBackground(), ref)
		if rerr != nil {
			return ref, rerr
		}
		ref = resolved
	}
	return ref, nil
}

// isDefaultTarget reports whether ref is in the session's default catalog and
// default schema (where a bare sequence name resolves).
func (a *App) isDefaultTarget(ref session.TableRef) bool {
	sess := a.sess
	if sess == nil {
		return false
	}
	schema, err := sess.DefaultSchema(a.ctxOrBackground(), "")
	return err == nil && ref.Catalog == sess.DefaultCatalog() && ref.Schema == schema
}

// ListTables returns all user tables of the default catalog (non _vibe_ tables).
func (a *App) ListTables() ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	sess := a.sess
	if sess == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	tabs, err := sess.ListTables(a.ctxOrBackground(), "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	var tables []string
	for _, t := range tabs {
		tables = append(tables, t.Name)
	}
	return tables, nil
}

// ListCatalogTables returns the tables of one connection's default schema,
// qualified (catalog.schema.table) so each name can be passed straight to
// DescribeTable / GetTableData / InsertRow / ... A connection of "" means the
// default catalog.
func (a *App) ListCatalogTables(connection string) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	sess := a.sess
	if sess == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	tabs, err := sess.ListTables(a.ctxOrBackground(), connection, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, t.Ref().String())
	}
	return out, nil
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	tref, err := a.tableRefArg(name)
	if err != nil {
		return nil, err
	}

	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	// Shared introspection (internal/schema): fills real PRIMARY KEY membership
	// from duckdb_constraints() — the previous inline query always reported
	// PK=false, leaving the schema tree / ER diagram to guess by column name.
	cols, err := engine.TableColumns(db, tref)
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	sess := a.sess
	if sess == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	tabs, err := sess.ListTables(a.ctxOrBackground(), "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	out := make([]TableStat, 0, len(tabs))
	for _, t := range tabs {
		var count int64
		if err := sess.DB().QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", t.Ref().SQL())).Scan(&count); err != nil {
			return nil, fmt.Errorf("failed to count rows of %q: %w", t.Name, err)
		}
		out = append(out, TableStat{Name: t.Name, Rows: count})
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

	if err := a.requireWorkspace(); err != nil {
		return "", err
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

	ddb := a.sess.DB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	copySQL := buildCopySQL(sqlStr, dst, copyOpts)
	if _, err := ddb.Exec(copySQL); err != nil {
		return "", fmt.Errorf("failed to export query results: %w", err)
	}
	a.logger.Info("exported query results", "format", format, "path", dst)
	return dst, nil
}

// TableData holds paginated table data.
type TableData struct {
	Columns []string `json:"columns"`
	// Types is the DuckDB type name of each column, parallel to Columns
	// — the datasheet needs it to pick an editor (date input vs
	// text), align/format cells, and round-trip a DECIMAL's scale.
	Types      []string `json:"types"`
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	tref, err := a.tableRefArg(name)
	if err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	var totalRows int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tref.SQL())).Scan(&totalRows); err != nil {
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
	query := fmt.Sprintf("SELECT * FROM %s%s LIMIT ? OFFSET ?", tref.SQL(), orderClause)
	rows, err := db.Query(query, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	rs, _, err := engine.ScanRows(rows, 0)
	if err != nil {
		return nil, err
	}
	columns, types := rs.NamesAndTypes()

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	result := &TableData{
		Columns:    columns,
		Types:      types,
		Rows:       rs.Rows,
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Build an allowlist of real columns to validate filter/sort identifiers.
	cols, err := engine.TableColumns(db, tref)
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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", tref.SQL(), whereClause)
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
	query := fmt.Sprintf("SELECT * FROM %s%s%s LIMIT ? OFFSET ?", tref.SQL(), whereClause, orderClause)
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query filtered table: %w", err)
	}
	defer rows.Close()

	rs, _, err := engine.ScanRows(rows, 0)
	if err != nil {
		return nil, err
	}
	columns, types := rs.NamesAndTypes()

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	result := &TableData{
		Columns:    columns,
		Types:      types,
		Rows:       rs.Rows,
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
// column's type (via information_schema). Constraints (NOT NULL, CHECK, keys)
// come from the database itself; a violation is returned as a field-keyed
// *WriteError where it can be attributed, so the UI can highlight the offending
// controls instead of surfacing a raw SQL string.
func (a *App) InsertRow(table string, values map[string]any) (*WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ddb, colTypes, metas, table, err := a.prepareWrite(table)
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

	ddb, colTypes, metas, table, err := a.prepareWrite(table)
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
// needed by the transactional write helpers.
func (a *App) prepareWrite(table string) (*sql.DB, map[string]string, map[string]crud.FieldMeta, string, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, nil, nil, "", err
	}
	tref, err := a.tableRefArg(table)
	if err != nil {
		return nil, nil, nil, "", err
	}
	ddb := a.sess.DB()
	if ddb == nil {
		return nil, nil, nil, "", fmt.Errorf("DuckDB not available")
	}
	// Writes address the table by its fully resolved name (catalog.schema.table),
	// so neither the connection's current catalog nor a same-named temp table
	// can redirect them.
	resolved := tref.String()
	colTypes, err := columnTypes(ddb, resolved)
	if err != nil {
		return nil, nil, nil, "", err
	}
	// Field metadata (validation, defaults) was an Access-era _vibe_ table that
	// workspaces do not have: writes are plain type-coerced row edits.
	return ddb, colTypes, map[string]crud.FieldMeta{}, resolved, nil
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

	ddb, colTypes, _, table, err := a.prepareWrite(table)
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

// CreateTable creates a new table with the given columns. Integer primary-key
// columns are made auto-numbering via a backing sequence (the Access AutoNumber
// behavior) so data-entry forms can omit the id.
func (a *App) CreateTable(name string, columns []ColumnInfo) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return err
	}
	for _, col := range columns {
		if !isValidIdentifier(col.Name) {
			return fmt.Errorf("invalid column name %q", col.Name)
		}
	}

	var colDefs, seqDDL []string
	for _, col := range columns {
		def := fmt.Sprintf("%s %s", engine.QuoteIdent(col.Name), col.Type)
		if col.PK && engine.IsIntegerType(col.Type) {
			// The sequence lives next to its table, in the same catalog and
			// schema. A bare table keeps the legacy nextval('seq') default text.
			seq := engine.AutoNumberSequence(tref.Name, col.Name)
			seqRef := session.TableRef{Catalog: tref.Catalog, Schema: tref.Schema, Name: seq}
			seqDDL = append(seqDDL, fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s START 1", seqRef.SQL()))
			if a.isDefaultTarget(tref) {
				def += fmt.Sprintf(" DEFAULT nextval('%s')", seq)
			} else {
				def += fmt.Sprintf(" DEFAULT nextval(%s)", session.QuoteLiteral(seqRef.SQL()))
			}
		}
		if col.PK {
			def += " PRIMARY KEY"
		}
		if col.NotNull {
			def += " NOT NULL"
		}
		colDefs = append(colDefs, def)
	}

	query := fmt.Sprintf("CREATE TABLE %s (%s)", tref.SQL(), strings.Join(colDefs, ", "))
	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	for _, ddl := range seqDDL {
		if _, err := ddb.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create id sequence: %w", err)
		}
	}
	_, err = ddb.Exec(query)
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

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return err
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err = ddb.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tref.SQL()))
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
	// TotalRows is the query's true row count; RowCount is how many are
	// actually in Rows (capped at queryGridRowCap). #91: a query like
	// `SELECT * FROM range(100000)` used to materialize every row into the
	// webview and could crash it.
	TotalRows int `json:"total_rows"`
}

// queryGridRowCap bounds how many rows the Query editor's grid will
// materialize in one ExecuteQuery call — independent of a2uiQueryRowCap
// (a2ui components render a compact inline view; the query grid is a full
// result set the user pages through). #84 adds a cancel button/context on
// top of this.
const queryGridRowCap = 5000

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

// humanDuration renders a query's elapsed time the way the run bar shows
// it: sub-millisecond as "<1 ms" (duration.String() would print something
// like "312.4µs", a unit the grid never uses elsewhere), whole milliseconds
// below a second as "%d ms", and a second or more as "%.2f s".
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1 ms"
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
}

func (a *App) ExecuteQuery(sqlStr string) (*QueryResult, error) {
	return a.RunQuery(RunQueryRequest{SQL: sqlStr})
}

// RunQueryRequest is the query editor's run (#117, SPEC 08). Connection is the
// "Run on" choice of this editor tab: the catalog unqualified names resolve
// against, pinned to one pooled connection for this run (USE is per
// connection). Empty is the workspace default. Qualified names
// (prod_pg.public.orders) work whatever it is, and may span connections.
type RunQueryRequest struct {
	SQL        string `json:"sql"`
	Connection string `json:"connection"`
	// TabID names the editor tab; it only labels the run (the choice is never
	// session-global).
	TabID string `json:"tabId"`
}

// RunQuery runs the query editor's statement on the chosen connection.
func (a *App) RunQuery(req RunQueryRequest) (*QueryResult, error) {
	if req.Connection != "" {
		if err := session.ValidateName(req.Connection); err != nil {
			return nil, fmt.Errorf("unknown connection %q", req.Connection)
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	sqlStr := req.SQL
	before := statusSnapshot(a.sess)
	start := time.Now()
	rs, total, err := a.engine.ExecuteCappedIn(context.Background(), req.Connection, sqlStr, nil, queryGridRowCap)
	a.afterRun(a.sess, before, sqlStr)
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	result := &QueryResult{
		Duration:  humanDuration(duration),
		RowCount:  rs.RowCount(),
		TotalRows: total,
		Rows:      rs.Rows,
	}

	for _, col := range rs.Columns {
		result.Columns = append(result.Columns, ColumnInfo{
			Name: col.Name,
			Type: col.Type,
		})
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
	// Types is the DuckDB type name of each column, parallel to Columns
	// — A2UITable/A2UIStat need it to format a cell (DECIMAL scale,
	// alignment) instead of guessing from typeof value.
	Types []string `json:"types"`
	Rows  [][]any  `json:"rows"`
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
	return a.RunA2UIQueryIn(sqlStr, "")
}

// RunA2UIQueryIn is RunA2UIQuery for a binding (or notebook sql cell) that
// names a connection (notebook format v2): the statement runs with that
// connection's catalog as the default for unqualified table names, on a
// pinned connection. An empty connection is the session default, exactly
// RunA2UIQuery. Every gate (read-only classification, row cap, timeout)
// applies identically.
func (a *App) RunA2UIQueryIn(sqlStr, connection string) (*A2UIQueryResult, error) {
	if connection != "" {
		if err := session.ValidateName(connection); err != nil {
			return nil, fmt.Errorf("unknown connection %q", connection)
		}
	}
	a.mu.RLock()
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	// ceiling = ModeFull: RunReadOnlySQL (below) forces read-only regardless
	// of the sandbox mode, via its own checkSQLClassReadOnly gate — see this
	// function's doc comment — so no additional cap is needed here.
	handler := a.newAgentToolHandler(agents.ModeFull)
	runSess := a.sess
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
	before := statusSnapshot(runSess)
	rs, truncated, err := handler.RunReadOnlySQLIn(ctx, connection, sqlStr, nil, a2uiQueryRowCap)
	a.afterRun(runSess, before, sqlStr)
	if err != nil {
		return nil, a.explainReadOnlyDenial(err)
	}

	out := &A2UIQueryResult{Truncated: truncated}
	out.Columns, out.Types = rs.NamesAndTypes()
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	qs, err := a.ws.Queries().List(context.Background())
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	return a.ws.Queries().Get(context.Background(), id)
}

// QuerySaveResult is what SaveQuery returns: the query's id and the revision
// the store now holds for it (empty for a legacy .vibe), which the editor sends
// back with its next save.
type QuerySaveResult struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// SaveQuery creates or updates a saved query, returning its id so the
// caller (QueryEditorView) can select and highlight it without a second
// round trip (#91: the id used to be discarded, so a freshly-created query
// couldn't become "selected").
func (a *App) SaveQuery(id, title, sqlStr, description, rev string) (*QuerySaveResult, error) {
	return a.saveQuery(id, title, sqlStr, description, rev, nil)
}

// SaveQueryRequest is SaveQuery with the editor's "Run on" choice (#117): the
// connection is saved with the query (its front matter) so a teammate runs it
// on the same connection. An empty Connection clears it (the workspace default).
type SaveQueryRequest struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	SQL         string `json:"sql"`
	Description string `json:"description"`
	Rev         string `json:"rev"`
	Connection  string `json:"connection"`
}

// SaveQueryOn creates or updates a saved query together with its connection.
func (a *App) SaveQueryOn(req SaveQueryRequest) (*QuerySaveResult, error) {
	if req.Connection != "" {
		if err := session.ValidateName(req.Connection); err != nil {
			return nil, fmt.Errorf("unknown connection %q", req.Connection)
		}
	}
	return a.saveQuery(req.ID, req.Title, req.SQL, req.Description, req.Rev, &req.Connection)
}

func (a *App) saveQuery(id, title, sqlStr, description, rev string, connection *string) (*QuerySaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	q := &queries.Query{
		ID:          id,  // "" lets the store assign one
		Rev:         rev, // the revision the editor loaded; a stale one is a conflict
		Title:       title,
		Description: description,
		SQL:         sqlStr,
		Engine:      "duckdb",
		Parameters:  []queries.Param{},
	}
	// The editor only edits title and SQL: whatever else the file carries (the
	// connection a CLI or agent chose, parameters, a description) is kept.
	if id != "" {
		if base, err := a.ws.Queries().Get(context.Background(), id); err == nil && base != nil {
			q.Connection, q.Parameters, q.Engine = base.Connection, base.Parameters, base.Engine
			if description == "" {
				q.Description = base.Description
			}
		}
	}
	if connection != nil {
		q.Connection = *connection
	}

	// Update first for an existing query; a new one (or an id the store does
	// not know) is created. Anything else, a conflict on a file that changed
	// on disk included, is returned to the caller instead of being retried as
	// a create.
	if id != "" {
		err := a.ws.Queries().Update(context.Background(), q)
		if err == nil {
			return &QuerySaveResult{ID: q.ID, Rev: q.Rev}, nil
		}
		if !errors.Is(err, queries.ErrNotFound) {
			return nil, conflictErr(err)
		}
	}
	if err := a.ws.Queries().Create(context.Background(), q); err != nil {
		return nil, err
	}
	return &QuerySaveResult{ID: q.ID, Rev: q.Rev}, nil
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

	q := &queries.Query{
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := a.ws.Queries().Create(context.Background(), q); err != nil {
		return "", fmt.Errorf("failed to save filter as query: %w", err)
	}
	a.logger.Info("saved filter as named query", "id", q.ID, "table", name, "title", title)
	return q.ID, nil
}

// generateFilterSQL is the single, deterministic FilterSpec → SQL renderer behind
// both PreviewFilterSQL and SaveFilterAsQuery. Unlike GetTableDataFiltered (which
// binds parameters at execution time), a saved query is a standalone SQL string,
// so literal values are safely inlined here. Column identifiers (filter columns
// and the optional projection) are validated against the table's real columns —
// no SQL injection via identifier or value. The caller must hold a.mu.
func (a *App) generateFilterSQL(name string, spec FilterSpec, columns []string) (string, error) {
	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return "", err
	}

	db := a.sess.DB()
	if db == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	cols, err := engine.TableColumns(db, tref)
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

	return fmt.Sprintf("SELECT %s FROM %s%s", projection, tref.SQL(), whereClause), nil
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

	if err := a.requireWorkspace(); err != nil {
		return err
	}

	return a.ws.Queries().Delete(context.Background(), id)
}

// --- Dashboard ---

// DashboardData holds overview stats.
type DashboardData struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	NotebookCount int    `json:"notebook_count"`
	TableCount    int    `json:"table_count"`
	QueryCount    int    `json:"query_count"`
	SkillCount    int    `json:"skill_count"`
	// LocalTables are the tables of the workspace's default connection (its own
	// `local` database unless the workspace says otherwise), as qualified names
	// the datasheet accepts (design #106 item 8: Home lists them).
	LocalTables []string `json:"local_tables"`
	// DefaultConnection names the connection LocalTables belong to.
	DefaultConnection string `json:"default_connection"`
	ReadOnly          bool   `json:"read_only"`
}

// GetDashboard returns the Home screen's live overview: the workspace's name,
// its counts and its own tables.
func (a *App) GetDashboard() (*DashboardData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	d := &DashboardData{
		Title:             a.ws.Name(),
		DefaultConnection: a.ws.DefaultConnection(),
		ReadOnly:          a.readOnly,
		LocalTables:       []string{},
	}
	ctx := a.ctxOrBackground()
	if nbs, err := a.ws.Notebooks().List(ctx); err == nil {
		d.NotebookCount = len(nbs)
	}
	if qs, err := a.ws.Queries().List(ctx); err == nil {
		d.QueryCount = len(qs)
	}
	if sk, err := a.ws.Skills().List(ctx); err == nil {
		d.SkillCount = len(sk)
	}
	if tabs, err := a.sess.ListTables(ctx, "", ""); err == nil {
		d.TableCount = len(tabs)
		for _, t := range tabs {
			d.LocalTables = append(d.LocalTables, t.Ref().String())
		}
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

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	// SendAgentQuery runs arbitrary raw SQL (DDL/DML) via a.engine.Execute, so it
	// is a Studio-only privileged surface exactly like ExecuteQuery and must be
	// capability-gated in App Mode (P4.4).

	rs, err := a.engine.Execute(context.Background(), query, nil)
	if err != nil {
		return &AgentResult{Error: err.Error()}, nil
	}

	// NormalizeValue (inside engine.ScanRows, which a.engine.Execute already
	// ran) owns []byte -> string now, so rs.Rows needs no per-cell cleanup
	// here — the same fix ExecuteQuery got.
	result := &AgentResult{SQL: query}
	result.Columns, _ = rs.NamesAndTypes()
	result.Rows = rs.Rows
	return result, nil
}

// GetAgentStatus returns agent status info, including the configured LLM
// endpoint so the agent panel can show what it's talking to.
func (a *App) GetAgentStatus() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	policy := a.agentPolicy()
	status := map[string]any{
		"available":       a.ws != nil,
		"mode":            string(policy.Mode),
		"askBeforeWrites": policy.AskBeforeWrites,
		"readOnly":        a.readOnly,
		"provider":        a.llmCfg.Provider,
		"endpoint":        a.llmCfg.Endpoint,
		"model":           a.llmCfg.Model,
	}
	return status
}

// agentPolicy resolves the open workspace's effective agent policy for this
// person — the committed ceiling and this person's own setting, whichever is
// stricter (workspace.EffectivePolicy). It is the one place every caller in
// this file reads it. Caller holds at least a.mu.RLock. With no workspace open
// it reports the fail-safe default.
func (a *App) agentPolicy() agents.FilePolicy {
	if a.ws == nil {
		return agents.FilePolicy{Mode: agents.DefaultSandboxMode, AskBeforeWrites: true}
	}
	policy := agents.PolicyFrom(a.ws)
	if a.readOnly {
		// A workspace opened without owning the hub cannot change data, whatever
		// the settings say.
		policy.Mode = agents.ModeReadOnly
	}
	return policy
}

// AgentConfig surfaces the agent configuration to the UI. Provider/model/endpoint
// are app-global and viper-driven (read-only here — configured via ~/.vibe.yaml
// or VIBE_LLM_* env); the rest is per workspace: Sandbox and AskBeforeWrites are
// what is in effect for this person (the committed team limit and this
// person's own choice, whichever is stricter), TeamSandbox and TeamAsk are the
// committed limit alone.
type AgentConfig struct {
	Provider        string `json:"provider"`        // read-only (viper-driven)
	Model           string `json:"model"`           // read-only (viper-driven)
	Endpoint        string `json:"endpoint"`        // read-only (viper-driven)
	Sandbox         string `json:"sandbox"`         // effective tier
	AskBeforeWrites bool   `json:"askBeforeWrites"` // effective gate
	// TeamSandbox is the highest tier vibe.yaml allows ("" means no limit).
	TeamSandbox string `json:"teamSandbox"`
	// TeamAskBeforeWrites is true when vibe.yaml requires the gate for everyone.
	TeamAskBeforeWrites bool `json:"teamAskBeforeWrites"`
}

// GetAgentConfig returns the viper-driven provider/model/endpoint plus the
// workspace's agent settings.
func (a *App) GetAgentConfig() (*AgentConfig, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	policy := a.agentPolicy()
	cfg := a.ws.Config()
	return &AgentConfig{
		Provider:            a.llmCfg.Provider,
		Model:               a.llmCfg.Model,
		Endpoint:            a.llmCfg.Endpoint,
		Sandbox:             string(policy.Mode),
		AskBeforeWrites:     policy.AskBeforeWrites,
		TeamSandbox:         cfg.Sandbox,
		TeamAskBeforeWrites: cfg.AskBeforeWrites != nil && *cfg.AskBeforeWrites,
	}, nil
}

// SetAgentConfig saves THIS PERSON's agent settings for the workspace (the
// "On this computer" layer, kept outside the folder). It can lower the tier
// freely and raise it only up to the team limit in vibe.yaml, and it cannot turn
// the gate off when vibe.yaml requires it. Provider/model/endpoint are
// app-global config and are not settable here. Studio-only.
func (a *App) SetAgentConfig(cfg AgentConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := a.saveLocalAgentLocked(cfg.Sandbox, cfg.AskBeforeWrites); err != nil {
		return err
	}
	policy := a.agentPolicy()
	a.emitEvent("agent:config", map[string]any{"sandbox": string(policy.Mode), "askBeforeWrites": policy.AskBeforeWrites})
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
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	policy := a.agentPolicy()
	cfg := llm.Config{
		Provider:        a.llmCfg.Provider,
		Endpoint:        a.llmCfg.Endpoint,
		Model:           a.llmCfg.Model,
		APIKey:          a.llmAPIKey(),
		Sandbox:         string(policy.Mode),
		AskBeforeWrites: policy.AskBeforeWrites,
	}
	// Chat has no cap of its own beyond whatever the file's agent_sandbox
	// setting allows — ceiling = ModeFull lets Tighter (in
	// agentToolHandler.Handle) resolve to the file's live mode on every
	// dispatch, same as before this fix.
	//
	// AgentChat (the non-streaming binding) is unused by the frontend
	// (AgentPanel/AgentView both call AgentChatStream) and never gets an
	// approver attached — a write needing approval here is simply refused,
	// fail-safe, exactly like a headless MCP client (#79 D5).
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
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	policy := a.agentPolicy()
	cfg := llm.Config{
		Provider:        a.llmCfg.Provider,
		Endpoint:        a.llmCfg.Endpoint,
		Model:           a.llmCfg.Model,
		APIKey:          a.llmAPIKey(),
		Sandbox:         string(policy.Mode),
		AskBeforeWrites: policy.AskBeforeWrites,
	}
	// Chat has no cap of its own beyond whatever the file's agent_sandbox
	// setting allows — see AgentChat's identical comment.
	handler := a.newAgentToolHandler(agents.ModeFull)
	// AgentChatStream is the one surface with a human actually watching this
	// turn, so it's the only place an approver is ever attached (#79 D4) —
	// every write the model calls for during this turn now blocks on an
	// inline "agent:approval" card instead of running (or, absent this,
	// being refused outright the way MCP/AgentChat are).
	handler.enableApprovals(turnID)
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
	// Connection is the connection the cell runs on ("" = the workspace default);
	// the model is told to keep it (#117).
	Connection string `json:"connection,omitempty"`
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

// taskCellFrom converts the cell the person is editing into the task's cell. A
// connection name is validated before it can reach the prompt.
func taskCellFrom(c *TaskCellArg) (*llm.TaskCell, error) {
	if c == nil {
		return nil, nil
	}
	if c.Connection != "" {
		if err := session.ValidateName(c.Connection); err != nil {
			return nil, fmt.Errorf("unknown connection %q", c.Connection)
		}
	}
	return &llm.TaskCell{Type: c.Type, SQL: c.SQL, View: c.View, Label: c.Label, Source: c.Source, Connection: c.Connection}, nil
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
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	cfg := llm.Config{
		Provider: a.llmCfg.Provider,
		Endpoint: a.llmCfg.Endpoint,
		Model:    a.llmCfg.Model,
		APIKey:   a.llmAPIKey(),
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
	// SetFilePolicy refresh in agentToolHandler.Handle (see its doc
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

	cell, err := taskCellFrom(req.Cell)
	if err != nil {
		return nil, err
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
// gated by BOTH the workspace's live policy AND the given ceiling — see
// agentToolHandler.ceiling's doc comment for why a ceiling is needed at all
// (AgentTask's "always read-only" contract must survive the per-dispatch
// policy refresh). The underlying *agents.Handler is the same one
// hub.NewAgentHandler builds for `vibe mcp serve` and Studio's own MCP server,
// so every caller dispatches every tool call through one registry with one set
// of sandbox/approval rules (#63). It is wrapped in agentToolHandler so each
// individual tool call — not just this construction — is locked and
// workspace-identity-checked. Callers must hold at least a.mu.RLock and have
// already checked a.ws != nil.
func (a *App) newAgentToolHandler(ceiling agents.SandboxMode) *agentToolHandler {
	tighten := ""
	if ceiling != agents.ModeFull {
		tighten = string(ceiling)
	}
	inner, _, err := hub.NewAgentHandler(a.ws, a.sess, tighten, a.logger)
	if err != nil {
		// Unreachable: every ceiling is one of the three valid modes. Fail
		// closed anyway.
		a.logger.Error("failed to build the agent handler", "error", err)
		sc := &agents.SandboxConfig{Mode: agents.ModeReadOnly, MaxQueryRows: 10000, RequireApproval: true}
		inner = agents.NewHandler(workspace.NewAgentAccess(a.ws, a.sess), engine.NewManager(a.sess, engine.WithLogger(a.logger)), sc, a.logger)
	}
	inner.SetActiveNotebook(a.activeNotebookID)
	return &agentToolHandler{app: a, ws: a.ws, sess: a.sess, inner: inner, ceiling: ceiling}
}

// agentToolHandler wraps an *agents.Handler with App's workspace-lifecycle
// lock. internal/agents.Handler methods talk directly to the hub's raw
// *sql.DB, bypassing every other App method's a.mu locking entirely. Without
// this wrapper, a chat turn that built its handler once (under a brief RLock)
// then ran several tool calls unlocked could race a concurrent
// CloseWorkspace/OpenWorkspace — each of which takes a.mu.Lock() to swap or
// nil out a.ws — and end up querying a closed or already-replaced session
// mid-turn.
//
// Each dispatch here re-takes a.mu.RLock for exactly the duration of that one
// tool call (not the whole multi-step turn, so a slow model thinking between
// tool calls never blocks a close), and rechecks a.ws and a.sess are still the
// exact ones this handler was built for, erroring instead of silently
// operating on a stale one if the workspace was closed or replaced mid-turn.
// On a successful data/schema/content-category tool call it also notifies the
// frontend so Home/Tables/etc. refresh instead of going stale.
type agentToolHandler struct {
	app   *App
	ws    workspace.Workspace
	sess  session.Session
	inner *agents.Handler
	// ceiling is the MOST PERMISSIVE sandbox mode this handler may ever run
	// at, regardless of what the workspace's own policy allows. Every dispatch
	// computes agents.Tighter(ceiling, policy) fresh — see Handle/RunReadOnlySQL
	// — rather than just re-reading the policy alone.
	//
	// This exists because of a real security regression a reviewer caught: once
	// dispatch started re-reading the live policy on every call, that SAME
	// refresh silently OVERWROTE AgentTask's "always read-only, regardless of
	// the setting" contract on its very first tool call. AgentTask sets
	// ceiling = ModeReadOnly; AgentChat/AgentChatStream/StartMCP/RunA2UIQuery
	// set ceiling = ModeFull (no additional cap beyond what the policy allows).
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

	if h.app.ws != h.ws || h.app.sess != h.sess {
		return agents.NewErrorResponse(req.ID, agents.ErrInternal, "the workspace was closed or replaced during this agent turn; reopen it and try again")
	}

	// Re-read the live policy on every dispatch so a settings change takes
	// effect on this handler's very next call, not just for handlers built
	// after the change. Clamped through h.ceiling (agents.Tighter) so this
	// refresh can never grant a handler more than its ceiling allows. The
	// policy is swapped atomically inside h.inner, so this is safe under
	// concurrent MCP calls (go test -race covers it).
	policy := h.app.agentPolicy()
	h.inner.SetFilePolicy(agents.Tighter(h.ceiling, policy.Mode), policy.AskBeforeWrites)

	// Re-read the live active notebook on every dispatch: Studio's MCP server
	// builds ONE handler for its whole lifetime, so an MCP client's
	// add_query_cell/create_dashboard call minutes after the server started
	// must still see whichever notebook is open in Studio right now.
	h.inner.SetActiveNotebook(h.app.activeNotebookID)
	resp := h.inner.Handle(ctx, req)
	if resp.Error == nil {
		h.app.notifyAgentToolChange(req.Method, resp.Result)
	}
	return resp
}

// RunReadOnlySQL is RunA2UIQuery's dispatch path: the same workspace-lifecycle
// lock and identity check Handle gives every JSON-RPC tool call, for
// agents.Handler.RunReadOnlySQL (a data-bound component's "sql", always gated
// to reads regardless of sandbox mode — see RunA2UIQuery). It never calls
// notifyAgentToolChange: RunReadOnlySQL can only ever read.
func (h *agentToolHandler) RunReadOnlySQL(ctx context.Context, sqlStr string, params map[string]any, maxRows int) (rs *engine.ResultSet, truncated bool, err error) {
	return h.RunReadOnlySQLIn(ctx, "", sqlStr, params, maxRows)
}

// RunReadOnlySQLIn is RunReadOnlySQL for a statement bound to a connection.
func (h *agentToolHandler) RunReadOnlySQLIn(ctx context.Context, connection, sqlStr string, params map[string]any, maxRows int) (rs *engine.ResultSet, truncated bool, err error) {
	h.app.mu.RLock()
	defer h.app.mu.RUnlock()

	if h.app.ws != h.ws || h.app.sess != h.sess {
		return nil, false, fmt.Errorf("the workspace was closed or replaced during this agent turn; reopen it and try again")
	}
	policy := h.app.agentPolicy()
	h.inner.SetFilePolicy(agents.Tighter(h.ceiling, policy.Mode), policy.AskBeforeWrites)
	return h.inner.RunReadOnlySQLIn(ctx, connection, sqlStr, params, maxRows)
}

// enableApprovals installs an approver on h that surfaces each pending write
// as an "agent:approval" event tagged with turnID, and blocks until the
// frontend answers via ResolveAgentApproval (or the request times out/is
// cancelled). Chat calls it once per turn (turnID = the turn); Studio's own MCP
// server calls it once with turnID "mcp", whose cards the shell shows.
//
// Invariant this whole thing depends on: the returned func is only ever
// invoked from inside h.Handle (via h.inner's approver, itself only called
// from agents.Handler.Handle/handleRunQuery), which holds h.app.mu.RLock()
// for its whole dispatch. Releasing that RLock for the DURATION of the wait
// — the h.app.mu.RUnlock()/RLock() pair below — is what keeps close/switch/
// settings responsive while a card sits pending; without it, every one of
// those (all a.mu.Lock() callers) would queue behind this one in-flight tool
// call for up to approvalTimeout. h.app.mu is an RWMutex, so releasing this
// goroutine's read lock while OTHER concurrent readers may still hold theirs
// is safe.
func (h *agentToolHandler) enableApprovals(turnID string) {
	h.inner.SetApprover(func(ctx context.Context, req agents.ApprovalRequest) error {
		timeout := h.app.approvalTimeout
		if timeout <= 0 {
			timeout = defaultApprovalTimeout
		}
		ctx2, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// sess captures the session this write was approved against (while the
		// lock is still held, below), so the post-wait check can tell a
		// swapped session apart from an unchanged one.
		sess := h.sess

		// The RUnlock happens inside announce, which Wait calls exactly once,
		// after it registers req and before it blocks. So by the time a close
		// holds a.mu.Lock, this waiter is registered and its CancelAll under
		// the lock reaches it.
		err := h.app.approvals.Wait(ctx2, req, func(r agents.ApprovalRequest) {
			h.app.mu.RUnlock()
			h.app.emitEvent("agent:approval", map[string]any{"turnId": turnID, "request": r})
		})
		h.app.mu.RLock()

		// Re-check identity BEFORE deriving the outcome: the workspace may have
		// been closed or replaced while this goroutine had the lock released,
		// and that must win over whatever Wait itself returned — otherwise a
		// Resolve(true) that raced the swap would emit "approved" for a change
		// that must never run on the new workspace.
		if h.app.ws != h.ws || h.app.sess != sess {
			err = agents.ErrApprovalCancelled
		}

		outcome := "approved"
		switch {
		case errors.Is(err, agents.ErrApprovalDeclined):
			outcome = "declined"
		case errors.Is(err, agents.ErrApprovalExpired):
			outcome = "expired"
		case errors.Is(err, agents.ErrApprovalCancelled):
			outcome = "cancelled"
		}
		h.app.emitEvent("agent:approval_done", map[string]any{"turnId": turnID, "id": req.ID, "outcome": outcome})

		if err != nil {
			return err
		}

		// Refresh h.inner's sandbox with the CURRENT policy before reporting
		// "approved": the wait can span minutes with a.mu released, during
		// which Settings may have tightened the tier or turned ask back on.
		// A human did approve this exact change, but "approved" must still
		// mean "still allowed" when it actually runs.
		policy := h.app.agentPolicy()
		h.inner.SetFilePolicy(agents.Tighter(h.ceiling, policy.Mode), policy.AskBeforeWrites)
		return nil
	})
}

// ResolveAgentApproval answers a pending approval card (id, from the
// "agent:approval" event's request.id) with the user's decision. Deliberately
// takes no a.mu — the goroutine it wakes up (enableApprovals' approver) is
// the one holding the file lock, and this call must never queue behind that.
func (a *App) ResolveAgentApproval(id string, approved bool) error {
	if a.approvals == nil || !a.approvals.Resolve(id, approved) {
		return fmt.Errorf("this request is no longer waiting")
	}
	return nil
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

// SelectImportFile opens a file dialog to select a data file for import
// (CSV/TSV or Excel). Replaces SelectCSVFile and the old
// SelectSpreadsheetFile stub (which always returned "not implemented" —
// the xlsx Browse button was broken in the real app before this).
func (a *App) SelectImportFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import data",
		Filters: []runtime.FileFilter{
			{DisplayName: "Data files (*.csv, *.tsv, *.xlsx)", Pattern: "*.csv;*.tsv;*.xlsx"},
			{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"},
			{DisplayName: "Excel Files (*.xlsx)", Pattern: "*.xlsx"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// InspectImport stages the whole file and returns its columns (with
// inferred types and primary-key candidates), a preview of the first 50
// rows, and the total row count — the data behind the wizard's preview
// grid. Read-only: it never writes to the container, only to the DuckDB
// session's own temp catalog, so it takes RLock like any other query.
func (a *App) InspectImport(src importer.Source) (*importer.Inspection, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	return importer.Inspect(a.ctx, db, src)
}

// ImportFile runs one import per the supplied options — create, replace,
// append, or upsert — pre-flighted and written atomically by
// importer.Import. Replaces ImportCSV, ImportSpreadsheet, and
// PreviewSpreadsheet.
//
// A dry run (o.DryRun) only writes to the DuckDB session's temp catalog, so
// it takes RLock; a real run takes the full Lock. Only a real, successful
// run (!DryRun && summary.OK) marks the file dirty, refreshes the
// workspace-<table> skill, and emits data:changed — a dry run or a
// pre-flight failure leaves the file exactly as it was.
func (a *App) ImportFile(o importer.Options) (*importer.Summary, error) {
	// No isValidIdentifier check here: importer.Import validates o.Table
	// itself (engine.ValidIdentifier, the same function isValidIdentifier
	// wraps) and returns a clean "invalid table name" error — a second
	// check here would just be two call sites that could quietly diverge.
	if o.DryRun {
		a.mu.RLock()
		defer a.mu.RUnlock()
	} else {
		a.mu.Lock()
		defer a.mu.Unlock()
	}

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	summary, err := importer.Import(a.ctx, db, o)
	if err != nil {
		return nil, err
	}

	if !o.DryRun && summary.OK {
		// The write already landed atomically inside importer.Import's own
		// transaction — there is no "mark dirty as writes begin" window to
		// worry about here, unlike the old per-row importer.
		// summary.Table, not o.Table: for append/upsert/replace into an
		// existing table, Import resolves and returns the CATALOG's own
		// spelling (a case-insensitive match, e.g. o.Table="customers"
		// against a table actually named "Customers") — writing the skill
		// under the caller's original casing could create a second,
		// wrong-table skill instead of refreshing the real one's.
		if err := agents.WriteImportSkill(a.ws.Skills(), a.sess, summary.Table); err != nil {
			a.logger.Warn("failed to write import skill", "table", summary.Table, "error", err)
		}
		a.emitEvent("data:changed")
	}

	return summary, nil
}

// exportTableTo writes `SELECT * FROM table` to dst via DuckDB COPY with the
// given WITH-clause options, the same mechanism ExportQueryResults already
// uses (buildCopySQL). Extracted from ExportTableCSV/ExportTableJSON
// so the datasheet export is DuckDB-native and testable without a save
// dialog: the two previously hand-rolled their own scan loops (Go %v with
// no CSV quoting — a comma in a value broke the file — and a second,
// slightly different NormalizeValue-based loop for JSON), which could and
// did disagree with what the grid itself shows for the same row. Caller
// owns table-name validation; this only builds and runs the COPY.
func (a *App) exportTableTo(table, dst, copyOpts string) error {
	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	copySQL := buildCopySQL(fmt.Sprintf("SELECT * FROM %s", engine.TableSQL(table)), dst, copyOpts)
	if _, err := ddb.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to export table: %w", err)
	}
	return nil
}

// ExportTableCSV exports a table as CSV via save dialog.
func (a *App) ExportTableCSV(tableName string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	if _, err := a.tableRefArg(tableName); err != nil {
		return "", err
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

	if err := a.exportTableTo(tableName, path, "FORMAT csv, HEADER true"); err != nil {
		return "", err
	}
	return path, nil
}

// ExportTableJSON exports a table as JSON via save dialog.
func (a *App) ExportTableJSON(tableName string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	if _, err := a.tableRefArg(tableName); err != nil {
		return "", err
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

	if err := a.exportTableTo(tableName, path, "FORMAT json, ARRAY true"); err != nil {
		return "", err
	}
	return path, nil
}

// --- Helpers ---

// buildCopySQL wraps a user/saved query in COPY (…) TO '<dst>' WITH
// (<opts>). Trailing whitespace and semicolons are stripped (a query that
// runs fine with a trailing ";" must export too — DuckDB rejects a second
// statement inside the COPY's parenthesized subquery), and the closing
// paren goes on its own line so a trailing "-- comment" in query can't
// swallow it. dst's single quotes are doubled so a path containing one
// stays a single, well-formed string literal.
//
// This does not try to parse comments out of query: a "SELECT 1; -- x"
// still fails, and DuckDB's own parser error says so — good enough, and
// safer than attempting to strip comments ourselves.
func buildCopySQL(query, dst, opts string) string {
	query = strings.TrimRight(query, " \t\r\n;")
	return fmt.Sprintf("COPY (%s\n) TO '%s' WITH (%s)",
		query, strings.ReplaceAll(dst, "'", "''"), opts)
}

// isValidIdentifier is engine.ValidIdentifier under Studio's existing name
// (used throughout this file) — see its doc comment. Kept as a thin
// wrapper, not a second implementation, so the two can never quietly
// diverge on what counts as a safe identifier.
func isValidIdentifier(name string) bool {
	return engine.ValidIdentifier(name)
}

// copyToXLSX writes the result of the given SELECT source to an .xlsx via the
// DuckDB excel extension (fetched once on first use). It never falls back to another format: failure is an error.
// It returns the written .xlsx path. Caller must hold a.mu (R or W).
func (a *App) copyToXLSX(source, dst string) (string, error) {
	ddb := a.sess.DB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	// The excel extension is not autoloaded (the hub runs hardened, with an
	// app-private extension directory): fetch and load it explicitly. A
	// failure is returned to the user; the export never silently becomes a
	// different format.
	if err := session.EnsureExtension(a.ctx, ddb, "excel", "Exporting to Excel"); err != nil {
		return "", err
	}

	// COPY (query) TO 'file.xlsx' WITH (FORMAT xlsx). Escape single quotes in the
	// destination path to keep the SQL literal well-formed.
	xlsxPath := dst
	if !strings.HasSuffix(strings.ToLower(xlsxPath), ".xlsx") {
		xlsxPath += ".xlsx"
	}
	copyXLSX := buildCopySQL(source, xlsxPath, "FORMAT xlsx, HEADER true")
	if _, err := ddb.ExecContext(a.ctx, copyXLSX); err != nil {
		return "", fmt.Errorf("failed to export to Excel: %w", err)
	}
	return xlsxPath, nil
}

// ExportTableXLSX exports an arbitrary table to an .xlsx via the DuckDB excel
// extension (an error if it cannot be fetched), returning the written path
// (plan §6.2 / P6.5).
func (a *App) ExportTableXLSX(tableName, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
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
// extension (an error if it cannot be fetched), returning the written path
// (plan §6.2 / P6.5).
func (a *App) ExportQueryXLSX(queryID, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}

	q, err := a.ws.Queries().Get(context.Background(), queryID)
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

// --- Preferences (app-global, not per-file) ---

// prefsStore returns the preferences store, or an error when the app was
// built without one (no user config dir).
func (a *App) prefsStore() (*prefs.Store, error) {
	if a.prefs == nil {
		return nil, fmt.Errorf("preferences are unavailable: no user config directory")
	}
	return a.prefs, nil
}

// GetPreferences returns the user's app-level preferences (theme, appearance).
// A missing or unreadable settings file yields the defaults; a parse error is
// logged, never surfaced, so a bad edit can't lock the user out of the UI.
func (a *App) GetPreferences() (prefs.Preferences, error) {
	s, err := a.prefsStore()
	if err != nil {
		return prefs.Defaults(), nil
	}
	p, err := s.Load()
	if err != nil {
		a.logger.Warn("preferences: falling back to defaults", "error", err)
	}
	return p, nil
}

// SetPreferences persists the user's app-level preferences.
func (a *App) SetPreferences(p prefs.Preferences) error {
	s, err := a.prefsStore()
	if err != nil {
		return err
	}
	// The frontend round-trips only the fields it knows (theme, appearance);
	// the recent-workspaces list lives beside them in the same document and must
	// survive a theme change.
	recentsMu.Lock()
	defer recentsMu.Unlock()
	cur, _ := s.Load()
	extra := map[string]json.RawMessage{}
	for k, v := range p.Extra {
		extra[k] = v
	}
	// Recents always come from disk: a stale copy round-tripped by the frontend
	// must never roll them back.
	if raw, ok := cur.Extra[prefsRecentKey]; ok {
		extra[prefsRecentKey] = raw
	} else {
		delete(extra, prefsRecentKey)
	}
	p.Extra = extra
	if err := s.Save(p); err != nil {
		return fmt.Errorf("failed to save preferences: %w", err)
	}
	return nil
}

// UserThemesResult is what ListUserThemes returns: every readable theme file
// plus a human-readable line per file that was skipped.
type UserThemesResult struct {
	Themes   []prefs.ThemeFile `json:"themes"`
	Problems []string          `json:"problems"`
	Dir      string            `json:"dir"`
}

// ListUserThemes scans <config dir>/vibe/themes/*.json. Validation happens in
// the frontend (studio/frontend/src/theme/schema.ts) so the rules live once.
func (a *App) ListUserThemes() (UserThemesResult, error) {
	s, err := a.prefsStore()
	if err != nil {
		return UserThemesResult{}, nil
	}
	themes, problems := s.ListUserThemes()
	return UserThemesResult{Themes: themes, Problems: problems, Dir: s.ThemesDir()}, nil
}

// OpenThemesDir creates the themes folder (with a README describing the
// format) and opens it in the OS file manager. Returns the path either way.
func (a *App) OpenThemesDir() (string, error) {
	s, err := a.prefsStore()
	if err != nil {
		return "", err
	}
	dir, err := s.EnsureThemesDir()
	if err != nil {
		return "", err
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, "file://"+dir)
	}
	return dir, nil
}

// explainReadOnlyDenial turns the classifier's generic "live bindings are
// read-only" refusal into the real reason when this window opened the workspace
// read-only: its own database is in use by another program, so the tables are
// simply not reachable yet.
func (a *App) explainReadOnlyDenial(err error) error {
	if !strings.Contains(err.Error(), "live bindings are read-only") {
		return err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.readOnly || a.ws == nil {
		return err
	}
	who := "another program"
	if a.holder != nil {
		who = a.holder.String()
	}
	return fmt.Errorf("%s is in use by %s, so this can't run yet. Take over the workspace to read it", a.ws.DefaultConnection(), who)
}
