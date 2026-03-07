package tui

import (
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRenderer(t *testing.T) {
	r := NewRenderer(80, nil)
	require.NotNil(t, r)
	assert.Equal(t, 80, r.width)
}

func TestNewRenderer_DefaultWidth(t *testing.T) {
	r := NewRenderer(0, nil)
	assert.Equal(t, 80, r.width)
}

func TestRenderer_RenderText(t *testing.T) {
	r := NewRenderer(80, nil)
	out := r.RenderText("# Hello\n\nWorld")
	assert.Contains(t, out, "Hello")
	assert.Contains(t, out, "World")
}

func TestRenderer_RenderTable(t *testing.T) {
	r := NewRenderer(80, nil)

	result := r.RenderTable(nil)
	assert.Equal(t, "(no table data)", result)

	cfg := &renderer.TableConfig{
		Result: &renderer.ResultSet{
			Columns: []renderer.Column{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			Rows: [][]any{
				{1, "Alice"},
				{2, "Bob"},
			},
		},
	}
	result = r.RenderTable(cfg)
	assert.Contains(t, result, "id")
	assert.Contains(t, result, "name")
	assert.Contains(t, result, "Alice")
	assert.Contains(t, result, "(2 rows)")
}

func TestRenderer_RenderChart(t *testing.T) {
	r := NewRenderer(80, nil)

	result := r.RenderChart(nil)
	assert.Equal(t, "(no chart data)", result)

	cfg := &renderer.ChartConfig{
		Title: "Test Chart",
		X:     "label",
		Y:     "value",
		Result: &renderer.ResultSet{
			Columns: []renderer.Column{
				{Name: "label", Type: "string"},
				{Name: "value", Type: "float64"},
			},
			Rows: [][]any{
				{"A", 10.0},
				{"B", 20.0},
				{"C", 15.0},
			},
		},
	}
	result = r.RenderChart(cfg)
	assert.Contains(t, result, "Test Chart")
	assert.Contains(t, result, "A")
	assert.Contains(t, result, "B")
}

func TestRenderer_RenderMetric(t *testing.T) {
	r := NewRenderer(80, nil)

	result := r.RenderMetric(nil)
	assert.Equal(t, "(no metric data)", result)

	cfg := &renderer.MetricConfig{
		Label:  "Revenue",
		Value:  42500.0,
		Format: "currency",
	}
	result = r.RenderMetric(cfg)
	assert.Contains(t, result, "Revenue")
	assert.Contains(t, result, "$42,500")
}

func TestRenderer_RenderForm(t *testing.T) {
	r := NewRenderer(80, nil)

	result := r.RenderForm(nil)
	assert.Equal(t, "(no form data)", result)

	cfg := &renderer.FormConfig{
		Fields: []renderer.FormField{
			{Name: "email", Label: "Email", Type: "email", Required: true},
			{Name: "name", Label: "Name", Type: "text"},
		},
	}
	result = r.RenderForm(cfg)
	assert.Contains(t, result, "Email")
	assert.Contains(t, result, "Name")
	assert.Contains(t, result, "Submit")
}

func TestRenderer_RenderDetail(t *testing.T) {
	r := NewRenderer(80, nil)

	result := r.RenderDetail(nil)
	assert.Equal(t, "(no detail data)", result)

	cfg := &renderer.DetailConfig{
		Layout: "card",
		Result: &renderer.ResultSet{
			Columns: []renderer.Column{
				{Name: "Name", Type: "string"},
				{Name: "Age", Type: "int"},
			},
			Rows: [][]any{
				{"Alice", 30},
			},
		},
	}
	result = r.RenderDetail(cfg)
	assert.Contains(t, result, "Name")
	assert.Contains(t, result, "Alice")
	assert.Contains(t, result, "Age")
}

func TestFormatMetricValue(t *testing.T) {
	tests := []struct {
		value    any
		format   string
		expected string
	}{
		{nil, "number", "--"},
		{42.0, "number", "42"},
		{1234567.89, "currency", "$1,234,567.89"},
		{0.95, "percent", "0.95%"},
		{1048576.0, "bytes", "1 MB"},
		{3661.0, "duration", "1h 1m"},
		{"hello", "", "hello"},
	}

	for _, tt := range tests {
		cfg := &renderer.MetricConfig{Value: tt.value, Format: tt.format}
		result := formatMetricValue(cfg)
		assert.Equal(t, tt.expected, result, "format=%s value=%v", tt.format, tt.value)
	}
}
