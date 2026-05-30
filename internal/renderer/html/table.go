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

	if cfg.PageSize > 0 {
		b.WriteString(fmt.Sprintf(`<div class="vibe-table-container" data-page-size="%d">`, cfg.PageSize))
	} else {
		b.WriteString(`<div class="vibe-table-container">`)
	}
	b.WriteString("\n")

	if cfg.Title != "" {
		b.WriteString(fmt.Sprintf(`  <div class="vibe-table-title">%s</div>`, html.EscapeString(cfg.Title)))
		b.WriteString("\n")
	}
	if cfg.Subtitle != "" {
		b.WriteString(fmt.Sprintf(`  <div class="vibe-table-subtitle">%s</div>`, html.EscapeString(cfg.Subtitle)))
		b.WriteString("\n")
	}

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
	for ri, row := range cfg.Result.Rows {
		if cfg.RowShading {
			cls := "vibe-row-even"
			if ri%2 == 1 {
				cls = "vibe-row-odd"
			}
			b.WriteString(fmt.Sprintf("      <tr class=\"%s\">\n", cls))
		} else {
			b.WriteString("      <tr>\n")
		}
		for i, cell := range row {
			var cellStr string
			if cfg.ColumnFmt != nil && i < len(cfg.Result.Columns) {
				if fmtCode, ok := cfg.ColumnFmt[cfg.Result.Columns[i].Name]; ok && fmtCode != "" {
					cellStr = renderer.Format(cell, fmtCode)
				} else {
					cellStr = formatValue(cell)
				}
			} else {
				cellStr = formatValue(cell)
			}
			b.WriteString(fmt.Sprintf("        <td>%s</td>\n", html.EscapeString(cellStr)))
		}
		b.WriteString("      </tr>\n")
	}
	b.WriteString("    </tbody>\n")

	b.WriteString("  </table>\n")

	// Client-side pagination scaffold. The page size is carried on the container
	// (data-page-size) and the controls element is the hook for client JS.
	if cfg.PageSize > 0 {
		b.WriteString(fmt.Sprintf(
			"  <div class=\"vibe-table-pagination\" data-page-size=\"%d\"></div>\n",
			cfg.PageSize,
		))
	}

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
