package agents

import (
	"encoding/json"
	"fmt"
	"strings"
)

// viewShape/viewFit/bestView are a Go port of studio/frontend/src/lib/
// notebookViews.ts (itself ported from schoolwork-workspace's
// src/notebook/views.js) — the SAME view-fitting rules, kept in sync by
// hand since preview_query (Go, this file) and the notebook cell view picker
// (TypeScript) both need to agree on what "suggestedView" means for a given
// result shape. Any rule change here must be mirrored there and vice versa —
// see notebookViews.spec.ts, whose fixtures this file's tests reuse.

// viewShape describes a result set: which columns hold numbers, which hold
// labels, and whether the first column's values are unique (a GROUP BY key
// vs. a second measure).
type viewShape struct {
	cols          []string
	numeric       map[string]bool
	rowCount      int
	firstDistinct bool
}

// isNumericValue judges a value from a Go-side result set (preview_query,
// run_query), which runs through engine.NormalizeValue: a small
// DECIMAL crosses as json.Number, not a Go float, so it must count as
// numeric too, or every DECIMAL column (which is most money/SUM columns)
// would stop fitting bar/line/stat and fall back to a label column instead.
// A json.Number OUT of the safe range crosses as a plain string instead
// (see the wire contract in engine/normalize.go) and is correctly judged a
// label here — that's the accepted "mixed column" edge case, not a bug.
func isNumericValue(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// describeViewShape judges a shape from the rows themselves (what the
// renderer will actually meet), not declared column types — matching
// describeShape in notebookViews.ts.
func describeViewShape(cols []string, rows [][]any) viewShape {
	numeric := make(map[string]bool, len(cols))
	for ci, c := range cols {
		for _, r := range rows {
			if ci >= len(r) || r[ci] == nil {
				continue
			}
			numeric[c] = isNumericValue(r[ci])
			break
		}
	}
	seen := make(map[string]bool, len(rows))
	firstDistinct := len(rows) > 0
	for _, r := range rows {
		var key string
		if len(r) > 0 {
			key = toDisplayString(r[0])
		}
		if seen[key] {
			firstDistinct = false
		}
		seen[key] = true
	}
	return viewShape{cols: cols, numeric: numeric, rowCount: len(rows), firstDistinct: firstDistinct}
}

func toDisplayString(v any) string {
	if v == nil {
		return "<nil>"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// viewFitResult mirrors notebookViews.ts's ViewFit.
type viewFitResult struct {
	OK  bool
	Why string
}

// checkViewFit reports whether view can render shape — same rules as
// viewFit() in notebookViews.ts.
func checkViewFit(view string, shape viewShape) viewFitResult {
	if len(shape.cols) == 0 {
		return viewFitResult{OK: false, Why: "the query returned no columns"}
	}
	switch view {
	case "table":
		return viewFitResult{OK: true}
	case "bar", "line", "area", "columns", "rings", "radar":
		return checkSeriesFit(view, shape)
	case "sankey", "heatmap":
		return checkTripleFit(view, shape)
	case "stat":
		if shape.rowCount == 0 {
			return viewFitResult{OK: false, Why: "needs one row — this query returned none"}
		}
		if shape.rowCount > 1 {
			return viewFitResult{OK: false, Why: "shows a single number, but this query returns more than one row — aggregate it, or add LIMIT 1"}
		}
		return viewFitResult{OK: true}
	default:
		return viewFitResult{OK: true}
	}
}

// checkSeriesFit is the rule for the label-then-numbers charts: a label
// column and at least one numeric column, plus radar's three-axis minimum and
// the rings' six-ring maximum.
func checkSeriesFit(view string, shape viewShape) viewFitResult {
	hasNumeric := false
	for _, n := range shape.numeric {
		if n {
			hasNumeric = true
			break
		}
	}
	if !hasNumeric {
		return viewFitResult{OK: false, Why: "needs a numeric column — this query returns only text (" + strings.Join(shape.cols, ", ") + ")"}
	}
	if len(shape.cols) < 2 {
		return viewFitResult{OK: false, Why: "needs two columns: something to label each point, then the number"}
	}
	if view == "radar" && shape.rowCount < 3 {
		return viewFitResult{OK: false, Why: "needs at least three rows, one per axis"}
	}
	if view == "rings" && shape.rowCount > maxRings {
		return viewFitResult{OK: false, Why: fmt.Sprintf("draws at most %d rings, but this query returns %d rows — filter or LIMIT it", maxRings, shape.rowCount)}
	}
	return viewFitResult{OK: true}
}

// maxRings is how many concentric rings the rings view draws.
const maxRings = 6

// checkTripleFit is the rule for sankey (source, target, weight) and heatmap
// (row, column, value): three columns, the third numeric.
func checkTripleFit(view string, shape viewShape) viewFitResult {
	want := "source, target, then a numeric weight"
	if view == "heatmap" {
		want = "a row label, a column label, then a numeric value"
	}
	if len(shape.cols) < 3 || !shape.numeric[shape.cols[2]] {
		return viewFitResult{OK: false, Why: "needs three columns: " + want}
	}
	return viewFitResult{OK: true}
}

var orderedNameHints = []string{"date", "day", "week", "month", "year", "term", "window", "period", "time", "quarter", "season"}

func looksOrderedName(name string) bool {
	n := strings.ToLower(name)
	for _, h := range orderedNameHints {
		if strings.Contains(n, h) {
			return true
		}
	}
	return false
}

const chartableRows = 25

// bestViewFor is the conservative "which view suits this shape" pick — same
// rules as bestView() in notebookViews.ts. Prefers table whenever a chart
// would be a guess.
func bestViewFor(shape viewShape) string {
	if shape.rowCount == 1 && len(shape.cols) == 1 {
		for _, c := range shape.cols {
			if shape.numeric[c] {
				return "stat"
			}
		}
	}
	if len(shape.cols) == 2 && shape.numeric[shape.cols[1]] && shape.rowCount > 1 &&
		shape.rowCount <= chartableRows && shape.firstDistinct {
		if looksOrderedName(shape.cols[0]) {
			return "line"
		}
		return "bar"
	}
	return "table"
}
