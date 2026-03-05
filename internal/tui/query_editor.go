package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// QueryEditorModel manages editing and running saved queries.
type QueryEditorModel struct {
	queryID    string
	queryTitle string
	querySQL   string
	output     string
	viewport   viewport.Model
	width      int
	height     int
}

// NewQueryEditorModel creates a new query editor.
func NewQueryEditorModel() QueryEditorModel {
	vp := viewport.New(60, 20)
	return QueryEditorModel{
		viewport: vp,
		width:    60,
		height:   20,
	}
}

// SetQuery configures the editor for a specific query.
func (q *QueryEditorModel) SetQuery(id, title, sql string) {
	q.queryID = id
	q.queryTitle = title
	q.querySQL = sql
	q.output = ""
	q.renderContent()
	q.viewport.GotoTop()
}

// SetSize updates the viewport dimensions.
func (q *QueryEditorModel) SetSize(width, height int) {
	q.width = width
	q.height = height
	q.viewport.Width = width
	q.viewport.Height = height
	q.renderContent()
}

// renderContent builds the viewport content from title, SQL, and output.
func (q *QueryEditorModel) renderContent() {
	if q.queryID == "" {
		q.viewport.SetContent(dimStyle.Render("No query selected"))
		return
	}

	title := contentTitleStyle.Render(q.queryTitle)
	sqlHeader := sectionHeaderStyle.Render("SQL:")
	sqlBody := q.querySQL

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", sqlHeader, sqlBody, "")

	if q.output != "" {
		outHeader := sectionHeaderStyle.Render("Output:")
		content = lipgloss.JoinVertical(lipgloss.Left, content, outHeader, q.output)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, content, dimStyle.Render("Press Enter to execute"))
	}

	q.viewport.SetContent(content)
}

// Update handles messages for the query editor.
func (q QueryEditorModel) Update(msg tea.Msg) (QueryEditorModel, tea.Cmd) {
	var cmd tea.Cmd
	q.viewport, cmd = q.viewport.Update(msg)
	return q, cmd
}

// View renders the query editor within the given dimensions.
func (q QueryEditorModel) View(width, height int) string {
	if q.queryID == "" {
		return dimStyle.Render("No query selected")
	}
	return q.viewport.View()
}
