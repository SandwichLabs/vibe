package agents

// Agent Query Wizard tool (plan §2.4, §4.3): "AI-as-Wizard" visual querying.
//
//   - build_query -> reuses the shared QBE renderer (crud.BuildFilterSQL)
//     and saves a named query (queries/*.sql) through the save_query path.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/SandwichLabs/assay/internal/crud"
	"github.com/SandwichLabs/assay/internal/engine"
	"github.com/SandwichLabs/assay/internal/queries"
	"github.com/SandwichLabs/assay/internal/session"
)

// handleBuildQuery renders a QBE filter spec into deterministic SQL over the
// bound table and saves it as a named query, through the same vet, duplicate
// check and dry run as save_query.
func (h *Handler) handleBuildQuery(ctx context.Context, req *Request) *Response {
	name := stringParam(req.Params["name"])
	table := stringParam(req.Params["table"])
	if !engine.ValidIdentifier(table) {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid table name %q", table))
	}

	spec := crud.FilterSpec{
		Combinator: stringParam(req.Params["combinator"]),
		Conditions: extractFilterConditions(req.Params["conditions"]),
	}
	columns := extractStringList(req.Params["columns"])

	ddb, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}

	cols, err := engine.TableColumns(ddb, session.TableRef{Name: table})
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to introspect columns for query: %v", err))
	}
	known := make(map[string]bool, len(cols))
	types := make(map[string]string, len(cols))
	for _, c := range cols {
		known[c.Name] = true
		types[c.Name] = c.Type
	}
	if spec.Conditions, err = typeConditionValues(spec.Conditions, types); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}

	sqlStr, err := crud.BuildFilterSQL(table, spec, columns, known)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("failed to build query: %v", err))
	}

	save := savedQuerySpec{
		id:  strings.TrimSpace(stringParam(req.Params["id"])),
		rev: stringParam(req.Params["rev"]),
		q:   queries.Query{Title: strings.TrimSpace(name), SQL: sqlStr, Engine: "duckdb", Parameters: []queries.Param{}},
	}
	resp := h.storeSavedQuery(ctx, req, save, "name")
	if resp.Error != nil || isDryRun(ctx) {
		return resp
	}
	if m, ok := resp.Result.(map[string]any); ok {
		m["name"], m["table"], m["sql"] = name, table, sqlStr
	}
	return resp
}

// typeConditionValues makes each comparison value the type of its column, so a
// numeric column is compared with a number and a boolean one with true/false
// rather than with a quoted string: "15" against an INTEGER column becomes 15.
// A value that cannot be that type is refused by name. Strings stay strings for
// text, date and every other type (the database coerces '2026-01-31' itself),
// and like is always textual.
func typeConditionValues(conds []crud.FilterCondition, types map[string]string) ([]crud.FilterCondition, error) {
	out := make([]crud.FilterCondition, len(conds))
	for i, c := range conds {
		out[i] = c
		s, isStr := c.Value.(string)
		if !isStr || c.Op == "like" || c.Op == "is_null" || c.Op == "is_not_null" {
			continue
		}
		typ := strings.ToUpper(types[c.Column])
		switch {
		case isNumericType(typ):
			n, ok := parseNumber(strings.TrimSpace(s))
			if !ok {
				return nil, fmt.Errorf("column %q is %s but the value %q is not a number", c.Column, types[c.Column], s)
			}
			out[i].Value = n
		case typ == "BOOLEAN":
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "true":
				out[i].Value = true
			case "false":
				out[i].Value = false
			default:
				return nil, fmt.Errorf("column %q is BOOLEAN but the value %q is not true or false", c.Column, s)
			}
		}
	}
	return out, nil
}

var numericTypePrefixes = []string{"TINYINT", "SMALLINT", "INTEGER", "BIGINT", "HUGEINT", "UTINYINT", "USMALLINT", "UINTEGER", "UBIGINT", "UHUGEINT", "INT", "FLOAT", "DOUBLE", "REAL", "DECIMAL", "NUMERIC"}

func isNumericType(typ string) bool {
	for _, p := range numericTypePrefixes {
		if typ == p || strings.HasPrefix(typ, p+"(") {
			return true
		}
	}
	return false
}

// parseNumber accepts a plain decimal and returns it as json.Number, which the
// filter renders as the same text: no digit is lost to a float64 (a DECIMAL or
// HUGEINT column compares exactly).
func parseNumber(s string) (any, bool) {
	if !decimalLiteral.MatchString(s) {
		return nil, false
	}
	return json.Number(s), true
}

var decimalLiteral = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// extractFilterConditions converts a params list ([]any of {column, op, value})
// into crud.FilterCondition entries.
func extractFilterConditions(raw any) []crud.FilterCondition {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]crud.FilterCondition, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, crud.FilterCondition{
			Column: stringParam(m["column"]),
			Op:     stringParam(m["op"]),
			Value:  m["value"],
		})
	}
	return out
}

// extractStringList converts a params list ([]any of strings) into a []string.
func extractStringList(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		out = append(out, stringParam(item))
	}
	return out
}
