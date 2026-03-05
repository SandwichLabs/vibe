package tui

import (
	"github.com/charmbracelet/glamour"
)

// renderMarkdown converts markdown text to styled terminal output using Glamour.
func renderMarkdown(content string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		// Fall back to plain text on error.
		return content
	}

	out, err := r.Render(content)
	if err != nil {
		return content
	}

	return out
}
