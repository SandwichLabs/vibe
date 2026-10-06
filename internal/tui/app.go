// Package tui is Vibe Studio's terminal interface (#69): a SQL REPL, a
// read-only table browser, and a notebook list. It is deliberately narrow —
// the page editor/wizard/renderer that used to live here were removed along
// with the page system (#66/#69); notebooks and A2UI dashboards are authored
// in the Wails Studio app, not the TUI.
package tui

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// mode selects which of the three top-level views is active.
type mode int

const (
	modeREPL mode = iota
	modeTables
	modeNotebooks
)

func (m mode) String() string {
	switch m {
	case modeREPL:
		return "REPL"
	case modeTables:
		return "Tables"
	case modeNotebooks:
		return "Notebooks"
	default:
		return "?"
	}
}

// ContainerAccess is the minimal container surface the TUI needs.
type ContainerAccess interface {
	// Notebooks is the notebook store (nil when there is none).
	Notebooks() notebooks.Store
	DuckDBDB() *sql.DB
	Session() session.Session
	Save() error
	Close() error
}

// EngineAccess is the minimal query-execution surface the TUI needs.
type EngineAccess interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error)
}

// App is the top-level BubbleTea model for Vibe Studio's TUI.
type App struct {
	container ContainerAccess
	engine    EngineAccess
	logger    *slog.Logger

	mode mode

	repl      REPLModel
	tables    TablesModel
	notebooks NotebooksModel

	keys     KeyMap
	helpView help.Model
	width    int
	height   int
	showHelp bool
	quitting bool
	status   string // one-line transient status (e.g. save result), cleared on next keypress

	// unsaved tracks whether a write has happened in this session (a REPL
	// statement that wasn't a plain read) since the last successful Save.
	// Drives the status-bar indicator, and whether quitting auto-saves.
	unsaved bool
}

// NewApp creates a new TUI application model.
func NewApp(container ContainerAccess, eng EngineAccess, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{
		container: container,
		engine:    eng,
		logger:    logger,
		mode:      modeREPL,
		repl:      NewREPLModel(),
		tables:    NewTablesModel(),
		notebooks: NewNotebooksModel(),
		keys:      DefaultKeyMap(),
		helpView:  newHelpModel(),
		width:     80,
		height:    24,
	}
	// The app starts in modeREPL directly (not through setMode), so the
	// REPL's textinput must be focused here too — bubbles/textinput drops
	// every keystroke while unfocused, which otherwise made the REPL
	// silently un-typeable on a fresh app until the user tabbed away and
	// back (setMode is the only other place that calls repl.Focus()).
	a.repl.Focus()
	return a
}

// LoadData loads the table list and notebook list from the container.
func (a *App) LoadData() {
	if a.container == nil {
		return
	}
	a.tables.LoadTables(a.container.Session())
	a.notebooks.LoadNotebooks(a.container.Notebooks())
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.propagateDimensions()
		return a, nil
	case replSubmitMsg:
		return a, a.executeREPLQuery(msg.query)
	case replResultMsg:
		a.repl.AppendOutput(msg.output)
		if msg.wrote {
			a.unsaved = true
		}
		return a, nil
	case tableDataMsg:
		a.tables.SetData(msg)
		return a, nil
	case saveResultMsg:
		if msg.err != "" {
			a.status = "save failed: " + msg.err
		} else {
			a.status = "saved"
			a.unsaved = false
		}
		if msg.thenQuit {
			a.quitting = true
			return a, tea.Quit
		}
		return a, nil
	}
	return a, nil
}

func (a *App) propagateDimensions() {
	mainHeight := a.height - 2 // -1 status bar, -1 mode bar
	if mainHeight < 4 {
		mainHeight = 4
	}
	a.repl.SetSize(a.width, mainHeight)
	a.tables.SetSize(a.width, mainHeight)
	a.notebooks.SetSize(a.width, mainHeight)
}

