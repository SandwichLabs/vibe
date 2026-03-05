package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

var (
	metricValueStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39")).
				Align(lipgloss.Center)

	metricLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Align(lipgloss.Center)

	metricBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 3)
)

// renderMetric renders a MetricConfig as a large styled value with a label.
func renderMetric(cfg *renderer.MetricConfig, maxWidth int) string {
	formattedValue := formatMetricValue(cfg.Value, cfg.Format)

	boxWidth := maxWidth / 3
	if boxWidth < 20 {
		boxWidth = 20
	}
	if boxWidth > 40 {
		boxWidth = 40
	}

	value := metricValueStyle.Width(boxWidth - 8).Render(formattedValue)
	label := metricLabelStyle.Width(boxWidth - 8).Render(cfg.Label)

	content := lipgloss.JoinVertical(lipgloss.Center, value, label)

	return metricBoxStyle.Width(boxWidth).Render(content)
}

// formatMetricValue formats a value according to the specified format type.
func formatMetricValue(value any, format string) string {
	if value == nil {
		return "--"
	}

	f := toFloat(value)

	switch format {
	case "currency":
		return fmt.Sprintf("$%s", formatNumber(f))
	case "percent":
		return fmt.Sprintf("%s%%", formatNumber(f))
	case "bytes":
		return formatBytes(f)
	case "duration":
		return formatDuration(f)
	case "number":
		return formatNumber(f)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// formatNumber formats a float64 with comma separators and up to two decimal places.
func formatNumber(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")

	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]

	negative := false
	if strings.HasPrefix(intPart, "-") {
		negative = true
		intPart = intPart[1:]
	}

	if len(intPart) > 3 {
		var result strings.Builder
		for i, ch := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				result.WriteByte(',')
			}
			result.WriteRune(ch)
		}
		intPart = result.String()
	}

	if negative {
		intPart = "-" + intPart
	}

	if len(parts) == 2 {
		return intPart + "." + parts[1]
	}
	return intPart
}

// formatBytes formats a byte count into a human-readable string.
func formatBytes(bytes float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := 0
	v := bytes
	for v >= 1024 && unit < len(units)-1 {
		v /= 1024
		unit++
	}
	return fmt.Sprintf("%s %s", formatNumber(v), units[unit])
}

// formatDuration formats seconds into a human-readable duration.
func formatDuration(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.0fs", seconds)
	}
	if seconds < 3600 {
		m := int(seconds) / 60
		s := int(seconds) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
