package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
)

// newHelpModel creates a configured help.Model.
func newHelpModel() help.Model {
	h := help.New()
	h.Styles.ShortKey = helpKeyStyle
	h.Styles.ShortDesc = helpDescStyle
	h.Styles.FullKey = helpKeyStyle
	h.Styles.FullDesc = helpDescStyle
	h.Styles.ShortSeparator = dimStyle
	h.Styles.FullSeparator = dimStyle
	return h
}

// renderHelp renders a full-screen help overlay showing all key bindings.
func renderHelp(keys KeyMap, helpModel help.Model, width, height int) string {
	helpModel.Width = width - 8
	content := helpModel.FullHelpView(keys.FullHelp())

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(accentColor).
		MarginBottom(1).
		Render("Vibe Studio - Key Bindings")

	footer := dimStyle.Render("Press any key to dismiss")

	body := lipgloss.JoinVertical(lipgloss.Left, title, "", content, "", footer)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 2).
		Width(width - 4).
		Height(height - 2)

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(body),
	)
}