func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return a, a.quit()
	// ctrl+s saves in every mode, including the REPL — unlike the bare "s"
	// shortcut (which the REPL's textinput owns for typing), ctrl+s is never
	// a character a query could contain, so it's safe to intercept globally
	// ahead of the showHelp/mode dispatch below.
	case "ctrl+s":
		a.status = ""
		return a, a.save(false)
	}

	if a.showHelp {
		a.showHelp = false
		return a, nil
	}

	a.status = ""

	switch msg.String() {
	case "?":
		// "?" only opens help when the active view isn't capturing free-text
		// input — in the REPL, "?" must type into the query (e.g. "SELECT
		// name FROM t WHERE x = ?" during editing), not pop a full-screen
		// overlay over it.
		if !a.captureText() {
			a.showHelp = true
			return a, nil
		}
	case "q":
		if a.mode != modeREPL || !a.repl.Focused() {
			return a, a.quit()
		}
	case "esc":
		if a.mode == modeTables {
			a.tables.Back()
			return a, nil
		}
		if a.mode == modeNotebooks {
			a.notebooks.Back()
			return a, nil
		}
	case "tab":
		a.cycleMode()
		return a, nil
	case "1":
		if !a.captureText() {
			a.setMode(modeREPL)
			return a, nil
		}
	case "2":
		if !a.captureText() {
			a.setMode(modeTables)
			return a, nil
		}
	case "3":
		if !a.captureText() {
			a.setMode(modeNotebooks)
			return a, nil
		}
	case "s":
		if !a.captureText() {
			return a, a.save(false)
		}
	}

	var cmd tea.Cmd
	switch a.mode {
	case modeREPL:
		a.repl, cmd = a.repl.Update(msg)
	case modeTables:
		var loadCmd tea.Cmd
		a.tables, loadCmd = a.tables.Update(msg, a.engine)
		cmd = loadCmd
	case modeNotebooks:
		a.notebooks, cmd = a.notebooks.Update(msg, a.container)
	}
	return a, cmd
}

// captureText reports whether the active view currently owns free-text
// keyboard input (the REPL's textinput), so single-letter global shortcuts
// like "s"/"1"/"2"/"3" must be routed to it instead of intercepted.
func (a *App) captureText() bool {
	return a.mode == modeREPL
}

func (a *App) setMode(m mode) {
	a.mode = m
	if m == modeREPL {
		a.repl.Focus()
	} else {
		a.repl.Blur()
	}
}

func (a *App) cycleMode() {
	switch a.mode {
	case modeREPL:
		a.setMode(modeTables)
	case modeTables:
		a.setMode(modeNotebooks)
	case modeNotebooks:
		a.setMode(modeREPL)
	}
}

func (a *App) executeREPLQuery(query string) tea.Cmd {
	return func() tea.Msg {
		if a.engine == nil {
			return replResultMsg{output: "-- Error: no engine available"}
		}
		ctx := context.Background()
		var duckDB *sql.DB
		if a.container != nil {
			duckDB = a.container.DuckDBDB()
		}
		// agents.IsReadOnlyStatement is the SAME classifier run_query uses
		// (real DuckDB parsing + statement counting), not a keyword-prefix
		// heuristic — a reviewer proved live that a prefix heuristic
		// (the previous looksLikeWrite) missed real writes disguised as a
		// read-looking prefix: `WITH x AS (INSERT ...) SELECT ...`,
		// `SELECT 1; INSERT ...` (multi-statement), and
		// `EXPLAIN ANALYZE INSERT ...` all "looked" read-only by prefix
		// alone and silently lost the write on quit. IsReadOnlyStatement
		// fails closed (false) on a nil db or a classification error, so
		// an unrecognized/unparseable statement is always treated as a
		// write here too.
		readOnly := agents.IsReadOnlyStatement(ctx, duckDB, query, nil)
		rs, err := a.engine.Execute(ctx, capRowsIfSafe(query, readOnly), nil)
		if err != nil {
			return replResultMsg{output: fmt.Sprintf("-- Error: %v", err)}
		}
		return replResultMsg{output: formatResultSet(rs), wrote: !readOnly}
	}
}

// capRowsIfSafe pushes a row cap down into the SQL itself, for the one
// statement shape where wrapping it is unconditionally valid: a single,
// standalone SELECT or WITH…SELECT that IsReadOnlyStatement has already
// confirmed is a pure read, with no LIMIT of its own. readOnly must come
// from agents.IsReadOnlyStatement, never a prefix guess — wrapping
// `WITH x AS (INSERT ...) SELECT ...` as a subquery, for example, would
// silently turn a real write into a broken/no-op SELECT instead of running
// it. This keeps a `SELECT * FROM huge_table` from ever being fully
// materialized in the TUI's memory in the first place — formatResultSet's
// own row cap is only a display-time backstop for everything this can't
// safely touch: SHOW/DESCRIBE/PRAGMA/EXPLAIN/SUMMARIZE (not valid as a
// subquery), a statement that already has its own LIMIT, a multi-statement
// input, and any write statement.
func capRowsIfSafe(query string, readOnly bool) string {
	if !readOnly {
		return query
	}
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), ";")
	if trimmed == "" || strings.Contains(trimmed, ";") {
		return query // empty, or genuinely multi-statement — leave untouched.
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return query
	}
	if strings.Contains(upper, "LIMIT") {
		return query // respect the caller's own LIMIT rather than double-wrapping.
	}
	return fmt.Sprintf("SELECT * FROM (%s) AS _vibe_repl_cap LIMIT %d", trimmed, replRowCap+1)
}

