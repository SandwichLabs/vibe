package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

var (
	detailKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Width(20)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	detailCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2)
)

// renderDetail renders key-value detail data in either card or list layout.
func renderDetail(cfg *renderer.DetailConfig, maxWidth int) string {
	layout := cfg.Layout
	if layout == "" {
		layout = "card"
	}

	var b strings.Builder

	for rowIdx, row := range cfg.Result.Rows {
		if rowIdx > 0 {
			b.WriteString("\n")
		}

		var pairs strings.Builder
		for i, col := range cfg.Result.Columns {
			val := ""
			if i < len(row) {
				val = formatCellValue(row[i])
			}

			key := detailKeyStyle.Render(col.Name)
			value := detailValueStyle.Render(val)

			if layout == "list" {
				pairs.WriteString(fmt.Sprintf("%s  %s\n", key, value))
			} else {
				pairs.WriteString(fmt.Sprintf("%s  %s\n", key, value))
			}
		}

		if layout == "card" {
			cardWidth := maxWidth - 6
			if cardWidth < 20 {
				cardWidth = 20
			}
			b.WriteString(detailCardStyle.Width(cardWidth).Render(pairs.String()))
		} else {
			b.WriteString(pairs.String())
		}
	}

	return b.String()
}
