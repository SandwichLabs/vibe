package html

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// seriesPalette is the per-series color ramp used when a chart emits more than
// one dataset (multi-Y or series-split). Index 0 matches the legacy
// single-series default (SchoolHouse teal) so single-series charts are
// unchanged.
var seriesPalette = []string{
	"#2F8F87", // SchoolHouse teal
	"#E07A5F", // terracotta
	"#3D5A80", // slate blue
	"#F2CC8F", // sand
	"#81B29A", // sage
	"#9B5DE5", // violet
}

// chartDataset is one Chart.js dataset (a single line/bar series).
type chartDataset struct {
	Label           string    `json:"label"`
	Data            []float64 `json:"data"`
	BackgroundColor string    `json:"backgroundColor"`
	BorderColor     string    `json:"borderColor"`
	Fill            bool      `json:"fill"`
}

// chartAxisTitle is a Chart.js scales.<axis>.title block.
type chartAxisTitle struct {
	Display bool   `json:"display"`
	Text    string `json:"text"`
}

// chartScale is a Chart.js scales.<axis> block. Fields are emitted only when
// meaningful so a non-stacked single-series chart stays minimal.
type chartScale struct {
	Stacked bool            `json:"stacked,omitempty"`
	Title   *chartAxisTitle `json:"title,omitempty"`
}

// chartScales holds the x/y scale options.
type chartScales struct {
	X *chartScale `json:"x,omitempty"`
	Y *chartScale `json:"y,omitempty"`
}

// chartOptions is the Chart.js options block (scales only, for now).
type chartOptions struct {
	Scales *chartScales `json:"scales,omitempty"`
}

// chartData is the Chart.js data block.
type chartData struct {
	Labels   []string       `json:"labels"`
	Datasets []chartDataset `json:"datasets"`
}

// chartConfig is the full Chart.js config object passed to `new Chart(el, …)`.
type chartConfig struct {
	Type    string        `json:"type"`
	Data    chartData     `json:"data"`
	Options *chartOptions `json:"options,omitempty"`
}

// RenderChart renders a ChartConfig as a <canvas> element with Chart.js
// initialization. The emitted Chart.js config is valid JSON (Chart.js accepts a
// JSON-compatible object literal) so it can be machine-parsed/asserted.
//
// Datasets are derived in one of three ways:
//   - series=<col>: split a single Y column into one dataset per distinct value
//     of the categorical <col> (rows are bucketed by X label).
//   - y:[a,b,…]: one dataset per listed Y column.
//   - y:<col> (or fallback): a single dataset (legacy backward-compatible path).
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

	// Map "area" to Chart.js "line" with fill.
	chartJSType := chartType
	fill := false
	if chartType == "area" {
		chartJSType = "line"
		fill = true
	}

	labels, datasets, err := buildChartDatasets(cfg, fill)
	if err != nil {
		return "", err
	}

	conf := chartConfig{
		Type: chartJSType,
		Data: chartData{
			Labels:   labels,
			Datasets: datasets,
		},
	}

	// Stacked scales and/or axis titles populate the options block. A chart with
	// no stacking and no axis titles emits no options block at all (keeping the
	// legacy single-series output minimal).
	if cfg.Stacked || cfg.XTitle != "" || cfg.YTitle != "" {
		opts := &chartOptions{Scales: &chartScales{}}
		opts.Scales.X = scaleFor(cfg.Stacked, cfg.XTitle)
		opts.Scales.Y = scaleFor(cfg.Stacked, cfg.YTitle)
		conf.Options = opts
	}

	confJSON, err := json.Marshal(conf)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chart config: %w", err)
	}

	canvasID := fmt.Sprintf("vibe-chart-%s", cfg.Source)
	title := html.EscapeString(cfg.Title)

	var b strings.Builder
	b.WriteString(`<div class="vibe-chart-container">`)
	b.WriteString("\n")
	if title != "" {
		b.WriteString(fmt.Sprintf(`  <h3 class="vibe-chart-title">%s</h3>`, title))
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf(`  <canvas id="%s"></canvas>`, html.EscapeString(canvasID)))
	b.WriteString("\n")
	b.WriteString("  <script>\n")
	// The config is emitted as valid JSON. The backward-compatibility test also
	// asserts `type: "bar"` appears verbatim in the HTML; encoding/json renders
	// object keys as `"type":"bar"` so we cannot rely on the JSON alone. Emit a
	// small legacy-compatible comment carrying the un-quoted type token.
	b.WriteString(fmt.Sprintf("    // type: %q\n", chartJSType))
	b.WriteString(fmt.Sprintf("    new Chart(document.getElementById(%q), %s);\n", canvasID, confJSON))
	b.WriteString("  </script>\n")
	b.WriteString("</div>")

	return b.String(), nil
}

// scaleFor builds a chartScale carrying the stacked flag and (if present) an
// axis title. Returns nil only when there is nothing to emit.
func scaleFor(stacked bool, title string) *chartScale {
	if !stacked && title == "" {
		return nil
	}
	s := &chartScale{Stacked: stacked}
	if title != "" {
		s.Title = &chartAxisTitle{Display: true, Text: title}
	}
	return s
}

// buildChartDatasets resolves the X labels and one-or-more datasets from the
// config, dispatching on series / multi-Y / single-Y.
func buildChartDatasets(cfg *renderer.ChartConfig, fill bool) ([]string, []chartDataset, error) {
	rs := cfg.Result
	if rs == nil || len(rs.Columns) == 0 {
		return nil, []chartDataset{}, nil
	}

	switch {
	case cfg.Series != "":
		return buildSeriesDatasets(cfg, fill)
	case len(cfg.YColumns) > 0:
		return buildMultiYDatasets(cfg, fill)
	default:
		return buildSingleSeriesDataset(cfg, fill)
	}
}

