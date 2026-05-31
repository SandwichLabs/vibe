package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

const (
	barChar   = "\u2588" // Full block character
	chartRows = 10       // Default number of rows for ASCII chart
)

var (
	chartTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	barStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	axisStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// renderChart renders a ChartConfig as an ASCII bar chart.
func renderChart(cfg *renderer.ChartConfig, maxWidth int) string {
	labels, values := extractChartValues(cfg)
	if len(values) == 0 {
		return "(no chart data)"
	}

	var b strings.Builder

	// Title
	if cfg.Title != "" {
		b.WriteString(chartTitleStyle.Render(cfg.Title))
		b.WriteString("\n\n")
	}

	// Find max value for scaling.
	maxVal := 0.0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	// Calculate label width.
	labelWidth := 0
	for _, l := range labels {
		if len(l) > labelWidth {
			labelWidth = len(l)
		}
	}
	if labelWidth > 15 {
		labelWidth = 15
	}

	barMaxWidth := maxWidth - labelWidth - 12 // space for label, value, padding
	if barMaxWidth < 10 {
		barMaxWidth = 10
	}

	// Render each bar.
	for i, v := range values {
		label := labels[i]
		if len(label) > labelWidth {
			label = label[:labelWidth-1] + "~"
		}

		barLen := int(math.Round(v / maxVal * float64(barMaxWidth)))
		if barLen < 0 {
			barLen = 0
		}

		bar := barStyle.Render(strings.Repeat(barChar, barLen))
		labelFmt := fmt.Sprintf("%*s", labelWidth, label)
		valueFmt := fmt.Sprintf(" %.1f", v)

		b.WriteString(axisStyle.Render(labelFmt))
		b.WriteString(" ")
		b.WriteString(axisStyle.Render("|"))
		b.WriteString(bar)
		b.WriteString(valueFmt)
		b.WriteString("\n")
	}

	return b.String()
}

// extractChartValues pulls labels and numeric values from a ChartConfig's result set.
func extractChartValues(cfg *renderer.ChartConfig) (labels []string, values []float64) {
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

	// Fallback to first two columns.
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
			values = append(values, renderer.ToFloat64(row[yIdx]))
		}
	}

	return labels, values
}
