package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SandwichLabs/vibe/internal/crud"
	"github.com/SandwichLabs/vibe/internal/session"
)

// TableInfo is one entry in the table list.
type TableInfo struct {
	Name     string
	RowCount int
	// Approx marks RowCount as DuckDB's statistics estimate, shown as "~1,234 rows".
	Approx bool
	// SQL is the quoted catalog.schema.table reference taken from the
	// session's listing, so a table whose stored name contains dots is not
	// re-parsed as a path.
	SQL string
	// Ref is the same reference as parts, handed to crud.Page to read rows.
	Ref session.TableRef
}

// TablesModel is the read-only table browser (#69): a list of tables, and
// on Enter, a paged view of that table's rows via TableBrowserModel. There
// is no editing here — Studio's Tables view is where data is changed.
type TablesModel struct {
	items    []TableInfo
	cursor   int
	browsing bool
	browser  TableBrowserModel
	sess     session.Session
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
// come from DuckDB's table statistics (estimated_size: free, approximate, marked
// "~" in the view), one query per catalog instead of a COUNT(*) per table. A
// local DuckDB table with no estimate (empty, or not yet analysed) falls back to
// an exact count, as does one with an estimate under 10,000; a remote table with no estimate shows none, since counting it
// can be slow or expensive.
func (m *TablesModel) LoadTables(sess session.Session) {
	if sess == nil {
		return
	}
	m.sess = sess
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
		est := session.EstimatedRows(ctx, sess.DB(), cat.Name)
		for _, ti := range tables {
			t := TableInfo{Name: displayTableName(ti, defCat, defSchema), SQL: ti.Ref().SQL(), Ref: ti.Ref(), RowCount: -1}
			n, ok := est[session.TableKey{Schema: ti.Schema, Name: ti.Name}]
			small := n < exactCountBelow
			if ok && ti.Type == "BASE TABLE" && !(small && cat.Type == "duckdb") {
				t.RowCount, t.Approx = int(n), true
			} else if cat.Type == "duckdb" {
				if n, err := crud.Count(ctx, sess.DB(), t.Ref); err == nil {
					t.RowCount = n
				}
			}
			items = append(items, t)
		}
	}
	m.items = items
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
}

// exactCountBelow is the estimate under which a local DuckDB table is counted
// exactly (cheap, and the statistics go stale after small edits).
const exactCountBelow = 10000

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
func (m TablesModel) Update(msg tea.Msg) (TablesModel, tea.Cmd) {
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
			m.browser.SetLoading(sel.Name, sel.RowCount, sel.Approx)
			m.browser.SetFocused(true)
			return m, m.loadTableData(sel)
		}
	}
	return m, nil
}

// loadTableData asynchronously loads up to 1000 rows of a table (crud.Page's
// MaxPageSize), the same reader Studio's table browser uses.
func (m TablesModel) loadTableData(item TableInfo) tea.Cmd {
	sess := m.sess
	tableName := item.Name
	ref := item.Ref
	if ref.Name == "" {
		ref = session.TableRef{Name: tableName}
	}
	return func() tea.Msg {
		if sess == nil {
			return tableDataMsg{tableName: tableName, err: fmt.Errorf("no session available")}
		}
		pg, err := crud.Page(context.Background(), sess.DB(), ref, crud.PageRequest{Page: 1, PageSize: crud.MaxPageSize, SkipCount: true})
		if err != nil {
			return tableDataMsg{tableName: tableName, err: err}
		}
		if len(pg.Columns) == 0 {
			return tableDataMsg{tableName: tableName}
		}
		cols, rows := tuiTableData(pg)
		return tableDataMsg{tableName: tableName, columns: cols, rows: rows}
	}
}

// tuiTableData turns a page of rows into table columns (sized to the content,
// 8 to 40 wide) and rows of display text.
func tuiTableData(pg *crud.PageResult) ([]table.Column, []table.Row) {
	cols := make([]table.Column, len(pg.Columns))
	for i, c := range pg.Columns {
		width := len(c)
		if width < 8 {
			width = 8
		}
		for _, row := range pg.Rows {
			if i < len(row) {
				if cw := len(formatCellValue(row[i])); cw > width {
					width = cw
				}
			}
		}
		if width > 40 {
			width = 40
		}
		cols[i] = table.Column{Title: c, Width: width}
	}

	rows := make([]table.Row, len(pg.Rows))
	for i, row := range pg.Rows {
		r := make(table.Row, len(row))
		for j, v := range row {
			r[j] = formatCellValue(v)
		}
		rows[i] = r
	}
	return cols, rows
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
			line += "  " + formatRowCount(t.RowCount, t.Approx)
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

// formatRowCount renders "1,234 rows", or "~1,234 rows" for an estimate.
func formatRowCount(n int, approx bool) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	prefix := ""
	if approx {
		prefix = "~"
	}
	return prefix + b.String() + " rows"
}