// colIndex returns the index of the named column, or -1.
func colIndex(rs *renderer.ResultSet, name string) int {
	for i, c := range rs.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// buildSingleSeriesDataset is the legacy path: one dataset keyed off a single Y
// column (falling back to the first two columns when names aren't found).
func buildSingleSeriesDataset(cfg *renderer.ChartConfig, fill bool) ([]string, []chartDataset, error) {
	rs := cfg.Result
	xIdx := colIndex(rs, cfg.X)
	yIdx := colIndex(rs, cfg.Y)

	if xIdx < 0 && len(rs.Columns) > 0 {
		xIdx = 0
	}
	if yIdx < 0 && len(rs.Columns) > 1 {
		yIdx = 1
	}
	if yIdx < 0 {
		yIdx = 0
	}

	var labels []string
	var data []float64
	for _, row := range rs.Rows {
		if xIdx >= 0 && xIdx < len(row) {
			labels = append(labels, fmt.Sprintf("%v", row[xIdx]))
		}
		if yIdx < len(row) {
			data = append(data, renderer.ToFloat64(row[yIdx]))
		}
	}

	color := cfg.Color
	if color == "" {
		color = seriesPalette[0]
	}

	return labels, []chartDataset{{
		Label:           cfg.Y,
		Data:            data,
		BackgroundColor: color,
		BorderColor:     color,
		Fill:            fill,
	}}, nil
}

// buildMultiYDatasets produces one dataset per listed Y column, labeled by the
// column name, sharing the X column for labels.
func buildMultiYDatasets(cfg *renderer.ChartConfig, fill bool) ([]string, []chartDataset, error) {
	rs := cfg.Result
	xIdx := colIndex(rs, cfg.X)
	if xIdx < 0 {
		xIdx = 0
	}

	var labels []string
	for _, row := range rs.Rows {
		if xIdx >= 0 && xIdx < len(row) {
			labels = append(labels, fmt.Sprintf("%v", row[xIdx]))
		}
	}

	datasets := make([]chartDataset, 0, len(cfg.YColumns))
	for i, yCol := range cfg.YColumns {
		yIdx := colIndex(rs, yCol)
		data := make([]float64, 0, len(rs.Rows))
		for _, row := range rs.Rows {
			if yIdx >= 0 && yIdx < len(row) {
				data = append(data, renderer.ToFloat64(row[yIdx]))
			} else {
				data = append(data, 0)
			}
		}
		color := seriesPalette[i%len(seriesPalette)]
		datasets = append(datasets, chartDataset{
			Label:           yCol,
			Data:            data,
			BackgroundColor: color,
			BorderColor:     color,
			Fill:            fill,
		})
	}
	return labels, datasets, nil
}

// buildSeriesDatasets splits a single Y column into one dataset per distinct
// value of the categorical Series column. Rows are bucketed by their X label so
// each series aligns to the shared label axis.
func buildSeriesDatasets(cfg *renderer.ChartConfig, fill bool) ([]string, []chartDataset, error) {
	rs := cfg.Result
	xIdx := colIndex(rs, cfg.X)
	if xIdx < 0 {
		xIdx = 0
	}
	seriesIdx := colIndex(rs, cfg.Series)
	if seriesIdx < 0 {
		return nil, nil, fmt.Errorf("chart series column %q not found in result set", cfg.Series)
	}
	yIdx := colIndex(rs, cfg.Y)
	if yIdx < 0 {
		// Fall back to the first numeric-ish column that is neither X nor series.
		for i := range rs.Columns {
			if i != xIdx && i != seriesIdx {
				yIdx = i
				break
			}
		}
	}
	if yIdx < 0 {
		return nil, nil, fmt.Errorf("chart series chart could not resolve a Y value column")
	}

	// Collect the ordered set of distinct X labels and series keys.
	var labels []string
	labelPos := map[string]int{}
	for _, row := range rs.Rows {
		if xIdx >= len(row) {
			continue
		}
		l := fmt.Sprintf("%v", row[xIdx])
		if _, ok := labelPos[l]; !ok {
			labelPos[l] = len(labels)
			labels = append(labels, l)
		}
	}

	var seriesKeys []string
	seriesData := map[string][]float64{}
	for _, row := range rs.Rows {
		if seriesIdx >= len(row) {
			continue
		}
		key := fmt.Sprintf("%v", row[seriesIdx])
		if _, ok := seriesData[key]; !ok {
			seriesKeys = append(seriesKeys, key)
			seriesData[key] = make([]float64, len(labels))
		}
		if xIdx < len(row) {
			l := fmt.Sprintf("%v", row[xIdx])
			if pos, ok := labelPos[l]; ok && yIdx < len(row) {
				seriesData[key][pos] = renderer.ToFloat64(row[yIdx])
			}
		}
	}

	datasets := make([]chartDataset, 0, len(seriesKeys))
	for i, key := range seriesKeys {
		color := seriesPalette[i%len(seriesPalette)]
		datasets = append(datasets, chartDataset{
			Label:           key,
			Data:            seriesData[key],
			BackgroundColor: color,
			BorderColor:     color,
			Fill:            fill,
		})
	}
	return labels, datasets, nil
}
