package crud

// Structured filter (QBE) → SQL. This file is the ONE WHERE builder behind the
// table browser (parameterized: values bound, run at once), the saved query
// and the agent's build_query tool (inline: values rendered as literals into a
// standalone SQL string). Both modes share the operator table, combinator
// handling and column allowlist, and differ only in how a value is emitted.
//
// Safety rules: every column must be in the caller's allowlist of the table's
// real columns and is rendered through session.QuoteIdent; an operator must be
// in the table below; a value is either a bound parameter or a
// session.QuoteLiteral / numeric literal. Nothing from a spec is ever
// concatenated raw.

import (
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// FilterCondition is one structured QBE filter clause. Op is one of
// eq/neq/lt/gt/lte/gte/like/is_null/is_not_null; Value is ignored for the
// is_null/is_not_null operators.
type FilterCondition struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// FilterSpec is a set of FilterConditions joined by a single combinator
// ("AND" or "OR"; defaults to AND).
type FilterSpec struct {
	Combinator string            `json:"combinator"`
	Conditions []FilterCondition `json:"conditions"`
}

// filterOps maps a FilterCondition.Op to its SQL comparison operator. Value-less
// operators (is_null/is_not_null) are handled separately.
var filterOps = map[string]string{
	"eq":   "=",
	"neq":  "<>",
	"lt":   "<",
	"gt":   ">",
	"lte":  "<=",
	"gte":  ">=",
	"like": "LIKE",
}

// BuildWhere translates spec into a parameterized WHERE clause (leading
// " WHERE ...", or "" when there are no conditions) and its bound args. known
// is the allowlist of the table's real column names.
func BuildWhere(spec FilterSpec, known map[string]bool) (string, []any, error) {
	var args []any
	clause, err := buildWhere(spec, known, func(v any) string {
		args = append(args, v)
		return "?"
	})
	if err != nil {
		return "", nil, err
	}
	return clause, args, nil
}

// BuildWhereInline is BuildWhere with the values rendered as SQL literals, for
// a standalone statement that is stored or shown rather than run with args.
func BuildWhereInline(spec FilterSpec, known map[string]bool) (string, error) {
	return buildWhere(spec, known, sqlLiteral)
}

// buildWhere is the single clause loop. value turns a condition value into the
// SQL text that stands for it (a placeholder or a literal).
func buildWhere(spec FilterSpec, known map[string]bool, value func(any) string) (string, error) {
	if len(spec.Conditions) == 0 {
		return "", nil
	}
	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	clauses := make([]string, 0, len(spec.Conditions))
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", fmt.Errorf("unknown filter column %q", c.Column)
		}
		col := session.QuoteIdent(c.Column)
		switch c.Op {
		case "is_null":
			clauses = append(clauses, col+" IS NULL")
		case "is_not_null":
			clauses = append(clauses, col+" IS NOT NULL")
		default:
			sqlOp, ok := filterOps[c.Op]
			if !ok {
				return "", fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, col+" "+sqlOp+" "+value(c.Value))
		}
	}
	return " WHERE " + strings.Join(clauses, " "+combinator+" "), nil
}

// BuildFilterSQL renders a FilterSpec (plus optional selected projection columns)
// into a deterministic standalone SQL string over the given table name. known is
// the allowlist of the table's real column names; every filter/projection
// identifier must be present in it or an error is returned.
func BuildFilterSQL(table string, spec FilterSpec, columns []string, known map[string]bool) (string, error) {
	return BuildFilterSQLFrom(session.QuoteIdent(table), spec, columns, known)
}

// BuildFilterSQLFrom is BuildFilterSQL over a pre-rendered FROM target (for
// example a catalog-qualified table reference), which the caller has already
// quoted.
func BuildFilterSQLFrom(from string, spec FilterSpec, columns []string, known map[string]bool) (string, error) {
	projection := "*"
	if len(columns) > 0 {
		parts := make([]string, 0, len(columns))
		for _, col := range columns {
			if !known[col] {
				return "", fmt.Errorf("unknown selected column %q", col)
			}
			parts = append(parts, session.QuoteIdent(col))
		}
		projection = strings.Join(parts, ", ")
	}
	where, err := BuildWhereInline(spec, known)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SELECT %s FROM %s%s", projection, from, where), nil
}

// sqlLiteral renders a FilterCondition value as an inline SQL literal. Numeric
// and boolean values render bare; everything else (including string-typed numeric
// inputs from the all-string form path) renders as a quoted string literal,
// which DuckDB coerces to the column type on comparison.
func sqlLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", t)
	case float32, float64:
		return fmt.Sprintf("%v", t)
	case string:
		return session.QuoteLiteral(t)
	default:
		return session.QuoteLiteral(fmt.Sprintf("%v", t))
	}
}
