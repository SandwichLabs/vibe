package tui

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// tableDataMsg carries loaded table data back to the app.
type tableDataMsg struct {
	tableName string
	columns   []table.Column
	rows      []table.Row
	err       error
}

// TableBrowserModel displays the contents of a user table.
type TableBrowserModel struct {
	tableName string
	rowCount  int
	approx    bool
	table     table.Model
	loading   bool
	err       string
	hasData   bool
	width     int
	height    int
}

// NewTableBrowserModel creates a new table browser.
func NewTableBrowserModel() TableBrowserModel {
	t := table.New(
		table.WithFocused(false),
		table.WithStyles(defaultTableStyles()),
	)
	return TableBrowserModel{
		table:  t,
		width:  60,
		height: 20,
	}
}

// SetLoading sets the loading state for a table.
func (t *TableBrowserModel) SetLoading(name string, rowCount int, approx bool) {
	t.tableName = name
	t.rowCount = rowCount
	t.approx = approx
	t.loading = true
	t.err = ""
	t.hasData = false
}

// SetData populates the table with loaded data.
func (t *TableBrowserModel) SetData(cols []table.Column, rows []table.Row, err error) {
	t.loading = false
	if err != nil {
		t.err = err.Error()
		return
	}
	t.hasData = true
	t.table.SetRows(nil) // stale rows wider than the new columns would index out of range
	t.table.SetColumns(cols)
	t.table.SetRows(rows)
	t.table.SetStyles(defaultTableStyles())
	t.table.SetWidth(t.width)
	t.table.SetHeight(t.height - 3) // leave room for title and info
}

// SetTable is a convenience for the old API (sets metadata without data rows).
func (t *TableBrowserModel) SetTable(name string, rowCount int, columns []string) {
	t.SetLoading(name, rowCount, false)
}

// SetSize updates the table browser dimensions.
func (t *TableBrowserModel) SetSize(width, height int) {
	t.width = width
	t.height = height
	t.table.SetWidth(width)
	t.table.SetHeight(height - 3)
}

// SetFocused sets whether the table has keyboard focus.
func (t *TableBrowserModel) SetFocused(focused bool) {
	t.table.SetCursor(0)
	t.table.Focus()
	if !focused {
		t.table.Blur()
	}
}

// Update handles messages for the table browser.
func (t TableBrowserModel) Update(msg tea.Msg) (TableBrowserModel, tea.Cmd) {
	if !t.hasData {
		return t, nil
	}
	var cmd tea.Cmd
	t.table, cmd = t.table.Update(msg)
	return t, cmd
}

// View renders the table browser within the given dimensions.
func (t TableBrowserModel) View(width, height int) string {
	if t.tableName == "" {
		return dimStyle.Render("No table selected")
	}

	title := contentTitleStyle.Render(t.tableName)
	info := dimStyle.Render("first 1000 rows")
	if t.rowCount >= 0 {
		info = dimStyle.Render(formatRowCount(t.rowCount, t.approx))
	}
	header := title + "\n" + info + "\n"

	if t.loading {
		return header + dimStyle.Render("Loading...")
	}

	if t.err != "" {
		return header + errorStyle.Render(t.err)
	}

	if !t.hasData {
		return header + dimStyle.Render("No data loaded")
	}

	return header + t.table.View()
}
