package tui

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sandwich-labs/vibe-app/internal/engine"
)

type focus int

const (
	focusSidebar focus = iota
	focusContent
	focusREPL
)

// contentView tracks which view is active in the content panel.
type contentView int

const (
	viewPage contentView = iota
	viewTable
	viewQuery
)

// ContainerAccess interface to avoid importing container directly.
type ContainerAccess interface {
	SQLiteDB() *sql.DB
	Save() error
	Close() error
}

// EngineAccess interface for query execution.
type EngineAccess interface {
	Execute(ctx context.Context, sql string, eng string, params map[string]any) (*engine.ResultSet, error)
}

// App is the top-level BubbleTea model for Vibe Studio.
type App struct {
	container    ContainerAccess
	engine       EngineAccess
	focus        focus
	activeView   contentView
	sidebar      SidebarModel
	content      ContentModel
	tableBrowser TableBrowserModel
	queryEditor  QueryEditorModel
	repl         REPLModel
	pageWizard   PageWizardModel
	keys         KeyMap
	helpView     help.Model
	width        int
	height       int
	unsaved      bool
	showHelp     bool
	quitting     bool
	logger       *slog.Logger
}

// NewApp creates a new TUI application model.
func NewApp(container ContainerAccess, engine EngineAccess, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	return &App{
		container:    container,
		engine:       engine,
		focus:        focusSidebar,
		activeView:   viewPage,
		sidebar:      NewSidebarModel(),
		content:      NewContentModel(),
		tableBrowser: NewTableBrowserModel(),
		queryEditor:  NewQueryEditorModel(),
		repl:         NewREPLModel(),
		pageWizard:   NewPageWizardModel(),
		keys:         DefaultKeyMap(),
		helpView:     newHelpModel(),
		width:        80,
		height:       24,
		logger:       logger,
	}
}

// LoadData loads pages, tables, and queries from the container into the sidebar.
func (a *App) LoadData() {
	if a.container == nil {
		return
	}

	db := a.container.SQLiteDB()

	// Load pages.
	rows, err := db.Query("SELECT id, title, page_type FROM _vibe_pages ORDER BY sort_order, title;")
	if err == nil {
		var pages []PageItem
		for rows.Next() {
			var p PageItem
			if err := rows.Scan(&p.ID, &p.Title, &p.Type); err == nil {
				pages = append(pages, p)
			}
		}
		rows.Close()
		a.sidebar.SetPages(pages)
	}

	// Load user tables with row counts.
	rows, err = db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE '_vibe_%' ORDER BY name;")
	if err == nil {
		var tables []TableItem
		for rows.Next() {
			var t TableItem
			if err := rows.Scan(&t.Name); err == nil {
				// Get row count.
				db.QueryRow("SELECT COUNT(*) FROM \"" + t.Name + "\";").Scan(&t.RowCount) //nolint:gosec,errcheck
				tables = append(tables, t)
			}
		}
		rows.Close()
		a.sidebar.SetTables(tables)
	}

	// Load queries.
	rows, err = db.Query("SELECT id, title FROM _vibe_queries ORDER BY title;")
	if err == nil {
		var queries []QueryItem
		for rows.Next() {
			var q QueryItem
			if err := rows.Scan(&q.ID, &q.Title); err == nil {
				queries = append(queries, q)
			}
		}
		rows.Close()
		a.sidebar.SetQueries(queries)
	}
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
		return a, nil
	case tableDataMsg:
		a.tableBrowser.SetData(msg.columns, msg.rows, msg.err)
		return a, nil
	case pageCreatedMsg:
		if msg.err != nil {
			a.logger.Error("failed to create page", "error", msg.err)
			return a, nil
		}
		a.LoadData()
		a.unsaved = true
		return a, openEditorCmd(msg.pageID, msg.content)
	case editorFinishedMsg:
		if msg.err != nil {
			a.logger.Error("editor error", "error", msg.err)
			return a, nil
		}
		// Save edited content back to database.
		if a.container != nil {
			db := a.container.SQLiteDB()
			now := time.Now().UTC().Format(time.RFC3339)
			_, err := db.Exec("UPDATE _vibe_pages SET content = ?, updated_at = ? WHERE id = ?;",
				msg.content, now, msg.pageID)
			if err != nil {
				a.logger.Error("failed to save edited page", "error", err)
			}
		}
		// Display the page in the content panel.
		title := a.lookupPageTitle(msg.pageID)
		_, contentWidth, _, _ := a.computeLayout()
		a.content.SetPage(title, msg.content, "page", contentWidth)
		a.activeView = viewPage
		a.setFocus(focusContent)
		return a, nil
	case saveResultMsg:
		if msg.err == "" {
			a.unsaved = false
		}
		return a, nil
	}
	return a, nil
}

