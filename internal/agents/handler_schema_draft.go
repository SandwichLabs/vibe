package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/SandwichLabs/vibe/internal/queries"
)

// --- Schema Builder draft + saved-SQL query handlers (ported from
// internal/llm, #63) ---

// handleProposeSchema validates a draft schema and renders it as an inline
// A2UI page (a heading + a column table per proposed table, plus a
// relationships table) the same way present_data does — there is no
// dedicated Schema Builder diagram screen. It is intentionally a no-op
// against the database: the model presents this draft in chat, the user
// confirms in a chat reply (not a UI button), and only then does the model
// call create_table once per table (parents before children, FKs declared
// inline) to actually build it.
func (h *Handler) handleProposeSchema(_ context.Context, req *Request) *Response {
	tables, _ := req.Params["tables"].([]any)
	if len(tables) == 0 {
		return NewErrorResponse(req.ID, ErrInvalidParams, "propose_schema needs at least one table")
	}
	relationships, _ := req.Params["relationships"].([]any)

	components := make([]any, 0, 1+2*len(tables))
	nextID := 0
	newID := func(prefix string) string {
		nextID++
		return fmt.Sprintf("%s%d", prefix, nextID)
	}

	// title becomes the a2ui.Page's own "title" field (rendered by the
	// panel as the card's heading) — it must NOT also be emitted as an h1
	// heading component, or the draft's title renders twice (review fix).
	title := "Proposed schema"
	if note := strings.TrimSpace(stringParam(req.Params["note"])); note != "" {
		title = note
	}

	for _, t := range tables {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name := stringParam(m["name"])
		components = append(components, map[string]any{"id": newID("h"), "type": "heading", "text": name, "level": float64(2)})

		cols, _ := m["columns"].([]any)
		rows := make([]any, 0, len(cols))
		for _, c := range cols {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			var constraints []string
			if b, _ := cm["primary_key"].(bool); b {
				constraints = append(constraints, "PRIMARY KEY")
			}
			if b, _ := cm["not_null"].(bool); b {
				constraints = append(constraints, "NOT NULL")
			}
			rows = append(rows, []any{stringParam(cm["name"]), stringParam(cm["type"]), strings.Join(constraints, ", ")})
		}
		components = append(components, map[string]any{
			"id": newID("t"), "type": "table",
			"columns": []any{"Column", "Type", "Constraints"},
			"rows":    rows,
		})
	}

	if len(relationships) > 0 {
		components = append(components, map[string]any{"id": newID("h"), "type": "heading", "text": "Relationships", "level": float64(2)})
		rows := make([]any, 0, len(relationships))
		for _, r := range relationships {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			optional := "required"
			if b, _ := rm["optional"].(bool); b {
				optional = "optional"
			}
			rows = append(rows, []any{
				stringParam(rm["child_table"]), stringParam(rm["child_column"]),
				stringParam(rm["parent_table"]), stringParam(rm["parent_column"]), optional,
			})
		}
		components = append(components, map[string]any{
			"id": newID("t"), "type": "table",
			"columns": []any{"Child table", "Child column", "Parent table", "Parent column", "Link"},
			"rows":    rows,
		})
	}

	raw := map[string]any{"title": title, "components": components}
	page, err := a2ui.Validate(raw)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to render schema draft: %v", err))
	}

	h.logger.Info("proposed schema draft", "tables", len(tables), "relationships", len(relationships))
	return NewResponse(req.ID, page)
}

// handleSaveQuery persists a named, already-written SQL string to the
// query store (queries/*.sql in a workspace, _vibe_queries in a .vibe; studio's SaveQueryOn binding does the same thing).
// Unlike build_query it does not render SQL from QBE conditions.
func (h *Handler) handleSaveQuery(ctx context.Context, req *Request) *Response {
	title, _ := req.Params["title"].(string)
	sqlStr, _ := req.Params["sql"].(string)

	q := &queries.Query{
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := h.access.Queries().Create(ctx, q); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to save query: %v", err))
	}
	return NewResponse(req.ID, map[string]any{"id": q.ID, "title": title, "saved": true})
}
