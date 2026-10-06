package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// TableInfo is one entry in the table list.
type TableInfo struct {
	Name     string
	RowCount int
	// SQL is the quoted catalog.schema.table reference taken from the
	// session's listing, so a table whose stored name contains dots is not
	// re-parsed as a path.
	SQL string
}

// TablesModel is the read-only table browser (#69): a list of tables, and
// on Enter, a paged view of that table's rows via TableBrowserModel. There
// is no editing here — Studio's Tables view is where data is changed.
type TablesModel struct {
	items    []TableInfo
	cursor   int
	browsing bool
	browser  TableBrowserModel
	width    int
	height   int
}

// NewTablesModel creates a new tables view.
func NewTablesModel() TablesModel {
	return TablesModel{browser: NewTableBrowserModel()}
}

// LoadTables refreshes the table list from every attached connection (catalog).
// Tables of the default connection's default schema keep their bare name; any
// other is listed qualified ("sales.orders", "sales.archive.orders"). Row counts
// are read for local DuckDB catalogs only: counting a remote table can be slow
// or expensive, so those show no count.
func (m *TablesModel) LoadTables(sess session.Session) {
	if sess == nil {
		return
	}
	ctx := context.Background()
	catalogs, err := sess.ListCatalogs(ctx)
	if err != nil {
		return
	}
	defCat := sess.DefaultCatalog()
	defSchema, _ := sess.DefaultSchema(ctx, "")

	var items []TableInfo
	for _, cat := range catalogs {
		tables, err := session.AllTables(ctx, sess, cat.Name)
		if err != nil {
			continue
		}
		for _, ti := range tables {
			t := TableInfo{Name: displayTableName(ti, defCat, defSchema), SQL: ti.Ref().SQL(), RowCount: -1}
			if cat.Type == "duckdb" {
				sess.DB().QueryRow("SELECT COUNT(*) FROM " + t.SQL + ";").Scan(&t.RowCount) //nolint:errcheck
			}
			items = append(items, t)
		}
	}
	m.items = items
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
}

// displayTableName is the name a table is listed under.
func displayTableName(ti session.TableInfo, defCat, defSchema string) string {
	switch {
	case ti.Catalog == defCat && ti.Schema == defSchema:
		return ti.Name
	case ti.Schema == "main" || ti.Schema == "":
		return ti.Catalog + "." + ti.Name
	default:
		return ti.Catalog + "." + ti.Schema + "." + ti.Name
	}
}

// SetSize updates the view's dimensions.
func (m *TablesModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.browser.SetSize(width, height)
}

// Back returns from the row browser to the table list.
func (m *TablesModel) Back() {
	m.browsing = false
}

// SetData delivers loaded row data to the embedded browser.
func (m *TablesModel) SetData(msg tableDataMsg) {
	m.browser.SetData(msg.columns, msg.rows, msg.err)
}

// Update handles input for the tables view.
func (m TablesModel) Update(msg tea.Msg, eng EngineAccess) (TablesModel, tea.Cmd) {
	if m.browsing {
		keyMsg, ok := msg.(tea.KeyMsg)
		if ok && keyMsg.String() == "esc" {
			m.browsing = false
			return m, nil
		}
		var cmd tea.Cmd
		m.browser, cmd = m.browser.Update(msg)
		return m, cmd
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor < len(m.items) {
			sel := m.items[m.cursor]
			m.browsing = true
			m.browser.SetLoading(sel.Name, sel.RowCount)
			m.browser.SetFocused(true)
			return m, m.loadTableData(eng, sel.Name, sel.SQL)
		}
	}
	return m, nil
}

// loadTableData asynchronously loads up to 1000 rows of a table.
func (m TablesModel) loadTableData(eng EngineAccess, tableName, tableSQL string) tea.Cmd {
	return func() tea.Msg {
		if eng == nil {
			return tableDataMsg{tableName: tableName, err: fmt.Errorf("no engine available")}
		}
		ctx := context.Background()
		query := fmt.Sprintf("SELECT * FROM %s LIMIT 1000", sqlOrQuoted(tableSQL, tableName))
		rs, err := eng.Execute(ctx, query, nil)
		if err != nil {
			return tableDataMsg{tableName: tableName, err: err}
		}
		if rs == nil || len(rs.Columns) == 0 {
			return tableDataMsg{tableName: tableName}
		}

		cols := make([]table.Column, len(rs.Columns))
		for i, c := range rs.Columns {
			width := len(c.Name)
			if width < 8 {
				width = 8
			}
			for _, row := range rs.Rows {
				if i < len(row) {
					if cw := len(formatCellValue(row[i])); cw > width {
						width = cw
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
				r[j] = formatCellValue(v)
			}
			rows[i] = r
		}
		return tableDataMsg{tableName: tableName, columns: cols, rows: rows}
	}
}

// View renders the table list, or the row browser when a table is open.
func (m TablesModel) View(width, height int) string {
	if m.browsing {
		return m.browser.View(width, height)
	}

	if len(m.items) == 0 {
		return dimStyle.Render("No tables in this workspace.")
	}

	title := contentTitleStyle.Render("Tables")
	lines := []string{title}
	for i, t := range m.items {
		line := sanitizeForTerminal(t.Name, false)
		if t.RowCount >= 0 {
			line += fmt.Sprintf("  %d rows", t.RowCount)
		}
		if i == m.cursor {
			lines = append(lines, selectedItemStyle.Render("> "+line))
		} else {
			lines = append(lines, listItemStyle.Render(line))
		}
	}
	lines = append(lines, "", dimStyle.Render("enter: browse rows"))

	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// sqlOrQuoted prefers the listing's reference and only falls back to quoting
// the bare name when none was recorded.
func sqlOrQuoted(tableSQL, name string) string {
	if tableSQL != "" {
		return tableSQL
	}
	return engine.QuoteIdent(name)
}