// pageCreatedMsg is sent after a page is inserted into the database.
type pageCreatedMsg struct {
	pageID  string
	content string
	err     error
}

// lookupPageTitle queries the page title from the database by ID.
func (a *App) lookupPageTitle(pageID string) string {
	if a.container == nil {
		return "Untitled"
	}
	var title string
	err := a.container.SQLiteDB().QueryRow(
		"SELECT title FROM _vibe_pages WHERE id = ?;", pageID,
	).Scan(&title)
	if err != nil || title == "" {
		return "Untitled"
	}
	return title
}

// propagateDimensions sends current dimensions to all sub-components.
func (a *App) propagateDimensions() {
	sidebarWidth, contentWidth, _, mainHeight := a.computeLayout()
	_ = sidebarWidth // sidebar sets its own size in View()
	a.content.SetSize(contentWidth, mainHeight)
	a.tableBrowser.SetSize(contentWidth, mainHeight)
	a.queryEditor.SetSize(contentWidth, mainHeight)
	a.repl.SetSize(a.width, a.computeREPLHeight())
}

func (a *App) computeREPLHeight() int {
	if a.focus == focusREPL {
		return replHeightExpanded
	}
	return replHeightNormal
}

func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys that work regardless of focus.
	switch msg.String() {
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}

	// If page wizard is active, route all input there.
	if a.pageWizard.IsActive() {
		a.pageWizard = a.pageWizard.Update(msg)
		// Check if wizard just completed.
		if !a.pageWizard.IsActive() && a.pageWizard.step == wizardStepDone {
			return a, a.createPageFromWizard()
		}
		return a, nil
	}

	switch msg.String() {
	case "?":
		if a.focus != focusREPL {
			a.showHelp = !a.showHelp
			return a, nil
		}
	}

	// If help overlay is showing, any key dismisses it.
	if a.showHelp {
		a.showHelp = false
		return a, nil
	}

	switch msg.String() {
	case "q":
		if a.focus != focusREPL {
			a.quitting = true
			return a, tea.Quit
		}
	case "tab":
		return a, a.cycleFocus()
	case "`":
		return a, a.toggleREPL()
	case "s":
		if a.focus != focusREPL {
			return a, a.save()
		}
	case "n":
		if a.focus != focusREPL {
			a.pageWizard.Start()
			return a, nil
		}
	case "enter":
		if a.focus == focusSidebar {
			return a, a.handleSidebarSelect()
		}
	}

	// Delegate to focused panel.
	var cmd tea.Cmd
	switch a.focus {
	case focusSidebar:
		a.sidebar, cmd = a.sidebar.Update(msg)
	case focusContent:
		switch a.activeView {
		case viewPage:
			a.content, cmd = a.content.Update(msg)
		case viewTable:
			a.tableBrowser, cmd = a.tableBrowser.Update(msg)
		case viewQuery:
			a.queryEditor, cmd = a.queryEditor.Update(msg)
		}
	case focusREPL:
		a.repl, cmd = a.repl.Update(msg)
	}

	return a, cmd
}

// setFocus changes focus and manages input blur/focus.
func (a *App) setFocus(f focus) tea.Cmd {
	a.focus = f
	if f == focusREPL {
		a.propagateDimensions()
		return a.repl.Focus()
	}
	a.repl.Blur()
	a.propagateDimensions()
	// Set table focus state.
	a.tableBrowser.SetFocused(f == focusContent && a.activeView == viewTable)
	return nil
}

