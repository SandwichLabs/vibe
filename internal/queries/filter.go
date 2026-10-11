package queries

// QBE (Query-By-Example) filter → SQL rendering (plan §2.3/§2.4/§2.6). This is
// the single deterministic FilterSpec → standalone-SQL renderer shared by the
// Studio backend (studio/app.go SaveFilterAsQuery) and the
// agent Query Wizard tool (internal/agents build_query), so the SQL previewed,
// saved, and run by the agent are byte-for-byte identical.
//
// Unlike a parameterized execution path, a saved/named query is a standalone SQL
// string, so literal values are safely inlined: identifiers (filter columns and
// the optional projection) are validated against the table's real columns, and
// string literals are single-quote escaped so a value can never break out of the
// literal.

import (
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
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

// SQLOperator maps a FilterCondition.Op to its SQL comparison operator. The
// value-less is_null / is_not_null operators are not in the table; callers
// render them directly. It is exported so the parameterized WHERE builder in
// Studio shares the one operator table.
func SQLOperator(op string) (string, bool) {
	sqlOp, ok := filterOps[op]
	return sqlOp, ok
}

// BuildFilterSQL renders a FilterSpec (plus optional selected projection columns)
// into a deterministic standalone SQL string over the given table. known is the
// allowlist of the table's real column names; every filter/projection identifier
// must be present in it or an error is returned (no SQL injection via identifier
// or value).
func BuildFilterSQL(table string, spec FilterSpec, columns []string, known map[string]bool) (string, error) {
	return BuildFilterSQLFrom(engine.QuoteIdent(table), spec, columns, known)
}

// BuildFilterSQLFrom is BuildFilterSQL over a pre-rendered FROM target (for
// example a catalog-qualified table reference), which the caller has already
// quoted. Everything else (column allowlist, quoting, inlined literals) is the
// same single rendering path.
func BuildFilterSQLFrom(from string, spec FilterSpec, columns []string, known map[string]bool) (string, error) {
	projection := "*"
	if len(columns) > 0 {
		parts := make([]string, 0, len(columns))
		for _, col := range columns {
			if !known[col] {
				return "", fmt.Errorf("unknown selected column %q", col)
			}
			parts = append(parts, engine.QuoteIdent(col))
		}
		projection = strings.Join(parts, ", ")
	}

	whereClause, err := buildFilterWhereInline(spec, known)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT %s FROM %s%s", projection, from, whereClause), nil
}

// buildFilterWhereInline renders a FilterSpec into a SQL WHERE clause (leading
// " WHERE ...", or "" when there are no conditions) with literal values inlined.
func buildFilterWhereInline(spec FilterSpec, known map[string]bool) (string, error) {
	if len(spec.Conditions) == 0 {
		return "", nil
	}

	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	var clauses []string
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", fmt.Errorf("unknown filter column %q", c.Column)
		}
		switch c.Op {
		case "is_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NULL", engine.QuoteIdent(c.Column)))
		case "is_not_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NOT NULL", engine.QuoteIdent(c.Column)))
		default:
			sqlOp, ok := filterOps[c.Op]
			if !ok {
				return "", fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, fmt.Sprintf("%s %s %s", engine.QuoteIdent(c.Column), sqlOp, sqlLiteral(c.Value)))
		}
	}

	return " WHERE " + strings.Join(clauses, " "+combinator+" "), nil
}

// sqlLiteral renders a FilterCondition value as a safe inline SQL literal. Numeric
// and boolean values render bare; everything else (including string-typed numeric
// inputs from the all-string form path) renders as a single-quote-escaped string
// literal, which DuckDB coerces to the column type on comparison.
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
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", t), "'", "''") + "'"
	}
}
