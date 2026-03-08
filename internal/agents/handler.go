package agents

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// ContainerAccess provides access to the container (interface to avoid circular imports).
type ContainerAccess interface {
	SQLiteDB() *sql.DB
	DuckDBDB() *sql.DB
}

// EngineAccess provides query execution.
type EngineAccess interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error)
}

// Handler dispatches JSON-RPC requests to the appropriate tool handler.
type Handler struct {
	container ContainerAccess
	engine    EngineAccess
	sandbox   *SandboxConfig
	approval  *ApprovalQueue
	logger    *slog.Logger
}

// duckDB returns the DuckDB connection or an error response if unavailable.
func (h *Handler) duckDB(reqID any) (*sql.DB, *Response) {
	db := h.container.DuckDBDB()
	if db == nil {
		return nil, NewErrorResponse(reqID, ErrInternal, "DuckDB not available")
	}
	return db, nil
}

// NewHandler creates a new request handler.
func NewHandler(container ContainerAccess, engine EngineAccess, sandbox *SandboxConfig, logger *slog.Logger) *Handler {
	return &Handler{
		container: container,
		engine:    engine,
		sandbox:   sandbox,
		approval:  NewApprovalQueue(),
		logger:    logger,
	}
}

// Handle processes a JSON-RPC request and returns a response.
func (h *Handler) Handle(ctx context.Context, req *Request) *Response {
	h.logger.Info("handling request", "method", req.Method, "id", req.ID)

	// Validate the method exists.
	tool := GetTool(req.Method)
	if tool == nil {
		return NewErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("unknown method: %q", req.Method))
	}

	// Validate required params.
	if err := validateParams(tool, req.Params); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}

	// Check sandbox permissions.
	if err := h.sandbox.Check(req.Method); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}

	// Check table allowlist for table-specific operations.
	if table := extractTableParam(req.Params); table != "" {
		if !h.sandbox.IsTableAllowed(table) {
			return NewErrorResponse(req.ID, ErrPermission, fmt.Sprintf("table %q is not in the allowed tables list", table))
		}
	}

	// Check if approval is required for write operations.
	if h.sandbox.RequireApproval && isWriteOperation(tool.Category) {
		approved := h.approval.Submit(&ApprovalRequest{
			ID:     uuid.New().String(),
			Method: req.Method,
			Params: req.Params,
		})
		if !approved {
			return NewErrorResponse(req.ID, ErrApprovalNeeded, "operation was rejected by user")
		}
	}

	return h.dispatch(ctx, req)
}

// dispatch routes the request to the appropriate handler method.
func (h *Handler) dispatch(ctx context.Context, req *Request) *Response {
	switch req.Method {
	// Introspect
	case "introspect_schema":
		return h.handleIntrospectSchema(ctx, req)
	case "describe_table":
		return h.handleDescribeTable(ctx, req)
	case "list_pages":
		return h.handleListPages(ctx, req)
	case "get_page":
		return h.handleGetPage(ctx, req)
	case "list_queries":
		return h.handleListQueries(ctx, req)
	case "help_search":
		return h.handleHelpSearch(ctx, req)

	// Data
	case "run_query":
		return h.handleRunQuery(ctx, req)
	case "create_record":
		return h.handleCreateRecord(ctx, req)
	case "update_record":
		return h.handleUpdateRecord(ctx, req)
	case "delete_record":
		return h.handleDeleteRecord(ctx, req)
	case "bulk_insert":
		return h.handleBulkInsert(ctx, req)

	// Page
	case "create_page":
		return h.handleCreatePage(ctx, req)
	case "update_page":
		return h.handleUpdatePage(ctx, req)
	case "delete_page":
		return h.handleDeletePage(ctx, req)

	// Schema
	case "create_table":
		return h.handleCreateTable(ctx, req)
	case "add_column":
		return h.handleAddColumn(ctx, req)
	case "drop_table":
		return h.handleDropTable(ctx, req)
	case "import_csv":
		return h.handleImportCSV(ctx, req)

	default:
		return NewErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("unknown method: %q", req.Method))
	}
}

// --- Introspect handlers ---

func (h *Handler) handleIntrospectSchema(ctx context.Context, req *Request) *Response {
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	rows, err := db.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name")
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to query schema: %v", err))
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to scan table name: %v", err))
		}
		tables = append(tables, name)
	}
	return NewResponse(req.ID, map[string]any{"tables": tables})
}

