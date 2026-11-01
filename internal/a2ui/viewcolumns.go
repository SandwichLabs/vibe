package a2ui

import (
	"fmt"
	"strings"

	"github.com/SandwichLabs/slaw/internal/jsonenc"
)

// This file checks a validated data artifact view against the result columns
// of its queries, which are only known once the queries are bound (the agent
// vet reads them at save time). ValidateView cannot do it: it knows query
// names, not what a query returns. A name that matches no column used to
// surface only when the person opened the artifact.

// maxListedColumns bounds the column list quoted in one error.
const maxListedColumns = 20

// minChartColumns is the number of result columns a data-bound component reads
// positionally (a label column, then values): fewer and it draws nothing
// useful. Components not listed read whatever comes back.
var minChartColumns = map[string]int{
	"stat": 1, "barChart": 2, "lineChart": 2, "areaChart": 2, "columnChart": 2,
	"radarChart": 2, "ringChart": 2, "streamChart": 1, "heatmap": 3, "sankeyChart": 3,
}

// chartShape is what the positional columns of a chart mean, for the message.
var chartShape = map[string]string{
	"stat": "the number in the first column", "barChart": "a label column, then a numeric column",
	"lineChart": "a label column, then a numeric column", "areaChart": "a label column, then a numeric column per series",
	"columnChart": "a label column, then a numeric column per series", "radarChart": "a label column, then a numeric column per series",
	"ringChart": "a label column, then a value column", "streamChart": "a value (optionally after a label column)",
	"heatmap": "a row label, a column label, then a value", "sankeyChart": "source, target, then a weight",
}

// CheckViewColumns checks every column name a view uses, and the column count
// of every query-bound chart, against columns (query name to its result column
// names, in order). A query missing from columns is skipped: its result is
// not known. Each problem is one line with the component's path, the bad name
// and the columns available. p must have passed ValidateView.
func CheckViewColumns(p Page, columns map[string][]string) []string {
	var problems []string
	for i, c := range p.Components {
		query, _ := c.Fields["query"].(string)
		cols, known := columns[query]
		if query == "" || !known {
			continue
		}
		path := fmt.Sprintf("components[%d]", i)
		label := fmt.Sprintf("%s (%s %s, query %s)", path, c.Type, jsonenc.Quote(c.ID), jsonenc.Quote(query))
		if c.Type == "pivot" {
			problems = append(problems, pivotColumnProblems(c.Fields, path, label, cols)...)
		}
		if d, ok := c.Fields["drill"].(map[string]any); ok {
			if col, _ := d["column"].(string); !hasColumn(cols, col) {
				problems = append(problems, missingColumn(path+".drill.column", label, col, cols))
			}
		}
		if ap := arityProblem(c.Type, label, cols); ap != "" {
			problems = append(problems, ap)
		}
	}
	return problems
}

func pivotColumnProblems(f map[string]any, path, label string, cols []string) []string {
	var problems []string
	check := func(field string, v any) {
		if s, ok := v.(string); ok && !hasColumn(cols, s) {
			problems = append(problems, missingColumn(path+"."+field, label, s, cols))
		}
	}
	if rows, ok := f["rows"].([]any); ok {
		for i, r := range rows {
			check(fmt.Sprintf("rows[%d]", i), r)
		}
	}
	check("columns", f["columns"])
	check("value", f["value"])
	return problems
}

func missingColumn(at, label, name string, cols []string) string {
	return fmt.Sprintf("%s: %s names %s, which is not a column of the query's result. Columns: %s. "+
		"(Name a column by its alias: SELECT sum(x) AS total ... gives \"total\".)", at, label, jsonenc.Quote(name), listColumns(cols))
}

func arityProblem(typ, label string, cols []string) string {
	need, ok := minChartColumns[typ]
	if !ok || len(cols) >= need {
		return ""
	}
	return fmt.Sprintf("%s: a %s reads %s, so the query needs at least %d column(s), but it returns %d. Columns: %s",
		label, typ, chartShape[typ], need, len(cols), listColumns(cols))
}

func hasColumn(cols []string, name string) bool {
	for _, c := range cols {
		if c == name {
			return true
		}
	}
	return false
}

func listColumns(cols []string) string {
	if len(cols) == 0 {
		return "(none)"
	}
	shown := cols
	extra := ""
	if len(shown) > maxListedColumns {
		extra = fmt.Sprintf(", and %d more", len(shown)-maxListedColumns)
		shown = shown[:maxListedColumns]
	}
	return strings.Join(shown, ", ") + extra
}
