package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ContentModel manages the main content panel display.
type ContentModel struct {
	pageTitle  string
	pageType   string
	rawContent string
	viewport   viewport.Model
	width      int
	height     int
}

// NewContentModel creates a new content model.
func NewContentModel() ContentModel {
	vp := viewport.New(60, 20)
	m := ContentModel{
		pageTitle: "Welcome",
		rawContent: `Welcome to **Vibe Studio**.

## Getting Started

- Press **n** to create a new page or query
- Use **j/k** to navigate the sidebar
- Press **Enter** to open an item
- Press **` + "`" + `** to toggle the SQL REPL
- Press **?** for full keyboard shortcuts

## Quick Actions

| Key | Action |
|-----|--------|
| n | New page or query |
| s | Save |
| Tab | Cycle panels |
| / | Filter sidebar |
`,
		pageType: "page",
		viewport: vp,
		width:    60,
		height:   20,
	}
	m.renderContent()
	return m
}

// SetPage updates the content panel to display the given page.
func (c *ContentModel) SetPage(title, content, pageType string, width int) {
	c.pageTitle = title
	c.rawContent = content
	c.pageType = pageType
	if width > 0 {
		c.width = width
	}
	c.renderContent()
	c.viewport.GotoTop()
}

// SetSize updates the viewport dimensions.
func (c *ContentModel) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.viewport.Width = width
	c.viewport.Height = height
	c.renderContent()
}

// renderContent renders the page title, type, and markdown content into the viewport.
func (c *ContentModel) renderContent() {
	title := contentTitleStyle.Render(c.pageTitle)
	typeLabel := dimStyle.Render(fmt.Sprintf("[%s]", c.pageType))

	header := lipgloss.JoinVertical(lipgloss.Left, title, typeLabel, "")

	body := renderMarkdown(c.rawContent, c.width-2)

	c.viewport.SetContent(header + "\n" + body)
}

// renderMarkdown renders markdown text using glamour.
func renderMarkdown(content string, width int) string {
	if width < 20 {
		width = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return out
}

// Update handles messages for the content panel.
func (c ContentModel) Update(msg tea.Msg) (ContentModel, tea.Cmd) {
	var cmd tea.Cmd
	c.viewport, cmd = c.viewport.Update(msg)
	return c, cmd
}

// View renders the content panel.
func (c ContentModel) View(width, height int) string {
	return c.viewport.View()
}