func (h *Handler) handleDescribeTable(ctx context.Context, req *Request) *Response {
	table := req.Params["table"].(string)
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}

	rows, err := db.QueryContext(ctx,
		"SELECT column_name, data_type, CASE WHEN is_nullable = 'NO' THEN true ELSE false END AS not_null FROM information_schema.columns WHERE table_schema = 'main' AND table_name = $1 ORDER BY ordinal_position",
		table)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to describe table: %v", err))
	}
	defer rows.Close()

	var columns []map[string]any
	for rows.Next() {
		var name, colType string
		var notNull bool
		if err := rows.Scan(&name, &colType, &notNull); err != nil {
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to scan column info: %v", err))
		}
		columns = append(columns, map[string]any{
			"name":     name,
			"type":     colType,
			"not_null": notNull,
			"pk":       false,
		})
	}
	return NewResponse(req.ID, map[string]any{"table": table, "columns": columns})
}

func (h *Handler) handleListPages(ctx context.Context, req *Request) *Response {
	db := h.container.SQLiteDB()
	rows, err := db.QueryContext(ctx, "SELECT title FROM _vibe_pages ORDER BY sort_order, title")
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to list pages: %v", err))
	}
	defer rows.Close()

	var pages []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to scan page name: %v", err))
		}
		pages = append(pages, name)
	}
	return NewResponse(req.ID, map[string]any{"pages": pages})
}

func (h *Handler) handleGetPage(ctx context.Context, req *Request) *Response {
	name := req.Params["name"].(string)
	db := h.container.SQLiteDB()

	var content string
	err := db.QueryRowContext(ctx, "SELECT content FROM _vibe_pages WHERE title = ?", name).Scan(&content)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to get page %q: %v", name, err))
	}
	return NewResponse(req.ID, map[string]any{"name": name, "content": content})
}

func (h *Handler) handleListQueries(ctx context.Context, req *Request) *Response {
	db := h.container.SQLiteDB()
	rows, err := db.QueryContext(ctx, "SELECT title, sql, engine FROM _vibe_queries ORDER BY title")
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to list queries: %v", err))
	}
	defer rows.Close()

	var queries []map[string]any
	for rows.Next() {
		var name, sqlText, engine string
		if err := rows.Scan(&name, &sqlText, &engine); err != nil {
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to scan query: %v", err))
		}
		queries = append(queries, map[string]any{
			"name":   name,
			"sql":    sqlText,
			"engine": engine,
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

// --- Data handlers ---

func (h *Handler) handleRunQuery(ctx context.Context, req *Request) *Response {
	sqlStr := req.Params["sql"].(string)
	var params map[string]any
	if p, ok := req.Params["params"].(map[string]any); ok {
		params = p
	}

	rs, err := h.engine.Execute(ctx, sqlStr, params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("query execution failed: %v", err))
	}

	// Enforce max rows.
	rows := rs.Rows
	if h.sandbox.MaxQueryRows > 0 && len(rows) > h.sandbox.MaxQueryRows {
		rows = rows[:h.sandbox.MaxQueryRows]
	}

	cols := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = c.Name
	}

	return NewResponse(req.ID, map[string]any{
		"columns": cols,
		"rows":    rows,
	})
}

func (h *Handler) handleCreateRecord(ctx context.Context, req *Request) *Response {
	table := req.Params["table"].(string)
	values := req.Params["values"].(map[string]any)

	cols := make([]string, 0, len(values))
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for k, v := range values {
		cols = append(cols, k)
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to create record: %v", err))
	}

	lastID, _ := result.LastInsertId()
	return NewResponse(req.ID, map[string]any{"last_insert_id": lastID})
}

func (h *Handler) handleUpdateRecord(ctx context.Context, req *Request) *Response {
	table := req.Params["table"].(string)
	values := req.Params["values"].(map[string]any)
	where := req.Params["where"].(string)

	setClauses := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for k, v := range values {
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", k))
		args = append(args, v)
	}

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(setClauses, ", "), where)
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to update record: %v", err))
	}

	affected, _ := result.RowsAffected()
	return NewResponse(req.ID, map[string]any{"rows_affected": affected})
}

func (h *Handler) handleDeleteRecord(ctx context.Context, req *Request) *Response {
	table := req.Params["table"].(string)
	where := req.Params["where"].(string)

	query := fmt.Sprintf("DELETE FROM %s WHERE %s", table, where)
	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	result, err := db.ExecContext(ctx, query)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to delete record: %v", err))
	}

	affected, _ := result.RowsAffected()
	return NewResponse(req.ID, map[string]any{"rows_affected": affected})
}

