package html

// P3.8 (plan §3.3): banded report HTML renderer + print/page CSS.
//
// RenderReport walks a precomputed band tree (reports.BandTree from P3.7) and
// emits Access-style banded HTML: a report-header band carrying the title, one
// group-header band per group, nested detail rows, a group-footer band with the
// formatted subtotal, and a report-footer band with the formatted grand total.
// It also emits print CSS (@media print + page-break rules) and a page-footer
// element carrying a page number + date so the output paginates sensibly when
// printed.

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/sandwich-labs/vibe-app/internal/reports"
)

// RenderReportOptions carries presentation options for RenderReport: per-column
// value format codes (renderer.Format) for detail bands and per-aggregate
// format codes for group/report footers.
type RenderReportOptions struct {
	ColumnFmt map[string]string
	AggFmt    map[string]string
}

// RenderReport walks the computed band tree and emits banded HTML (report/page
// header-footer, group header/footer with subtotals, detail rows, page
// numbers/dates, print CSS, pagination).
func (r *Renderer) RenderReport(tree *reports.BandTree, spec reports.ReportSpec, opts RenderReportOptions) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("band tree is required")
	}

	var b strings.Builder

	b.WriteString(SharedComponentCSS)
	b.WriteString("\n")
	b.WriteString(reportPrintCSS)
	b.WriteString("\n")

	b.WriteString(`<section class="vibe-report" aria-label="`)
	b.WriteString(html.EscapeString(spec.Title))
	b.WriteString("\">\n")

	// Report header band.
	b.WriteString(`  <header class="vibe-report-header">`)
	b.WriteString(`<h1 class="vibe-report-title">`)
	b.WriteString(html.EscapeString(spec.Title))
	b.WriteString("</h1></header>\n")

	if len(tree.Groups) == 0 {
		// Empty-report state (not an error).
		b.WriteString(`  <div class="vibe-report-empty">No data to display.</div>` + "\n")
	} else {
		for _, g := range tree.Groups {
			r.writeGroupBand(&b, spec, opts, g)
		}
	}

	// Report footer band (grand totals).
	b.WriteString(`  <footer class="vibe-report-footer">` + "\n")
	r.writeAggregates(&b, "vibe-report-footer-agg", spec.ReportFooterAggregates, tree.FooterAggregates, opts)
	b.WriteString("  </footer>\n")

	// Page footer with page number + date (resolved by print CSS counters).
	b.WriteString(`  <div class="vibe-page-footer">`)
	b.WriteString(`<span class="vibe-page-number"></span>`)
	b.WriteString(fmt.Sprintf(`<span class="vibe-page-date">%s</span>`, html.EscapeString(time.Now().Format("2006-01-02"))))
	b.WriteString("</div>\n")

	b.WriteString("</section>\n")

	return b.String(), nil
}

// writeGroupBand emits a group-header band, its detail rows, and a group-footer
// band carrying the formatted subtotals.
func (r *Renderer) writeGroupBand(b *strings.Builder, spec reports.ReportSpec, opts RenderReportOptions, g reports.GroupBand) {
	b.WriteString(`  <div class="vibe-report-group">` + "\n")

	// Group header band.
	b.WriteString(`    <div class="vibe-group-header">`)
	b.WriteString(html.EscapeString(g.Key))
	b.WriteString("</div>\n")

	// Detail rows.
	for _, row := range g.Details {
		b.WriteString(`    <div class="vibe-report-detail">`)
		for i, col := range spec.Detail.Columns {
			var cell any
			if i < len(row) {
				cell = row[i]
			}
			formatted := renderer.Format(cell, opts.ColumnFmt[col.Field])
			b.WriteString(fmt.Sprintf(`<span class="vibe-report-cell" data-field="%s">%s</span>`,
				html.EscapeString(col.Field), html.EscapeString(formatted)))
		}
		b.WriteString("</div>\n")
	}

	// Group footer band (subtotals).
	b.WriteString(`    <div class="vibe-group-footer">` + "\n")
	r.writeAggregates(b, "vibe-group-footer-agg", spec.GroupFooterAggregates, g.FooterAggregates, opts)
	b.WriteString("    </div>\n")

	b.WriteString("  </div>\n")
}

// writeAggregates emits one element per aggregate, formatted via renderer.Format
// using the per-aggregate format code (falling back to a plain number).
func (r *Renderer) writeAggregates(b *strings.Builder, cls string, aggs []reports.Aggregate, values map[string]float64, opts RenderReportOptions) {
	for _, a := range aggs {
		val := values[a.Name]
		formatted := renderer.Format(val, opts.AggFmt[a.Name])
		b.WriteString(fmt.Sprintf(`    <span class="%s" data-agg="%s">%s</span>`+"\n",
			cls, html.EscapeString(a.Name), html.EscapeString(formatted)))
	}
}

// reportPrintCSS provides screen + print styling for banded reports. Print rules
// are guarded by @media print and add page-break boundaries on groups plus a
// running page number/date footer.
const reportPrintCSS = `<style>
@media print {
  .vibe-report-header { page-break-after: avoid; }
  .vibe-report-group { page-break-inside: avoid; }
  .vibe-report-footer { page-break-before: auto; }
  .vibe-page-footer { position: fixed; bottom: 0; width: 100%; }
  .vibe-page-number::after { content: "Page " counter(page); }
}
</style>`