// saveResultMsg is sent after a save operation completes. thenQuit carries
// the quit-with-unsaved-writes request through to the Update handler, which
// only sets a.quitting/returns tea.Quit once the save this message reports
// on has actually finished — never before, so a write made moments before
// quitting is never silently dropped.
type saveResultMsg struct {
	err      string
	thenQuit bool
}

// save persists the container. When thenQuit is true (the auto-save-on-quit
// path — see quit()), the returned saveResultMsg carries that through so the
// program only exits after the save attempt completes, whether it succeeded
// or not — a failed save must still let the user quit rather than trap them,
// but it must never race the quit against an in-flight write.
func (a *App) save(thenQuit bool) tea.Cmd {
	return func() tea.Msg {
		if a.container == nil {
			return saveResultMsg{err: "no container open", thenQuit: thenQuit}
		}
		if err := a.container.Save(); err != nil {
			return saveResultMsg{err: err.Error(), thenQuit: thenQuit}
		}
		return saveResultMsg{thenQuit: thenQuit}
	}
}

// quit auto-saves any unsaved write before exiting (issue #69 follow-up:
// REPL writes — INSERT/UPDATE/CREATE TABLE/etc — used to be silently
// discarded on quit, since only Ctrl+C/`q` and never a save ran on the way
// out). A clean quit with nothing unsaved exits immediately, same as before.
func (a *App) quit() tea.Cmd {
	if !a.unsaved {
		a.quitting = true
		return tea.Quit
	}
	return a.save(true)
}

// View implements tea.Model.
func (a *App) View() string {
	if a.quitting {
		return "Goodbye!\n"
	}
	if a.showHelp {
		return renderHelp(a.keys, a.helpView, a.width, a.height)
	}

	mainHeight := a.height - 2
	if mainHeight < 4 {
		mainHeight = 4
	}

	var body string
	switch a.mode {
	case modeREPL:
		body = a.repl.View(a.width, mainHeight)
	case modeTables:
		body = a.tables.View(a.width, mainHeight)
	case modeNotebooks:
		body = a.notebooks.View(a.width, mainHeight)
	}

	return lipgloss.JoinVertical(lipgloss.Left, a.renderModeBar(), body, a.renderStatusBar())
}

func (a *App) renderModeBar() string {
	labels := []mode{modeREPL, modeTables, modeNotebooks}
	var parts []string
	for i, m := range labels {
		// While the REPL owns the keyboard (captureText), "1"/"2"/"3" type
		// into the query instead of switching views — see captureText's doc
		// comment — so the bar must not advertise them as if they worked
		// here. Only "n:Label" numbering is dropped; tab still switches
		// views from any mode, so the label itself stays.
		var label string
		if a.captureText() {
			label = fmt.Sprintf(" %s ", m.String())
		} else {
			label = fmt.Sprintf(" %d:%s ", i+1, m.String())
		}
		if m == a.mode {
			parts = append(parts, selectedItemStyle.Render(label))
		} else {
			parts = append(parts, dimStyle.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return lipgloss.NewStyle().Width(a.width).Render(bar)
}

func (a *App) renderStatusBar() string {
	status := " VIBE STUDIO"
	if a.unsaved {
		status += " [unsaved]"
	}
	if a.status != "" {
		status += " — " + a.status
	}
	hint := " " + a.helpView.ShortHelpView(a.keys.ShortHelp()) + " "

	statusStyle := lipgloss.NewStyle().
		Width(a.width).
		Background(lipgloss.Color("235")).
		Foreground(lipgloss.Color("252"))

	left := statusStyle.Render(status)
	right := statusStyle.Render(hint)

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	padding := lipgloss.NewStyle().Width(gap).Background(lipgloss.Color("235")).Render("")

	return left + padding + right
}
