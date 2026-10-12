package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

func (h *Handler) handleCreateRecord(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	values, _ := req.Params["values"].(map[string]any)

	cols := make([]string, 0, len(values))
	vals := make([]any, 0, len(values))
	for k, v := range values {
		cols = append(cols, session.QuoteIdent(k))
		vals = append(vals, v)
	}
	placeholders, params := namedPlaceholders("v", vals)

	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	tblSQL, tblErr := h.tableSQL(ctx, table)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tblSQL, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	if err := h.requireWriteStatement(ctx, db, query, params); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}
	boundSQL, args, err := engine.BindParams(query, params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to bind parameters: %v", err))
	}
	result, err := db.ExecContext(ctx, boundSQL, args...)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to create record: %v", err))
	}

	lastID, _ := result.LastInsertId()
	return NewResponse(req.ID, map[string]any{"last_insert_id": lastID})
}

func (h *Handler) handleUpdateRecord(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	values, _ := req.Params["values"].(map[string]any)
	where, _ := req.Params["where"].(string)
	if strings.TrimSpace(where) == "" {
		return NewErrorResponse(req.ID, ErrInvalidParams, "where is required")
	}

	cols := make([]string, 0, len(values))
	vals := make([]any, 0, len(values))
	for k, v := range values {
		cols = append(cols, session.QuoteIdent(k))
		vals = append(vals, v)
	}
	placeholders, params := namedPlaceholders("v", vals)
	setClauses := make([]string, len(cols))
	for i, c := range cols {
		setClauses[i] = fmt.Sprintf("%s = %s", c, placeholders[i])
	}

	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	tblSQL, tblErr := h.tableSQL(ctx, table)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tblSQL, strings.Join(setClauses, ", "), where)
	if err := h.requireWriteStatement(ctx, db, query, params); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}
	boundSQL, args, err := engine.BindParams(query, params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to bind parameters: %v", err))
	}
	result, err := db.ExecContext(ctx, boundSQL, args...)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to update record: %v", err))
	}

	affected, _ := result.RowsAffected()
	return NewResponse(req.ID, map[string]any{"rows_affected": affected})
}

func (h *Handler) handleDeleteRecord(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	where, _ := req.Params["where"].(string)
	if strings.TrimSpace(where) == "" {
		return NewErrorResponse(req.ID, ErrInvalidParams, "where is required")
	}

	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	tblSQL, tblErr := h.tableSQL(ctx, table)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", tblSQL, where)
	if err := h.requireWriteStatement(ctx, db, query, nil); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}
	result, err := db.ExecContext(ctx, query)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to delete record: %v", err))
	}

	affected, _ := result.RowsAffected()
	return NewResponse(req.ID, map[string]any{"rows_affected": affected})
}

func (h *Handler) handleBulkInsert(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	columns, ok := req.Params["columns"].([]any)
	if !ok {
		return NewErrorResponse(req.ID, ErrInvalidParams, "columns must be an array of strings")
	}
	rows, ok := req.Params["rows"].([]any)
	if !ok {
		return NewErrorResponse(req.ID, ErrInvalidParams, "rows must be an array of arrays")
	}
	if len(rows) == 0 {
		return NewErrorResponse(req.ID, ErrInvalidParams, "rows must contain at least one row")
	}

	colNames := make([]string, len(columns))
	for i, c := range columns {
		colNames[i] = session.QuoteIdent(fmt.Sprintf("%v", c))
	}

	placeholders := make([]string, len(colNames))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	rowPlaceholder := "(" + strings.Join(placeholders, ", ") + ")"

	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}

	// Classify a representative single-row INSERT of this exact shape
	// (same table/columns, the first row's real values bound as named
	// params) through the real classifier before touching the database.
	// Execution below still uses a prepared "?"-placeholder statement run
	// once per row (unchanged, proven path) — the classification query is
	// structurally identical (same identifiers, same literal-vs-bound-param
	// shape), so what's classified matches what runs.
	firstRow, ok := rows[0].([]any)
	if !ok {
		return NewErrorResponse(req.ID, ErrInvalidParams, "each row must be an array")
	}
	if len(firstRow) != len(colNames) {
		return NewErrorResponse(req.ID, ErrInvalidParams, "row length does not match columns length")
	}
	classifyPlaceholders, classifyParams := namedPlaceholders("v", firstRow)
	tblSQL, tblErr := h.tableSQL(ctx, table)
	if tblErr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, tblErr.Error())
	}
	classifyQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tblSQL, strings.Join(colNames, ", "), strings.Join(classifyPlaceholders, ", "))
	if err := h.requireWriteStatement(ctx, db, classifyQuery, classifyParams); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", tblSQL, strings.Join(colNames, ", "), rowPlaceholder)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to begin transaction: %v", err))
	}

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		_ = tx.Rollback()
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to prepare statement: %v", err))
	}
	defer stmt.Close()

	var inserted int64
	for _, row := range rows {
		rowVals, ok := row.([]any)
		if !ok {
			_ = tx.Rollback()
			return NewErrorResponse(req.ID, ErrInvalidParams, "each row must be an array")
		}
		_, err := stmt.ExecContext(ctx, rowVals...)
		if err != nil {
			_ = tx.Rollback()
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to insert row: %v", err))
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to commit transaction: %v", err))
	}

	return NewResponse(req.ID, map[string]any{"rows_inserted": inserted})
}