// handleSidebarSelect loads the selected item into the content panel.
func (a *App) handleSidebarSelect() tea.Cmd {
	si := a.sidebar.SelectedItem()
	if si == nil {
		return nil
	}

	switch {
	case si.page != nil:
		if a.container == nil {
			return nil
		}
		db := a.container.SQLiteDB()
		var content string
		err := db.QueryRow("SELECT content FROM _vibe_pages WHERE id = ?;", si.page.ID).Scan(&content)
		if err != nil {
			content = fmt.Sprintf("(failed to load page: %v)", err)
		}
		_, contentWidth, _, _ := a.computeLayout()
		a.content.SetPage(si.page.Title, content, si.page.Type, contentWidth)
		a.activeView = viewPage
		a.setFocus(focusContent)

	case si.table != nil:
		a.tableBrowser.SetLoading(si.table.Name, si.table.RowCount)
		a.activeView = viewTable
		a.setFocus(focusContent)
		return a.loadTableData(si.table.Name)

	case si.query != nil:
		if a.container == nil {
			return nil
		}
		db := a.container.SQLiteDB()
		var sqlText string
		err := db.QueryRow("SELECT sql FROM _vibe_queries WHERE id = ?;", si.query.ID).Scan(&sqlText)
		if err != nil {
			sqlText = fmt.Sprintf("-- failed to load query: %v", err)
		}
		a.queryEditor.SetQuery(si.query.ID, si.query.Title, sqlText)
		a.activeView = viewQuery
		a.setFocus(focusContent)
	}

	return nil
}

// createPageFromWizard inserts a new page into the database using the wizard result.
func (a *App) createPageFromWizard() tea.Cmd {
	pageType, title, tmpl := a.pageWizard.Result()
	return func() tea.Msg {
		if a.container == nil {
			return pageCreatedMsg{err: fmt.Errorf("no container open")}
		}
		db := a.container.SQLiteDB()
		id := generateID()
		now := time.Now().UTC().Format(time.RFC3339)
		content := templateContent(pageType, tmpl)
		_, err := db.Exec(
			`INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 0, ?, ?);`,
			id, title, content, pageType, now, now,
		)
		if err != nil {
			return pageCreatedMsg{err: fmt.Errorf("failed to insert page: %w", err)}
		}
		return pageCreatedMsg{pageID: id, content: content}
	}
}

// templateContent returns starter content for a page based on type and template.
func templateContent(pageType, tmpl string) string {
	switch pageType {
	case "dashboard":
		switch tmpl {
		case "metrics":
			return "# Dashboard\n\n```sql [results=metrics]\nSELECT 'Total' AS label, COUNT(*) AS value FROM sqlite_master;\n```\n\n{metric: metrics}\n"
		case "charts":
			return "# Dashboard\n\n```sql [results=data]\nSELECT name, type FROM sqlite_master;\n```\n\n{chart: data type=bar}\n"
		}
	case "form":
		switch tmpl {
		case "crud":
			return "# Form\n\n{form: table_name}\n"
		}
	case "report":
		switch tmpl {
		case "summary":
			return "# Report\n\n```sql [results=summary]\nSELECT type, COUNT(*) AS count FROM sqlite_master GROUP BY type;\n```\n\n{table: summary}\n"
		}
	}
	// Default: blank or markdown.
	return "# " + tmpl + "\n\nStart writing here.\n"
}

