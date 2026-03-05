package html

import (
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// RenderTable renders a TableConfig as an HTML table with sticky headers and optional sortable columns.
func (r *Renderer) RenderTable(cfg *renderer.TableConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("table config is required")
	}
	if cfg.Result == nil {
		return "", fmt.Errorf("table result set is required")
	}

	var b strings.Builder

	b.WriteString(`<div class="vibe-table-container">`)
	b.WriteString("\n")

	if cfg.Searchable {
		b.WriteString(`  <input type="text" class="vibe-table-search" placeholder="Search..." onkeyup="vibeTableSearch(this)">`)
		b.WriteString("\n")
	}

	b.WriteString(`  <table class="vibe-table">`)
	b.WriteString("\n")

	// Header
	b.WriteString("    <thead>\n      <tr>\n")
	for _, col := range cfg.Result.Columns {
		escapedName := html.EscapeString(col.Name)
		if cfg.Sortable {
			b.WriteString(fmt.Sprintf(`        <th class="vibe-sortable" onclick="vibeTableSort(this)">%s</th>`, escapedName))
		} else {
			b.WriteString(fmt.Sprintf("        <th>%s</th>", escapedName))
		}
		b.WriteString("\n")
	}
	b.WriteString("      </tr>\n    </thead>\n")

	// Body
	b.WriteString("    <tbody>\n")
	for _, row := range cfg.Result.Rows {
		b.WriteString("      <tr>\n")
		for _, cell := range row {
			b.WriteString(fmt.Sprintf("        <td>%s</td>\n", html.EscapeString(formatValue(cell))))
		}
		b.WriteString("      </tr>\n")
	}
	b.WriteString("    </tbody>\n")

	b.WriteString("  </table>\n")
	b.WriteString("</div>")

	return b.String(), nil
}

// formatValue converts an arbitrary value to a display string.
func formatValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
