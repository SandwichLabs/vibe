package agents

// Agent Query Wizard tool (plan §2.4, §4.3): "AI-as-Wizard" visual querying.
//
//   - build_query -> reuses the shared QBE renderer (queries.BuildFilterSQL)
//     and persists a named query to _vibe_queries.

import (
	"context"
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// handleBuildQuery renders a QBE filter spec into deterministic SQL over the
// bound table and persists it as a named query in the registry.
func (h *Handler) handleBuildQuery(ctx context.Context, req *Request) *Response {
	name := stringParam(req.Params["name"])
	table := stringParam(req.Params["table"])
	if !engine.ValidIdentifier(table) {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid table name %q", table))
	}

	spec := queries.FilterSpec{
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
	for _, c := range cols {
		known[c.Name] = true
	}

	sqlStr, err := queries.BuildFilterSQL(table, spec, columns, known)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("failed to build query: %v", err))
	}

	q := &queries.Query{
		Title:      name,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := h.access.Queries().Create(ctx, q); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to save query: %v", err))
	}

	h.logger.Info("built query", "id", q.ID, "name", name, "table", table)
	return NewResponse(req.ID, map[string]any{
		"id":      q.ID,
		"name":    name,
		"table":   table,
		"sql":     sqlStr,
		"created": true,
	})
}

// extractFilterConditions converts a params list ([]any of {column, op, value})
// into queries.FilterCondition entries.
func extractFilterConditions(raw any) []queries.FilterCondition {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]queries.FilterCondition, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, queries.FilterCondition{
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
