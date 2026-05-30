package html

// TDD RED — Phase 3 / unit P3.8 (plan §3.3): "Banded report HTML renderer +
// print/page CSS".
//
// This renderer unit test drives RenderReport over a precomputed band tree
// (the P3.7 reports.BandTree) and asserts the full banded-HTML structure:
//
//   - a report header band carrying the report title
//   - one group-header band per group (one per sales_rep)
//   - detail rows nested under each group, with values run through
//     renderer.Format
//   - a group-footer band showing the formatted subtotal (=Sum)
//   - a report-footer band showing the formatted grand total
//   - print CSS (@media print + page-break-before/after on band boundaries)
//   - a page-number + date element in the footer
//   - an empty-report state (not an error) when there are no groups
//
// These assertions target behavior RenderReport does NOT yet implement (RED).

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reportTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// sampleBandTree builds a "sales by rep" band tree: Alice (50,100,200 -> 350)
// and Bob (75,300 -> 375), grand total 725. Detail rows carry (order_id, amount)
// matching the spec's detail columns.
func sampleBandTree() (*reports.BandTree, reports.ReportSpec) {
	spec := reports.ReportSpec{
		ID:     "rpt_sales_by_rep",
		Title:  "Sales by Rep",
		Source: "SELECT sales_rep, order_id, amount FROM orders",
		Groups: []reports.GroupLevel{{Field: "sales_rep", SortDir: "asc"}},
		Sort:   []reports.SortSpec{{Field: "amount", Dir: "asc"}},
		Detail: reports.DetailBand{
			Columns: []reports.ReportColumn{
				{Field: "order_id", Caption: "Order"},
				{Field: "amount", Caption: "Amount"},
			},
		},
		GroupFooterAggregates: []reports.Aggregate{
			{Name: "rep_total", Func: reports.AggSum, Field: "amount"},
		},
		ReportFooterAggregates: []reports.Aggregate{
			{Name: "grand_total", Func: reports.AggSum, Field: "amount"},
		},
	}

	tree := &reports.BandTree{
		Groups: []reports.GroupBand{
			{
				Key: "Alice",
				// columns: order_id, amount (matches spec.Detail.Columns order)
				Details: [][]any{
					{int64(5), 50.0},
					{int64(4), 100.0},
					{int64(1), 200.0},
				},
				FooterAggregates: map[string]float64{"rep_total": 350.0},
			},
			{
				Key: "Bob",
				Details: [][]any{
					{int64(2), 75.0},
					{int64(3), 300.0},
				},
				FooterAggregates: map[string]float64{"rep_total": 375.0},
			},
		},
		FooterAggregates: map[string]float64{"grand_total": 725.0},
	}
	return tree, spec
}

// TestRenderReportBandStructure asserts the full banded HTML structure.
func TestRenderReportBandStructure(t *testing.T) {
	r := NewRenderer(reportTestLogger())
	tree, spec := sampleBandTree()

	// Per-column format: amount as currency, applied to detail + footer values.
	opts := RenderReportOptions{
		ColumnFmt: map[string]string{"amount": "usd0"},
		AggFmt:    map[string]string{"rep_total": "usd0", "grand_total": "usd0"},
	}

	html, err := r.RenderReport(tree, spec, opts)
	require.NoError(t, err)

	// Report header band with the title.
	assert.Regexp(t, `class="[^"]*vibe-report-header`, html,
		"must emit a report-header band")
	assert.Contains(t, html, "Sales by Rep", "report header must carry the report title")

	// One group-header band per rep, each carrying the group key.
	assert.Regexp(t, `class="[^"]*vibe-group-header`, html,
		"must emit a group-header band")
	assert.Equal(t, 2, strings.Count(html, "vibe-group-header"),
		"one group-header band per distinct sales_rep")
	assert.Contains(t, html, "Alice", "group header must carry the group key")
	assert.Contains(t, html, "Bob")

	// Detail rows nested under groups, with values run through renderer.Format.
	assert.Regexp(t, `class="[^"]*vibe-report-detail`, html,
		"must emit detail rows")
	// usd0 of 50/100/200 etc.
	assert.Contains(t, html, "$50", "amount cells must be formatted via renderer.Format (usd0)")
	assert.Contains(t, html, "$200")

	// Group-footer band with formatted subtotal (=Sum).
	assert.Regexp(t, `class="[^"]*vibe-group-footer`, html,
		"must emit a group-footer band")
	assert.Contains(t, html, "$350", "Alice group footer must show her formatted subtotal")
	assert.Contains(t, html, "$375", "Bob group footer must show his formatted subtotal")

	// Report-footer band with formatted grand total.
	assert.Regexp(t, `class="[^"]*vibe-report-footer`, html,
		"must emit a report-footer band")
	assert.Contains(t, html, "$725", "report footer must show the formatted grand total")
}

// TestRenderReportPrintCSS asserts print CSS, page-break rules, and a
// page-number/date footer element are emitted.
func TestRenderReportPrintCSS(t *testing.T) {
	r := NewRenderer(reportTestLogger())
	tree, spec := sampleBandTree()

	html, err := r.RenderReport(tree, spec, RenderReportOptions{})
	require.NoError(t, err)

	assert.Contains(t, html, "@media print",
		"must emit print CSS guarded by @media print")
	assert.Regexp(t, `page-break-(before|after)`, html,
		"must emit page-break rules on band/page boundaries")
	assert.Regexp(t, `class="[^"]*vibe-page-number`, html,
		"must emit a page-number element in the page footer")
	assert.Regexp(t, `class="[^"]*vibe-page-date`, html,
		"must emit a date element in the page footer")
}

// TestRenderReportEmpty asserts an empty band tree renders an empty-report
// state, not an error.
func TestRenderReportEmpty(t *testing.T) {
	r := NewRenderer(reportTestLogger())
	_, spec := sampleBandTree()

	empty := &reports.BandTree{
		Groups:           []reports.GroupBand{},
		FooterAggregates: map[string]float64{},
	}

	html, err := r.RenderReport(empty, spec, RenderReportOptions{})
	require.NoError(t, err, "empty result must not be an error")
	assert.Regexp(t, `class="[^"]*vibe-report-empty`, html,
		"empty band tree must render an empty-report state")
}
