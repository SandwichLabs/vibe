package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/crud"
	"github.com/SandwichLabs/vibe/internal/session"
)

// --- Schema handlers ---

func (h *Handler) handleCreateTable(ctx context.Context, req *Request) *Response {
	name, _ := req.Params["name"].(string)
	columns, ok := req.Params["columns"].([]any)
	if !ok {
		return NewErrorResponse(req.ID, ErrInvalidParams, "columns must be an array")
	}

	var colDefs []string
	for _, col := range columns {
		colMap, ok := col.(map[string]any)
		if !ok {
			return NewErrorResponse(req.ID, ErrInvalidParams, "each column must be an object with name and type")
		}
		colName := fmt.Sprintf("%v", colMap["name"])
		colType := fmt.Sprintf("%v", colMap["type"])
		if !crud.ValidColumnType(colType) {
			return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid column type %q", colType))
		}
		colDefs = append(colDefs, fmt.Sprintf("%s %s", session.QuoteIdent(colName), colType))
	}

	tblSQL, tblErr := h.tableSQL(ctx, name)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	query := fmt.Sprintf("CREATE TABLE %s (%s)", tblSQL, strings.Join(colDefs, ", "))
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to create table: %v", err))
	}
	return NewResponse(req.ID, map[string]any{"table": name, "created": true})
}

func (h *Handler) handleAddColumn(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	name, _ := req.Params["name"].(string)
	colType, _ := req.Params["type"].(string)
	if !crud.ValidColumnType(colType) {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid column type %q", colType))
	}

	tblSQL, tblErr := h.tableSQL(ctx, table)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tblSQL, session.QuoteIdent(name), colType)
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to add column: %v", err))
	}
	return NewResponse(req.ID, map[string]any{"table": table, "column": name, "added": true})
}

func (h *Handler) handleDropTable(ctx context.Context, req *Request) *Response {
	name, _ := req.Params["name"].(string)

	tblSQL, tblErr := h.tableSQL(ctx, name)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", tblSQL)
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to drop table: %v", err))
	}
	return NewResponse(req.ID, map[string]any{"table": name, "dropped": true})
}

func (h *Handler) handleImportCSV(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	data, _ := req.Params["data"].(string)
	hasHeader := true
	if h, ok := req.Params["has_header"].(bool); ok {
		hasHeader = h
	}

	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) == 0 {
		return NewErrorResponse(req.ID, ErrInvalidParams, "CSV data is empty")
	}

	startIdx := 0
	if hasHeader {
		startIdx = 1
	}

	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to begin transaction: %v", err))
	}

	var inserted int64
	for i := startIdx; i < len(lines); i++ {
		fields := strings.Split(lines[i], ",")
		placeholders := make([]string, len(fields))
		args := make([]any, len(fields))
		for j, f := range fields {
			placeholders[j] = "?"
			args[j] = strings.TrimSpace(f)
		}
		tblSQL, tblErr := h.tableSQL(ctx, table)
		if tblErr != nil {
			return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
		}
		query := fmt.Sprintf("INSERT INTO %s VALUES (%s)", tblSQL, strings.Join(placeholders, ", "))
		_, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			_ = tx.Rollback()
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to import row %d: %v", i, err))
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to commit import: %v", err))
	}

	return NewResponse(req.ID, map[string]any{"table": table, "rows_imported": inserted})
}

// stringParam coerces an any value to a string, returning "" for nil.
func stringParam(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
