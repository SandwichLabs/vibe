package tui

import (
	"log/slog"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// Renderer converts page nodes to terminal output using Lipgloss and Glamour.
type Renderer struct {
	logger *slog.Logger
	width  int
}

// NewRenderer creates a new TUI renderer with the specified terminal width.
func NewRenderer(width int, logger *slog.Logger) *Renderer {
	if logger == nil {
		logger = slog.Default()
	}
	if width <= 0 {
		width = 80
	}
	return &Renderer{
		logger: logger,
		width:  width,
	}
}

// RenderText renders markdown content to styled terminal output.
func (r *Renderer) RenderText(content string) string {
	return renderMarkdown(content, r.width)
}

// RenderTable renders a table configuration as a terminal table.
func (r *Renderer) RenderTable(cfg *renderer.TableConfig) string {
	if cfg == nil || cfg.Result == nil {
		return "(no table data)"
	}
	return renderTable(cfg, r.width)
}

// RenderChart renders a chart configuration as ASCII art.
func (r *Renderer) RenderChart(cfg *renderer.ChartConfig) string {
	if cfg == nil || cfg.Result == nil {
		return "(no chart data)"
	}
	return renderChart(cfg, r.width)
}

// RenderMetric renders a metric value in a large styled format.
func (r *Renderer) RenderMetric(cfg *renderer.MetricConfig) string {
	if cfg == nil {
		return "(no metric data)"
	}
	return renderMetric(cfg, r.width)
}

// RenderForm renders form fields as a display layout.
func (r *Renderer) RenderForm(cfg *renderer.FormConfig) string {
	if cfg == nil {
		return "(no form data)"
	}
	return renderForm(cfg, r.width)
}

// RenderDetail renders key-value detail display.
func (r *Renderer) RenderDetail(cfg *renderer.DetailConfig) string {
	if cfg == nil || cfg.Result == nil {
		return "(no detail data)"
	}
	return renderDetail(cfg, r.width)
}
