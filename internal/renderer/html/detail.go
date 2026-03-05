package html

import (
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// RenderDetail renders a DetailConfig as an HTML definition list.
func (r *Renderer) RenderDetail(cfg *renderer.DetailConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("detail config is required")
	}
	if cfg.Result == nil {
		return "", fmt.Errorf("detail result set is required")
	}

	layout := cfg.Layout
	if layout == "" {
		layout = "card"
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf(`<div class="vibe-detail vibe-detail-%s">`, html.EscapeString(layout)))
	b.WriteString("\n")

	// Render each row as a definition list
	for _, row := range cfg.Result.Rows {
		b.WriteString("  <dl>\n")
		for i, col := range cfg.Result.Columns {
			escapedName := html.EscapeString(col.Name)
			val := ""
			if i < len(row) {
				val = html.EscapeString(formatValue(row[i]))
			}
			b.WriteString(fmt.Sprintf("    <dt>%s</dt>\n", escapedName))
			b.WriteString(fmt.Sprintf("    <dd>%s</dd>\n", val))
		}
		b.WriteString("  </dl>\n")
	}

	b.WriteString("</div>")

	return b.String(), nil
}
