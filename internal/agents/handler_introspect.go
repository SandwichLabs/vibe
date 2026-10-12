package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/session"
)

// --- Introspect handlers ---

func (h *Handler) handleIntrospectSchema(ctx context.Context, req *Request) *Response {
	sess, errResp := h.sess(req.ID)
	if errResp != nil {
		return errResp
	}
	// Optional "connection": introspect that catalog instead of the default.
	// Tables of a non-default catalog come back qualified (catalog.schema.table)
	// so the agent can use them verbatim in every other tool.
	connection, _ := req.Params["connection"].(string)
	catalog := connection
	if catalog == "" {
		catalog = sess.DefaultCatalog()
	}
	tabs, err := h.listCatalogTables(ctx, sess, catalog)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to query schema: %v", err))
	}
	tables := make([]string, 0, len(tabs))
	for _, t := range tabs {
		if catalog == sess.DefaultCatalog() {
			tables = append(tables, t.Name)
		} else {
			tables = append(tables, t.Ref().String())
		}
	}
	return NewResponse(req.ID, map[string]any{"tables": tables})
}

// listCatalogTables lists the tables of catalog's default schema.
func (h *Handler) listCatalogTables(ctx context.Context, sess session.Session, catalog string) ([]session.TableInfo, error) {
	return sess.ListTables(ctx, catalog, "")
}

func (h *Handler) handleDescribeTable(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	sess, errResp := h.sess(req.ID)
	if errResp != nil {
		return errResp
	}

	// table may be qualified (catalog.schema.table); an unqualified name
	// resolves against the default catalog.
	desc, err := sess.DescribeTable(ctx, table)
	if err != nil {
		// A missing table was an empty column list before; keep that shape.
		if errors.Is(err, session.ErrNoSuchTable) {
			return NewResponse(req.ID, map[string]any{"table": table, "columns": []map[string]any(nil)})
		}
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to describe table: %v", err))
	}

	var columns []map[string]any
	for _, c := range desc.Columns {
		columns = append(columns, map[string]any{
			"name":     c.Name,
			"type":     c.DataType,
			"not_null": c.NotNull,
			"pk":       false,
		})
	}
	return NewResponse(req.ID, map[string]any{"table": table, "columns": columns})
}

func (h *Handler) handleListQueries(ctx context.Context, req *Request) *Response {
	list, err := h.access.Queries().List(ctx)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to list queries: %v", err))
	}
	var queries []map[string]any
	for _, q := range list {
		queries = append(queries, map[string]any{
			"name":   q.Title,
			"sql":    q.SQL,
			"engine": q.Engine,
		})
	}
	return NewResponse(req.ID, map[string]any{"queries": queries})
}

func (h *Handler) handleHelpSearch(_ context.Context, req *Request) *Response {
	query := ""
	if q, ok := req.Params["query"].(string); ok {
		query = q
	}

	entries := SearchHelp(query)

	results := make([]map[string]any, len(entries))
	for i, e := range entries {
		results[i] = map[string]any{
			"topic":   e.Topic,
			"title":   e.Title,
			"content": e.Content,
		}
	}
	return NewResponse(req.ID, map[string]any{"results": results, "count": len(results)})
}
