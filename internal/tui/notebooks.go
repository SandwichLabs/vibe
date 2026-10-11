package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// NotebooksModel is the read-only notebook list + cell dump view (#69):
// title + cell count per notebook, and on Enter, a plain-text dump of each
// cell's source (sql/markdown) or a one-line summary (a2ui) — no rendering,
// this is a terminal.
type NotebooksModel struct {
	items    []notebooks.Summary
	cursor   int
	viewing  bool
	viewport viewport.Model
	title    string
	width    int
	height   int
	err      string
}

// NewNotebooksModel creates a new notebooks view.
func NewNotebooksModel() NotebooksModel {
	return NotebooksModel{viewport: viewport.New(80, 20)}
}

// LoadNotebooks refreshes the notebook list from the workspace.
func (m *NotebooksModel) LoadNotebooks(store notebooks.Store) {
	if store == nil {
		return
	}
	list, err := store.List(context.Background())
	if err != nil {
		return
	}
	m.items = list
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
}

// SetSize updates the view's dimensions.
func (m *NotebooksModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = width
	viewportHeight := height - 2
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	m.viewport.Height = viewportHeight
}

// Back returns from the cell dump to the notebook list.
func (m *NotebooksModel) Back() {
	m.viewing = false
}

// Update handles input for the notebooks view.
func (m NotebooksModel) Update(msg tea.Msg, ws WorkspaceAccess) (NotebooksModel, tea.Cmd) {
	if m.viewing {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "esc" {
			m.viewing = false
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
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
		if m.cursor < len(m.items) && ws != nil {
			sel := m.items[m.cursor]
			m.title = sanitizeForTerminal(sel.Title, false)
			m.err = ""
			var nb notebooks.Notebook
			var err error
			if store := ws.Notebooks(); store == nil {
				err = errors.New("no notebook store")
			} else {
				nb, _, err = store.Get(context.Background(), sel.ID)
			}
			if err != nil {
				m.err = err.Error()
			} else {
				m.viewport.SetContent(dumpNotebook(nb))
				m.viewport.GotoTop()
			}
			m.viewing = true
		}
	}
	return m, nil
}

// dumpNotebook renders a notebook's cells as plain text — sql and markdown
// cells print their source verbatim; a2ui cells print a one-line summary
// (component count) since there is no A2UI renderer in the TUI. Every piece
// of notebook-authored content (title, label, source) is untrusted — a
// hand-edited file, an import, or an agent could have written a control or
// ANSI-escape character into it — so it all goes through
// sanitizeForTerminal before reaching the terminal.
func dumpNotebook(nb notebooks.Notebook) string {
	out := fmt.Sprintf("# %s (%d cells)\n\n", sanitizeForTerminal(nb.Title, false), len(nb.Cells))
	for i, c := range nb.Cells {
		out += fmt.Sprintf("--- cell %d: %s", i+1, c.Type)
		if c.Label != "" {
			out += " (" + sanitizeForTerminal(c.Label, false) + ")"
		}
		out += " ---\n"
		switch c.Type {
		case notebooks.CellSQL:
			out += sanitizeForTerminal(c.Source, true) + "\n"
			out += dimStyle.Render(fmt.Sprintf("[view: %s]", c.View)) + "\n"
		case notebooks.CellMarkdown:
			out += sanitizeForTerminal(c.Source, true) + "\n"
		case notebooks.CellA2UI:
			n := 0
			title := ""
			if c.Page != nil {
				n = len(c.Page.Components)
				title = sanitizeForTerminal(c.Page.Title, false)
			}
			out += dimStyle.Render(fmt.Sprintf("[a2ui dashboard %q, %d components — not rendered in the TUI]", title, n)) + "\n"
		}
		out += "\n"
	}
	return out
}

// View renders the notebook list, or the cell dump when a notebook is open.
func (m NotebooksModel) View(width, height int) string {
	if m.viewing {
		title := contentTitleStyle.Render(m.title)
		if m.err != "" {
			return title + "\n" + errorStyle.Render(m.err)
		}
		return title + "\n" + m.viewport.View()
	}

	if len(m.items) == 0 {
		return dimStyle.Render("No notebooks in this workspace.")
	}

	title := contentTitleStyle.Render("Notebooks")
	lines := []string{title}
	for i, n := range m.items {
		line := fmt.Sprintf("%s  (%d cells, updated %s)", sanitizeForTerminal(n.Title, false), n.CellCount, n.UpdatedAt)
		if i == m.cursor {
			lines = append(lines, selectedItemStyle.Render("> "+line))
		} else {
			lines = append(lines, listItemStyle.Render(line))
		}
	}
	lines = append(lines, "", dimStyle.Render("enter: view cells (source only, no rendering)"))

	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