// generateID creates a simple unique ID for new records.
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// loadTableData asynchronously loads row data for a table.
func (a *App) loadTableData(tableName string) tea.Cmd {
	return func() tea.Msg {
		if a.engine == nil {
			return tableDataMsg{tableName: tableName, err: fmt.Errorf("no engine available")}
		}

		ctx := context.Background()
		query := fmt.Sprintf("SELECT * FROM %q LIMIT 1000", tableName)
		rs, err := a.engine.Execute(ctx, query, "sqlite", nil)
		if err != nil {
			return tableDataMsg{tableName: tableName, err: err}
		}

		if rs == nil || len(rs.Columns) == 0 {
			return tableDataMsg{tableName: tableName}
		}

		// Convert to bubbles/table types.
		cols := make([]table.Column, len(rs.Columns))
		for i, c := range rs.Columns {
			width := len(c.Name)
			if width < 8 {
				width = 8
			}
			// Check max data width.
			for _, row := range rs.Rows {
				if i < len(row) {
					cellWidth := len(fmt.Sprintf("%v", row[i]))
					if cellWidth > width {
						width = cellWidth
					}
				}
			}
			if width > 40 {
				width = 40
			}
			cols[i] = table.Column{Title: c.Name, Width: width}
		}

		rows := make([]table.Row, len(rs.Rows))
		for i, row := range rs.Rows {
			r := make(table.Row, len(row))
			for j, v := range row {
				r[j] = fmt.Sprintf("%v", v)
			}
			rows[i] = r
		}

		return tableDataMsg{tableName: tableName, columns: cols, rows: rows}
	}
}

// loadTableColumns gets column names for a table via PRAGMA table_info.
func (a *App) loadTableColumns(db *sql.DB, tableName string) []string {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q);", tableName))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err == nil {
			columns = append(columns, fmt.Sprintf("%s (%s)", name, colType))
		}
	}
	return columns
}

// executeREPLQuery runs a SQL query through the engine and returns the result.
func (a *App) executeREPLQuery(query string) tea.Cmd {
	return func() tea.Msg {
		if a.engine == nil {
			return replResultMsg{output: "-- Error: no engine available"}
		}

		ctx := context.Background()
		rs, err := a.engine.Execute(ctx, query, "sqlite", nil)
		if err != nil {
			return replResultMsg{output: fmt.Sprintf("-- Error: %v", err)}
		}

		return replResultMsg{output: formatResultSet(rs)}
	}
}

// formatResultSet formats a ResultSet as a simple text table.
func formatResultSet(rs *engine.ResultSet) string {
	if rs == nil || len(rs.Columns) == 0 {
		return "-- OK (no results)"
	}

	var b strings.Builder

	// Header.
	colNames := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		colNames[i] = c.Name
	}
	b.WriteString(strings.Join(colNames, " | "))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", len(b.String())-1))
	b.WriteString("\n")

	// Rows.
	for _, row := range rs.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = fmt.Sprintf("%v", v)
		}
		b.WriteString(strings.Join(vals, " | "))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("(%d rows)", len(rs.Rows)))
	return b.String()
}

func (a *App) cycleFocus() tea.Cmd {
	switch a.focus {
	case focusSidebar:
		return a.setFocus(focusContent)
	case focusContent:
		return a.setFocus(focusREPL)
	case focusREPL:
		return a.setFocus(focusSidebar)
	}
	return nil
}

func (a *App) toggleREPL() tea.Cmd {
	if a.focus == focusREPL {
		return a.setFocus(focusContent)
	}
	return a.setFocus(focusREPL)
}

func (a *App) save() tea.Cmd {
	return func() tea.Msg {
		if a.container == nil {
			return saveResultMsg{err: "no container open"}
		}
		if err := a.container.Save(); err != nil {
			return saveResultMsg{err: err.Error()}
		}
		return saveResultMsg{}
	}
}

// saveResultMsg is sent after a save operation completes.
type saveResultMsg struct {
	err string
}

// View implements tea.Model.
func (a *App) View() string {
	if a.quitting {
		return "Goodbye!\n"
	}
	if a.showHelp {
		return renderHelp(a.keys, a.helpView, a.width, a.height)
	}
	if a.pageWizard.IsActive() {
		return a.renderWizardOverlay()
	}
	return a.renderLayout()
}

// renderWizardOverlay renders the page wizard as a centered overlay.
func (a *App) renderWizardOverlay() string {
	content := a.pageWizard.View(a.width-8, a.height-4)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 2).
		Width(a.width - 4).
		Height(a.height - 2)

	return lipgloss.Place(a.width, a.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(content),
	)
}
