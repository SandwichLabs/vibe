package tui

import (
	"fmt"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// renderTable renders a TableConfig as a terminal table with plain text formatting.
func renderTable(cfg *renderer.TableConfig, maxWidth int) string {
	if cfg == nil || cfg.Result == nil || len(cfg.Result.Columns) == 0 {
		return "(no table data)"
	}

	cols := cfg.Result.Columns
	rows := cfg.Result.Rows

	// Calculate column widths.
	widths := make([]int, len(cols))
	for i, col := range cols {
		widths[i] = len(col.Name)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				s := formatCellValue(cell)
				if len(s) > widths[i] {
					widths[i] = len(s)
				}
			}
		}
	}

	// Cap column widths to fit terminal.
	if maxWidth > 0 {
		totalWidth := 0
		for _, w := range widths {
			totalWidth += w + 3 // +3 for " | " separator
		}
		if totalWidth > maxWidth {
			maxColWidth := (maxWidth - 3*len(widths)) / len(widths)
			if maxColWidth < 10 {
				maxColWidth = 10
			}
			for i := range widths {
				if widths[i] > maxColWidth {
					widths[i] = maxColWidth
				}
			}
		}
	}

	var b strings.Builder

	// Header row.
	for i, col := range cols {
		b.WriteString(padRight(col.Name, widths[i]))
		if i < len(cols)-1 {
			b.WriteString(" | ")
		}
	}
	b.WriteString("\n")

	// Separator.
	for i, w := range widths {
		b.WriteString(strings.Repeat("-", w))
		if i < len(widths)-1 {
			b.WriteString("-+-")
		}
	}
	b.WriteString("\n")

	// Data rows.
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			s := formatCellValue(cell)
			if len(s) > widths[i] {
				s = s[:widths[i]-1] + "~"
			}
			b.WriteString(padRight(s, widths[i]))
			if i < len(row)-1 {
				b.WriteString(" | ")
			}
		}
		b.WriteString("\n")
	}

	// Row count summary.
	b.WriteString(fmt.Sprintf("\n(%d rows)", len(rows)))

	return b.String()
}

// padRight pads a string to the given width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// formatCellValue converts an arbitrary value to a display string.
func formatCellValue(v any) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprintf("%v", v)
}
