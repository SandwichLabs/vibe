package html

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// RenderChart renders a ChartConfig as a <canvas> element with Chart.js initialization.
func (r *Renderer) RenderChart(cfg *renderer.ChartConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("chart config is required")
	}
	if cfg.Result == nil {
		return "", fmt.Errorf("chart result set is required")
	}

	chartType := cfg.Type
	if chartType == "" {
		chartType = "bar"
	}

	// Map "area" to Chart.js "line" with fill
	chartJSType := chartType
	fill := false
	if chartType == "area" {
		chartJSType = "line"
		fill = true
	}

	// Extract labels (X axis) and data (Y axis) from result set
	labels, data := extractChartData(cfg)

	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chart labels: %w", err)
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chart data: %w", err)
	}

	canvasID := fmt.Sprintf("vibe-chart-%s", cfg.Source)
	color := cfg.Color
	if color == "" {
		color = "#4A90D9"
	}

	title := html.EscapeString(cfg.Title)

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div class="vibe-chart-container">`, ))
	b.WriteString("\n")
	if title != "" {
		b.WriteString(fmt.Sprintf(`  <h3 class="vibe-chart-title">%s</h3>`, title))
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf(`  <canvas id="%s"></canvas>`, html.EscapeString(canvasID)))
	b.WriteString("\n")
	b.WriteString("  <script>\n")
	b.WriteString(fmt.Sprintf(`    new Chart(document.getElementById(%q), {`, canvasID))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`      type: %q,`, chartJSType))
	b.WriteString("\n")
	b.WriteString("      data: {\n")
	b.WriteString(fmt.Sprintf("        labels: %s,\n", labelsJSON))
	b.WriteString("        datasets: [{\n")
	b.WriteString(fmt.Sprintf("          label: %q,\n", cfg.Y))
	b.WriteString(fmt.Sprintf("          data: %s,\n", dataJSON))
	b.WriteString(fmt.Sprintf("          backgroundColor: %q,\n", color))
	b.WriteString(fmt.Sprintf("          borderColor: %q,\n", color))
	if fill {
		b.WriteString("          fill: true,\n")
	} else {
		b.WriteString("          fill: false,\n")
	}
	b.WriteString("        }]\n")
	b.WriteString("      }\n")
	b.WriteString("    });\n")
	b.WriteString("  </script>\n")
	b.WriteString("</div>")

	return b.String(), nil
}

// extractChartData pulls X-axis labels and Y-axis values from the result set.
func extractChartData(cfg *renderer.ChartConfig) (labels []string, data []float64) {
	if cfg.Result == nil || len(cfg.Result.Columns) == 0 {
		return nil, nil
	}

	xIdx := -1
	yIdx := -1
	for i, col := range cfg.Result.Columns {
		if col.Name == cfg.X {
			xIdx = i
		}
		if col.Name == cfg.Y {
			yIdx = i
		}
	}

	// Fallback: use first two columns if named columns not found
	if xIdx < 0 && len(cfg.Result.Columns) > 0 {
		xIdx = 0
	}
	if yIdx < 0 && len(cfg.Result.Columns) > 1 {
		yIdx = 1
	}
	if yIdx < 0 {
		yIdx = 0
	}

	for _, row := range cfg.Result.Rows {
		if xIdx < len(row) {
			labels = append(labels, fmt.Sprintf("%v", row[xIdx]))
		}
		if yIdx < len(row) {
			data = append(data, toFloat64(row[yIdx]))
		}
	}

	return labels, data
}

// toFloat64 attempts to convert a value to float64.
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}
