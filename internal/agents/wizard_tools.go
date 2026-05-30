package agents

// Agent Schema/Query Wizard tools (plan §2.1/§2.4, §4.3). These are the
// "AI-as-Wizard" surface for relationships and visual querying:
//
//   - create_relationship  -> reuses the shared FK-emit path (schema.ApplyForeignKey)
//                             and persists a row to _vibe_relationships.
//   - list_relationships   -> reads back _vibe_relationships.
//   - build_query          -> reuses the shared QBE renderer (queries.BuildFilterSQL)
//                             and persists a named query to _vibe_queries.

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/sandwich-labs/vibe-app/internal/schema"
)

// handleCreateRelationship applies a real DuckDB foreign key for the requested
// link and persists the relationship metadata to _vibe_relationships.
func (h *Handler) handleCreateRelationship(_ context.Context, req *Request) *Response {
	spec := schema.ForeignKeySpec{
		ParentTable:  stringParam(req.Params["parent_table"]),
		ParentColumn: stringParam(req.Params["parent_column"]),
		ChildTable:   stringParam(req.Params["child_table"]),
		ChildColumn:  stringParam(req.Params["child_column"]),
		OnDelete:     stringParam(req.Params["on_delete"]),
		OnUpdate:     stringParam(req.Params["on_update"]),
	}

	ddb, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}

	if err := schema.ApplyForeignKey(ddb, spec); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to create relationship: %v", err))
	}

	rel := schema.Relationship{
		ID:               uuid.New().String(),
		ParentTable:      spec.ParentTable,
		ParentColumn:     spec.ParentColumn,
		ChildTable:       spec.ChildTable,
		ChildColumn:      spec.ChildColumn,
		OnDelete:         schema.NormalizeAction(spec.OnDelete),
		OnUpdate:         schema.NormalizeAction(spec.OnUpdate),
		EnforceIntegrity: true,
	}
	store := schema.NewRelationshipStore(h.container.SQLiteDB())
	if err := store.Save(rel); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to persist relationship metadata: %v", err))
	}

	h.logger.Info("created relationship",
		"id", rel.ID,
		"parent_table", rel.ParentTable, "parent_column", rel.ParentColumn,
		"child_table", rel.ChildTable, "child_column", rel.ChildColumn)

	return NewResponse(req.ID, map[string]any{
		"id":            rel.ID,
		"parent_table":  rel.ParentTable,
		"parent_column": rel.ParentColumn,
		"child_table":   rel.ChildTable,
		"child_column":  rel.ChildColumn,
		"created":       true,
	})
}

// handleListRelationships reads back every stored relationship definition.
func (h *Handler) handleListRelationships(_ context.Context, req *Request) *Response {
	store := schema.NewRelationshipStore(h.container.SQLiteDB())
	rels, err := store.List()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to list relationships: %v", err))
	}

	out := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		out = append(out, map[string]any{
			"id":                r.ID,
			"parent_table":      r.ParentTable,
			"parent_column":     r.ParentColumn,
			"child_table":       r.ChildTable,
			"child_column":      r.ChildColumn,
			"on_delete":         r.OnDelete,
			"on_update":         r.OnUpdate,
			"enforce_integrity": r.EnforceIntegrity,
		})
	}
	return NewResponse(req.ID, map[string]any{"relationships": out})
}

// handleBuildQuery renders a QBE filter spec into deterministic SQL over the
// bound table and persists it as a named query in the registry.
func (h *Handler) handleBuildQuery(ctx context.Context, req *Request) *Response {
	name := stringParam(req.Params["name"])
	table := stringParam(req.Params["table"])
	if !schema.ValidIdentifier(table) {
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

	cols, err := schema.TableColumns(ddb, table)
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

	registry := queries.NewRegistry(h.container.SQLiteDB(), h.logger)
	q := &queries.Query{
		ID:         uuid.New().String(),
		Title:      name,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := registry.Create(ctx, q); err != nil {
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

// handleCreateReport is the Report Wizard tool: it assembles an Access-style
// banded report spec (grouping/sort/detail/aggregates) and persists it to
// _vibe_reports via the shared reports.ReportStore (plan §3.4 / §4.3).
func (h *Handler) handleCreateReport(_ context.Context, req *Request) *Response {
	id := stringParam(req.Params["id"])
	if id == "" {
		id = uuid.New().String()
	}

	spec := reports.ReportSpec{
		ID:     id,
		Title:  stringParam(req.Params["title"]),
		Source: stringParam(req.Params["source"]),
		Groups: extractGroupLevels(req.Params["group_by"]),
		Sort:   extractSortSpecs(req.Params["sort"]),
		Detail: reports.DetailBand{
			Columns: extractReportColumns(req.Params["columns"]),
		},
		GroupFooterAggregates:  extractAggregates(req.Params["aggregates"]),
		ReportFooterAggregates: extractAggregates(req.Params["report_aggregates"]),
	}

	store := reports.NewReportStore(h.container.SQLiteDB())
	if err := store.Save(spec); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to persist report: %v", err))
	}

	h.logger.Info("created report", "id", spec.ID, "title", spec.Title, "source", spec.Source)
	return NewResponse(req.ID, map[string]any{
		"id":      spec.ID,
		"title":   spec.Title,
		"source":  spec.Source,
		"created": true,
	})
}

// extractGroupLevels converts a params list ([]any of {field, sort_dir}) into
// reports.GroupLevel entries.
func extractGroupLevels(raw any) []reports.GroupLevel {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]reports.GroupLevel, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		field := stringParam(m["field"])
		if field == "" {
			continue
		}
		out = append(out, reports.GroupLevel{
			Field:   field,
			SortDir: stringParam(m["sort_dir"]),
		})
	}
	return out
}

// extractSortSpecs converts a params list ([]any of {field, dir}) into
// reports.SortSpec entries.
func extractSortSpecs(raw any) []reports.SortSpec {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]reports.SortSpec, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		field := stringParam(m["field"])
		if field == "" {
			continue
		}
		out = append(out, reports.SortSpec{
			Field: field,
			Dir:   stringParam(m["dir"]),
		})
	}
	return out
}

// extractReportColumns converts a params list ([]any of {field, caption}) into
// reports.ReportColumn entries.
func extractReportColumns(raw any) []reports.ReportColumn {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]reports.ReportColumn, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		field := stringParam(m["field"])
		if field == "" {
			continue
		}
		out = append(out, reports.ReportColumn{
			Field:   field,
			Caption: stringParam(m["caption"]),
		})
	}
	return out
}

// extractAggregates converts a params list ([]any of {name, func, field}) into
// reports.Aggregate entries.
func extractAggregates(raw any) []reports.Aggregate {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]reports.Aggregate, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringParam(m["name"])
		if name == "" {
			continue
		}
		out = append(out, reports.Aggregate{
			Name:  name,
			Func:  reports.AggFunc(stringParam(m["func"])),
			Field: stringParam(m["field"]),
		})
	}
	return out
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
