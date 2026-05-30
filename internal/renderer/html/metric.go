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

	if cfg.Title != "" {
		b.WriteString(fmt.Sprintf(`  <div class="vibe-metric-title">%s</div>`, html.EscapeString(cfg.Title)))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf(`  <div class="vibe-metric-value">%s</div>`, escapedValue))
	b.WriteString("\n")

	if escapedLabel != "" {
		b.WriteString(fmt.Sprintf(`  <div class="vibe-metric-label">%s</div>`, escapedLabel))
		b.WriteString("\n")
	}

	// Comparison delta vs a prior value.
	if cfg.Comparison != "" && cfg.PriorValue != nil {
		b.WriteString(renderMetricDelta(cfg))
	}

	// Sparkline from a numeric series.
	if cfg.Sparkline && len(cfg.Series) > 0 {
		b.WriteString(renderMetricSparkline(cfg.Series))
	}

	b.WriteString("</div>")

	return b.String(), nil
}

// renderMetricDelta renders the comparison delta element. The direction class
// (up/down/flat) encodes the sign of the change vs the prior value, and the
// element text carries the signed percent change.
func renderMetricDelta(cfg *renderer.MetricConfig) string {
	cur := renderer.ToFloat64(cfg.Value)
	prior := renderer.ToFloat64(cfg.PriorValue)

	direction := "flat"
	sign := ""
	switch {
	case cur > prior:
		direction = "up"
		sign = "+"
	case cur < prior:
		direction = "down"
	}

	var pct float64
	if prior != 0 {
		pct = (cur - prior) / prior * 100
	}

	pctText := fmt.Sprintf("%s%.1f%%", sign, pct)

	return fmt.Sprintf(
		"  <div class=\"vibe-metric-delta %s\">%s</div>\n",
		direction, html.EscapeString(pctText),
	)
}

// renderMetricSparkline renders inline SVG sparkline markup for a numeric series.
func renderMetricSparkline(series []float64) string {
	const width, height = 100.0, 24.0

	minV, maxV := series[0], series[0]
	for _, v := range series {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	span := maxV - minV
	if span == 0 {
		span = 1
	}

	var pts strings.Builder
	for i, v := range series {
		var x float64
		if len(series) > 1 {
			x = float64(i) / float64(len(series)-1) * width
		}
		y := height - (v-minV)/span*height
		if i > 0 {
			pts.WriteByte(' ')
		}
		pts.WriteString(fmt.Sprintf("%.2f,%.2f", x, y))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		"  <svg class=\"vibe-metric-sparkline\" viewBox=\"0 0 %g %g\" preserveAspectRatio=\"none\">\n",
		width, height,
	))
	b.WriteString(fmt.Sprintf(
		"    <polyline fill=\"none\" stroke=\"currentColor\" points=\"%s\" />\n",
		pts.String(),
	))
	b.WriteString("  </svg>\n")
	return b.String()
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
