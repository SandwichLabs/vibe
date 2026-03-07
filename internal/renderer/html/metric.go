package html

import (
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// RenderMetric renders a MetricConfig as a styled metric card.
func (r *Renderer) RenderMetric(cfg *renderer.MetricConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("metric config is required")
	}

	formattedValue := formatMetricValue(cfg)
	escapedLabel := html.EscapeString(cfg.Label)
	escapedValue := html.EscapeString(formattedValue)

	var b strings.Builder
	b.WriteString(`<div class="vibe-metric">`)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`  <div class="vibe-metric-value">%s</div>`, escapedValue))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`  <div class="vibe-metric-label">%s</div>`, escapedLabel))
	b.WriteString("\n")
	b.WriteString("</div>")

	return b.String(), nil
}

// formatMetricValue formats a metric value. If cfg.Fmt is set, uses the
// Evidence-compatible format system. Otherwise falls back to legacy Format field.
func formatMetricValue(cfg *renderer.MetricConfig) string {
	if cfg.Value == nil {
		return "--"
	}

	// Prefer the new Evidence-compatible Fmt code
	if cfg.Fmt != "" {
		return renderer.Format(cfg.Value, cfg.Fmt)
	}

	// Legacy format field fallback
	switch cfg.Format {
	case "currency":
		return fmt.Sprintf("$%s", legacyFormatNumber(renderer.ToFloat64(cfg.Value)))
	case "percent":
		return fmt.Sprintf("%s%%", legacyFormatNumber(renderer.ToFloat64(cfg.Value)))
	case "bytes":
		return formatBytes(renderer.ToFloat64(cfg.Value))
	case "duration":
		return formatDuration(renderer.ToFloat64(cfg.Value))
	case "number":
		return legacyFormatNumber(renderer.ToFloat64(cfg.Value))
	default:
		return fmt.Sprintf("%v", cfg.Value)
	}
}

// legacyFormatNumber formats a float64 with comma separators and up to two decimal places.
// This preserves the original behavior for the legacy Format field.
func legacyFormatNumber(v float64) string {
	// Format with up to 2 decimal places, trimming trailing zeros
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")

	// Add comma separators to the integer part
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
	return fmt.Sprintf("%s %s", legacyFormatNumber(v), units[unit])
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
