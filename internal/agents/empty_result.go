package agents

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// explainEmptyResult is ported from schoolwork-workspace's
// src/agent/empty-result.js. The incident it fixes: a model wrote
// `Attended = 'Yes'` against a column that actually held `Y`/`N`, got an
// empty result back, and told a user "nobody attended" — a false finding
// from a wrong literal guess, not a true absence of data. A zero-row result
// carries its own diagnosis: for every column the query compared against a
// string literal, the distinct values that column actually holds, so the
// model (or a person reading the tool step) can correct the query instead
// of reporting a false negative.
//
// This must run BEFORE budgetRows (an empty result is never truncated
// anyway) and must never execute anything beyond a handful of read-only,
// internally-constructed SELECT DISTINCT probes against real
// (information_schema-verified) columns — never the caller's SQL text
// itself beyond regex-extracted identifiers, and every identifier is
// quoted via session.QuoteIdent before it reaches a query string.
const emptyResultMaxDistinctValues = 40
const emptyResultMaxValuesShown = 25

var (
	literalEqRe = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_]*)\s*(?:=|<>|!=)\s*'((?:[^']|'')*)'`)
	literalInRe = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_]*)\s+IN\s*\(\s*((?:'(?:[^']|'')*'\s*,?\s*)+)\)`)
	quotedValRe = regexp.MustCompile(`'((?:[^']|'')*)'`)
)

type literalPredicate struct {
	column string
	values []string
}

// literalPredicates extracts `col = 'literal'`/`col <> 'literal'`/`col IN
// ('a','b')` comparisons from sql. Deliberately a regex over SQL text
// rather than a parser: this only ever generates an advisory hint, so a
// missed clause just means one less hint, never an incorrect one.
func literalPredicates(sqlText string) []literalPredicate {
	found := map[string]*literalPredicate{}
	order := []string{}
	add := func(col, val string) {
		key := strings.ToLower(col)
		p, ok := found[key]
		if !ok {
			p = &literalPredicate{column: col}
			found[key] = p
			order = append(order, key)
		}
		unescaped := strings.ReplaceAll(val, "''", "'")
		for _, v := range p.values {
			if v == unescaped {
				return
			}
		}
		p.values = append(p.values, unescaped)
	}

	for _, m := range literalEqRe.FindAllStringSubmatch(sqlText, -1) {
		add(m[1], m[2])
	}
	for _, m := range literalInRe.FindAllStringSubmatch(sqlText, -1) {
		for _, vm := range quotedValRe.FindAllStringSubmatch(m[2], -1) {
			add(m[1], vm[1])
		}
	}

	out := make([]literalPredicate, 0, len(order))
	for _, k := range order {
		out = append(out, *found[k])
	}
	return out
}

// explainEmptyResult returns advisory text to append to a zero-row
// run_query response, or "" when there is nothing useful to say (no literal
// comparisons found, every literal is actually present, or a column can't
// be resolved). userDB is queried read-only, using only
// information_schema-verified table/column names, quoted via
// session.QuoteIdent.
func explainEmptyResult(ctx context.Context, userDB *sql.DB, sqlText string) string {
	preds := literalPredicates(sqlText)
	if len(preds) == 0 || userDB == nil {
		return ""
	}

	var notes []string
	for _, pred := range preds {
		table, ok := resolveColumnTable(ctx, userDB, pred.column)
		if !ok {
			continue
		}
		actual, ok := distinctValues(ctx, userDB, table, pred.column)
		if !ok || len(actual) == 0 {
			continue
		}
		have := map[string]bool{}
		for _, v := range actual {
			have[v] = true
		}
		var missed []string
		for _, v := range pred.values {
			if !have[v] {
				missed = append(missed, v)
			}
		}
		if len(missed) == 0 {
			continue
		}
		shownN := len(actual)
		if shownN > emptyResultMaxValuesShown {
			shownN = emptyResultMaxValuesShown
		}
		shown := make([]string, shownN)
		for i := 0; i < shownN; i++ {
			shown[i] = fmt.Sprintf("'%s'", actual[i])
		}
		more := ""
		if len(actual) > emptyResultMaxValuesShown {
			more = fmt.Sprintf(", … (%d distinct)", len(actual))
		}
		missedQuoted := make([]string, len(missed))
		for i, v := range missed {
			missedQuoted[i] = fmt.Sprintf("'%s'", v)
		}
		notes = append(notes, fmt.Sprintf("  %s: you compared against %s, but this column holds %s%s",
			pred.column, strings.Join(missedQuoted, ", "), strings.Join(shown, ", "), more))
	}
	if len(notes) == 0 {
		return ""
	}
	return fmt.Sprintf("This query matched 0 rows, and at least one literal compared against does not appear in the data:\n%s\nRe-run with a value the column actually holds before drawing any conclusion. Do not report \"none\" or \"never\" as a finding based on this empty result.",
		strings.Join(notes, "\n"))
}

// resolveColumnTable finds which table (in the default catalog's default
// schema) has a column with this name, case-insensitively. Returns ok=false when zero or
// ambiguous matches are found — better to say nothing than guess wrong.
func resolveColumnTable(ctx context.Context, db *sql.DB, column string) (string, bool) {
	rows, err := db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.columns WHERE table_catalog = current_database() AND table_schema = current_schema() AND lower(column_name) = lower($1) LIMIT 2",
		column)
	if err != nil {
		return "", false
	}
	defer rows.Close()

	var table string
	n := 0
	for rows.Next() {
		if err := rows.Scan(&table); err != nil {
			return "", false
		}
		n++
	}
	if n != 1 {
		return "", false
	}
	return table, true
}

// distinctValues returns the distinct string values a column holds, or
// ok=false when the column has too many distinct values to be a useful
// hint (an id or free-text column) or the probe query fails.
func distinctValues(ctx context.Context, db *sql.DB, table, column string) ([]string, bool) {
	qCol := session.QuoteIdent(column)
	qTable := session.QuoteIdent(table)

	var n int
	err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(DISTINCT %s) FROM %s", qCol, qTable)).Scan(&n)
	if err != nil || n == 0 || n > emptyResultMaxDistinctValues {
		return nil, false
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		"SELECT DISTINCT CAST(%s AS VARCHAR) FROM %s WHERE %s IS NOT NULL ORDER BY 1 LIMIT %d",
		qCol, qTable, qCol, emptyResultMaxDistinctValues))
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}