func (h *Handler) handleBulkInsert(ctx context.Context, req *Request) *Response {
	table := req.Params["table"].(string)
	columns, ok := req.Params["columns"].([]any)
	if !ok {
		return NewErrorResponse(req.ID, ErrInvalidParams, "columns must be an array of strings")
	}
	rows, ok := req.Params["rows"].([]any)
	if !ok {
		return NewErrorResponse(req.ID, ErrInvalidParams, "rows must be an array of arrays")
	}

	colNames := make([]string, len(columns))
	for i, c := range columns {
		colNames[i] = fmt.Sprintf("%v", c)
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to begin transaction: %v", err))
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", table, strings.Join(colNames, ", "), rowPlaceholder)
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		tx.Rollback()
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to prepare statement: %v", err))
	}
	defer stmt.Close()

	var inserted int64
	for _, row := range rows {
		rowVals, ok := row.([]any)
		if !ok {
			tx.Rollback()
			return NewErrorResponse(req.ID, ErrInvalidParams, "each row must be an array")
		}
		_, err := stmt.ExecContext(ctx, rowVals...)
		if err != nil {
			tx.Rollback()
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to insert row: %v", err))
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to commit transaction: %v", err))
	}

	return NewResponse(req.ID, map[string]any{"rows_inserted": inserted})
}

// --- Page handlers ---

func (h *Handler) handleCreatePage(ctx context.Context, req *Request) *Response {
	name := req.Params["name"].(string)
	content := req.Params["content"].(string)
	now := nowISO()

	db := h.container.SQLiteDB()
	_, err := db.ExecContext(ctx,
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at, metadata) VALUES (?, ?, ?, 'markdown', 0, ?, ?, '{}')",
		uuid.New().String(), name, content, now, now,
	)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to create page: %v", err))
	}
	return NewResponse(req.ID, map[string]any{"name": name, "created": true})
}

func (h *Handler) handleUpdatePage(ctx context.Context, req *Request) *Response {
	name := req.Params["name"].(string)
	content := req.Params["content"].(string)

	db := h.container.SQLiteDB()
	result, err := db.ExecContext(ctx,
		"UPDATE _vibe_pages SET content = ?, updated_at = ? WHERE title = ?",
		content, nowISO(), name,
	)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to update page: %v", err))
	}
	affected, _ := result.RowsAffected()
	return NewResponse(req.ID, map[string]any{"name": name, "updated": affected > 0})
}

func (h *Handler) handleDeletePage(ctx context.Context, req *Request) *Response {
	name := req.Params["name"].(string)

	db := h.container.SQLiteDB()
	result, err := db.ExecContext(ctx, "DELETE FROM _vibe_pages WHERE title = ?", name)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to delete page: %v", err))
	}
	affected, _ := result.RowsAffected()
	return NewResponse(req.ID, map[string]any{"name": name, "deleted": affected > 0})
}

// --- Schema handlers ---

func (h *Handler) handleCreateTable(ctx context.Context, req *Request) *Response {
	name := req.Params["name"].(string)
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
		colDefs = append(colDefs, fmt.Sprintf("%s %s", colName, colType))
	}

	query := fmt.Sprintf("CREATE TABLE %s (%s)", name, strings.Join(colDefs, ", "))
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
	table := req.Params["table"].(string)
	name := req.Params["name"].(string)
	colType := req.Params["type"].(string)

	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, colType)
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
	name := req.Params["name"].(string)

	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", name)
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
	table := req.Params["table"].(string)
	data := req.Params["data"].(string)
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
		query := fmt.Sprintf("INSERT INTO %s VALUES (%s)", table, strings.Join(placeholders, ", "))
		_, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			tx.Rollback()
			return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to import row %d: %v", i, err))
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to commit import: %v", err))
	}

	return NewResponse(req.ID, map[string]any{"table": table, "rows_imported": inserted})
}

// --- Helpers ---

// validateParams checks that all required parameters are present.
func validateParams(tool *ToolDef, params map[string]any) error {
	for _, p := range tool.Params {
		if p.Required {
			if _, ok := params[p.Name]; !ok {
				return fmt.Errorf("missing required parameter: %q", p.Name)
			}
		}
	}
	return nil
}

// extractTableParam extracts the table name from params if present.
func extractTableParam(params map[string]any) string {
	if t, ok := params["table"].(string); ok {
		return t
	}
	return ""
}

// isWriteOperation returns true if the category involves modifications.
func isWriteOperation(category string) bool {
	return category == "data" || category == "page" || category == "schema"
}

// nowISO returns the current UTC time as an ISO 8601 string.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
