package tui

import (
	"github.com/charmbracelet/lipgloss"
)

const (
	sidebarWidthPercent = 20
	replHeightNormal    = 6
	replHeightExpanded  = 14
	minSidebarWidth     = 20
	maxSidebarWidth     = 40
	collapseThreshold   = 60
)

// computeLayout calculates panel dimensions based on terminal size and focus.
func (a *App) computeLayout() (sidebarWidth, contentWidth, replHeight, mainHeight int) {
	replHeight = replHeightNormal
	if a.focus == focusREPL {
		replHeight = replHeightExpanded
	}

	// Sidebar width (or 0 if terminal too narrow).
	if a.width < collapseThreshold {
		sidebarWidth = 0
	} else {
		sidebarWidth = a.width * sidebarWidthPercent / 100
		if sidebarWidth < minSidebarWidth {
			sidebarWidth = minSidebarWidth
		}
		if sidebarWidth > maxSidebarWidth {
			sidebarWidth = maxSidebarWidth
		}
	}

	contentWidth = a.width - sidebarWidth
	if sidebarWidth > 0 {
		contentWidth-- // -1 for border
	}
	if contentWidth < 10 {
		contentWidth = 10
	}

	mainHeight = a.height - replHeight - 2 // -2 for status bar and borders
	if mainHeight < 4 {
		mainHeight = 4
	}

	return
}

// renderLayout renders the three-panel layout: sidebar | content, with REPL at the bottom.
func (a *App) renderLayout() string {
	sidebarWidth, contentWidth, rHeight, mainHeight := a.computeLayout()

	var mainPanel string

	if sidebarWidth > 0 {
		// Sidebar panel.
		sidebarStyle := lipgloss.NewStyle().
			Width(sidebarWidth).
			Height(mainHeight).
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true)

		if a.focus == focusSidebar {
			sidebarStyle = sidebarStyle.BorderForeground(accentColor)
		}

		sidebarView := sidebarStyle.Render(a.sidebar.View(sidebarWidth, mainHeight))

		// Content panel.
		contentStyle := lipgloss.NewStyle().
			Width(contentWidth).
			Height(mainHeight)

		if a.focus == focusContent {
			contentStyle = contentStyle.BorderStyle(lipgloss.NormalBorder()).
				BorderLeft(true).
				BorderForeground(accentColor)
		}

		contentStr := a.renderContentPanel(contentWidth, mainHeight)
		contentView := contentStyle.Render(contentStr)

		mainPanel = lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, contentView)
	} else {
		// Collapsed: content only.
		contentStyle := lipgloss.NewStyle().
			Width(a.width).
			Height(mainHeight)

		contentStr := a.renderContentPanel(a.width, mainHeight)
		mainPanel = contentStyle.Render(contentStr)
	}

	// REPL panel at the bottom.
	replStyle := lipgloss.NewStyle().
		Width(a.width).
		Height(rHeight).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true)

	if a.focus == focusREPL {
		replStyle = replStyle.BorderForeground(accentColor)
	}

	replView := replStyle.Render(a.repl.View(a.width, rHeight))

	// Status bar.
	statusBar := a.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, mainPanel, replView, statusBar)
}

func (a *App) renderContentPanel(width, height int) string {
	switch a.activeView {
	case viewTable:
		return a.tableBrowser.View(width, height)
	case viewQuery:
		return a.queryEditor.View(width, height)
	default:
		return a.content.View(width, height)
	}
}

func (a *App) renderStatusBar() string {
	status := " VIBE STUDIO"
	if a.unsaved {
		status += " [modified]"
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

	padding := lipgloss.NewStyle().
		Width(gap).
		Background(lipgloss.Color("235")).
		Render("")

	return left + padding + right
}
