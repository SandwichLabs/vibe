package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Colors
	accentColor    = lipgloss.Color("39")  // Blue
	dimColor       = lipgloss.Color("240") // Gray
	errorColor     = lipgloss.Color("196") // Red
	highlightColor = lipgloss.Color("229") // Yellow

	// List item styles
	listItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(highlightColor).
				Bold(true)

	// Content title style
	contentTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor).
				MarginBottom(1).
				Underline(true)

	// REPL prompt style
	replPromptStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	// REPL output style
	replOutputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	// Error text style
	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	// Dim text style
	dimStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	// Help key style
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	// Help description style
	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
)

// defaultTableStyles returns configured styles for bubbles/table.
func defaultTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(dimColor).
		BorderBottom(true).
		Bold(true).
		Foreground(accentColor)
	s.Selected = s.Selected.
		Foreground(highlightColor).
		Bold(true)
	return s
}
