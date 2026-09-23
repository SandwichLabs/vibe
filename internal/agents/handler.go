package agents

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/sandwich-labs/vibe-app/internal/a2ui"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/queries"
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
	// sandbox is an atomic.Pointer, not a plain *SandboxConfig field, because
	// a long-lived Handler (StartMCPServer builds exactly one for the whole
	// life of the MCP server) can be dispatched to concurrently while
	// SetSandboxMode swaps in a live "agent_sandbox" setting change — a
	// caller that mutated a shared *SandboxConfig's fields in place instead
	// would race with every concurrent Handle()/RunReadOnlySQL() reading
	// those same fields. Load() once per call, use the returned pointer for
	// the rest of that call — never re-Load mid-call, so one call sees one
	// consistent sandbox even if SetSandboxMode fires concurrently.
	sandbox  atomic.Pointer[SandboxConfig]
	approval *ApprovalQueue
	logger   *slog.Logger

	// mode gates task-only tools (preview_query, write_cell,
	// need_more_info — see ToolDef.Modes/IsModeVisible). Defaults to ""
	// (deny every Modes-restricted tool) — safe-by-default for chat and MCP,
	// which never set it. internal/llm's Task() loop calls SetMode(ModeTask)
	// on the Handler it builds for a task() call. This is a second,
	// independent gate on top of the tool-list filtering
	// ListToolsForMode(ModeChat)/ListToolsForMode(ModeMCP) already does in
	// the runner/MCP server — a caller that bypasses the advertised tool
	// list and dispatches "write_cell" directly from chat is still denied
	// here.
	mode string

	// activeNotebookID is the id of the notebook currently open in Studio
	// (set via App.SetActiveNotebook, itself called by NotebookView.vue on
	// mount/switch), consulted by add_query_cell/create_dashboard when the
	// caller omits notebook_id. Empty when no notebook is open (a fresh
	// Studio session, or an external MCP client with no Studio window at
	// all) — in that case those tools create a brand-new notebook instead.
	//
	// activeNotebookMu guards activeNotebookID specifically (review fix,
	// finding 8: a DATA RACE). StartMCPServer builds ONE Handler for the
	// server's whole lifetime, and mark3labs/mcp-go dispatches each
	// tools/call on its own goroutine (per internal/mcp/server.go's own Handler
	// doc comment) — two concurrent MCP calls previously wrote/read this
	// field with no synchronization at all. studio/app.go's
	// agentToolHandler.Handle already holds a.mu.RLock() for the whole
	// dispatch and reads a.activeNotebookID under that lock before calling
	// SetActiveNotebook here — but the WRITE into this struct (a completely
	// different lock domain from a.mu) still needs its own guard, since
	// a.mu.RLock() permits multiple concurrent readers, each of which then
	// calls SetActiveNotebook concurrently on this SAME Handler instance.
	// This mutex is deliberately separate from a.mu — reusing a.mu here
	// would mean resolveActiveNotebook (called from inside Handle, which is
	// invoked while a.mu is already held) would have to re-acquire a.mu.RLock
	// from within the SAME goroutine that already holds it: a well-known Go
	// RWMutex hazard (a queued Lock() from a concurrent SetActiveNotebook
	// Wails-binding call can strand that nested RLock forever if it arrives
	// between the two RLock calls). A private mutex sidesteps that entirely.
	activeNotebookMu sync.RWMutex
	activeNotebookID string

	// previewedMu / previewedSQL track which exact (trimmed) sql strings
	// preview_query has succeeded on THIS Handler's lifetime — for
	// chat/MCP that's the process lifetime (irrelevant there: write_cell is
	// task-only), for task mode it's exactly one AgentTask call (a fresh
	// Handler is built per call — see studio/app.go's AgentTask), which is
	// exactly "this task turn". handleWriteCell requires its sql to be a
	// member of this set (review fix, finding 4).
	previewedMu  sync.Mutex
	previewedSQL map[string]bool
}

// SetMode sets the agent-surface mode this Handler dispatches for (see the
// mode field doc). Must be called before Handle for a Handler that should
// allow task-only tools.
func (h *Handler) SetMode(mode string) { h.mode = mode }

// SetActiveNotebook records which notebook (if any) is currently open in
// Studio, for add_query_cell/create_dashboard's "append to the open
// notebook" fallback. Safe to call concurrently with Handle (see
// activeNotebookMu's doc comment on the Handler struct).
func (h *Handler) SetActiveNotebook(id string) {
	h.activeNotebookMu.Lock()
	h.activeNotebookID = id
	h.activeNotebookMu.Unlock()
}

// resolveActiveNotebook reads activeNotebookID under its own lock — see
// activeNotebookMu's doc comment for why this can't just reuse a2ui/callers'
// own locking.
func (h *Handler) resolveActiveNotebook() string {
	h.activeNotebookMu.RLock()
	defer h.activeNotebookMu.RUnlock()
	return h.activeNotebookID
}

// markPreviewed records sql (trimmed) as successfully previewed this turn.
func (h *Handler) markPreviewed(sql string) {
	h.previewedMu.Lock()
	if h.previewedSQL == nil {
		h.previewedSQL = make(map[string]bool, 4)
	}
	h.previewedSQL[strings.TrimSpace(sql)] = true
	h.previewedMu.Unlock()
}

// wasPreviewed reports whether sql (trimmed) was previously recorded by
// markPreviewed.
func (h *Handler) wasPreviewed(sql string) bool {
	h.previewedMu.Lock()
	defer h.previewedMu.Unlock()
	return h.previewedSQL[strings.TrimSpace(sql)]
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
	h := &Handler{
		container: container,
		engine:    engine,
		approval:  NewApprovalQueue(),
		logger:    logger,
	}
	h.sandbox.Store(sandbox)
	return h
}

// SetSandboxMode atomically swaps in a new sandbox mode, preserving every
// other SandboxConfig field (MaxQueryRows, AllowedTables, ...) from whatever
// is currently stored. Safe to call concurrently with Handle()/
// RunReadOnlySQL() — see the sandbox field's doc comment. Used by
// studio/app.go's agentToolHandler to make a long-lived Handler (the one
// StartMCPServer builds) track the file's live "agent_sandbox" setting on
// every dispatch, without racing on a shared *SandboxConfig.
func (h *Handler) SetSandboxMode(mode SandboxMode) {
	cur := h.sandbox.Load()
	next := *cur
	next.Mode = mode
	h.sandbox.Store(&next)
}

// Approvals returns the handler's approval queue so callers (and tests) can
// drive the approval workflow (Pending/Approve/Reject) for gated write tools.
func (h *Handler) Approvals() *ApprovalQueue {
	return h.approval
}

// Handle processes a JSON-RPC request and returns a response.
func (h *Handler) Handle(ctx context.Context, req *Request) *Response {
	h.logger.Info("handling request", "method", req.Method, "id", req.ID)

	// Validate the method exists.
	tool := GetTool(req.Method)
	if tool == nil {
		return NewErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("unknown method: %q", req.Method))
	}
	if !IsModeVisible(req.Method, h.mode) {
		return NewErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method %q is not available in this context", req.Method))
	}

	// Validate required params.
	if err := validateParams(tool, req.Params); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}

	// Load the sandbox ONCE for this call (see the field's doc comment) so a
	// concurrent SetSandboxMode can't make one request's checks straddle two
	// different sandbox configs.
	sb := h.sandbox.Load()

	// Check sandbox permissions.
	if err := sb.Check(req.Method); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}

	// Check table allowlist for table-specific operations.
	if table := extractTableParam(req.Params); table != "" {
		if !sb.IsTableAllowed(table) {
			return NewErrorResponse(req.ID, ErrPermission, fmt.Sprintf("table %q is not in the allowed tables list", table))
		}
	}

	// Check if approval is required for write operations.
	if sb.RequireApproval && isWriteOperation(tool.Category) {
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
	case "list_queries":
		return h.handleListQueries(ctx, req)
	case "list_notebooks":
		return h.handleListNotebooks(ctx, req)
	case "get_notebook":
		return h.handleGetNotebook(ctx, req)
	case "help_search":
		return h.handleHelpSearch(ctx, req)
	case "list_skills":
		return h.handleListSkills(ctx, req)
	case "read_skill":
		return h.handleReadSkill(ctx, req)

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

	// Schema
	case "create_table":
		return h.handleCreateTable(ctx, req)
	case "add_column":
		return h.handleAddColumn(ctx, req)
	case "drop_table":
		return h.handleDropTable(ctx, req)
	case "import_csv":
		return h.handleImportCSV(ctx, req)

	// Query Wizard
	case "build_query":
		return h.handleBuildQuery(ctx, req)

	// Schema Builder draft + saved-SQL query (ported from internal/llm, #63)
	case "propose_schema":
		return h.handleProposeSchema(ctx, req)
	case "save_query":
		return h.handleSaveQuery(ctx, req)

	// A2UI artifacts (#67)
	case "present_data":
		return h.handlePresentData(ctx, req)

	// Skills (#62)
	case "create_or_update_skill":
		return h.handleCreateOrUpdateSkill(ctx, req)

	// Task mode (#66 part B)
	case "preview_query":
		return h.handlePreviewQuery(ctx, req)
	case "write_cell":
		return h.handleWriteCell(ctx, req)
	case "need_more_info":
		return h.handleNeedMoreInfo(ctx, req)

	// Notebook-authoring (#67 deferred part)
	case "add_query_cell":
		return h.handleAddQueryCell(ctx, req)
	case "create_dashboard":
		return h.handleCreateDashboard(ctx, req)

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
	table, _ := req.Params["table"].(string)
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

// RunReadOnlySQL executes sql as a live A2UI data-bound component's query
// (RunA2UIQuery, #67 step-2 review fix) — a distinct entry point from the
// JSON-RPC "run_query" dispatch (handleRunQuery), not a JSON-RPC method
// itself, because a data-bound "sql" field must be denied a write/DDL
// statement regardless of the file's sandbox mode (checkSQLClassReadOnly),
// where handleRunQuery's checkSQLClass deliberately lets read-write/full
// through — that permissiveness is correct for an agent's own one-shot
// run_query tool call, wrong for a query that re-executes on every render.
// It reuses classifySQL and h.engine.Execute exactly as handleRunQuery
// does — no second classifier, no second execution path.
//
// maxRows > 0 pushes the row cap down into the query itself (review fix,
// round 4): a component's SQL was already classified sqlClassRead above —
// a single statement, no side effects — so it is safe to run it as
// `SELECT * FROM (<sql>) AS _a2ui_limit LIMIT maxRows+1` instead of
// fetching a potentially huge/unbounded result (SELECT * FROM
// range(100000000)) into memory just to slice it down to maxRows
// afterward. truncated reports whether the cap actually cut anything.
// maxRows <= 0 skips the wrap and returns the full result, uncapped.
//
// Not every class-Read shape can be wrapped as a FROM-subquery this way —
// confirmed empirically: a plain SELECT/CTE, DESCRIBE, SHOW (TABLES), and
// SUMMARIZE all wrap fine, but PRAGMA table_info(...) and EXPLAIN are a
// DuckDB parser error as a subquery. Rather than hand-maintain a list of
// exactly which shapes are exempt (classifySQL already has enough of that
// bookkeeping), this just tries the wrapped form first and falls back to
// running sqlStr directly (then truncating client-side, exactly like
// before this fix) on any error from the wrapped attempt — safe to retry
// because the statement is already proven read-only, and these excluded
// shapes are metadata queries that never return a huge row count anyway,
// so the fallback's un-limited execution costs nothing in practice.
func (h *Handler) RunReadOnlySQL(ctx context.Context, sqlStr string, params map[string]any, maxRows int) (rs *engine.ResultSet, truncated bool, err error) {
	userDB := h.container.DuckDBDB()
	if userDB == nil {
		return nil, false, fmt.Errorf("DuckDB not available")
	}
	class, reason := classifySQL(ctx, userDB, sqlStr, params)
	if err := h.sandbox.Load().checkSQLClassReadOnly(class, reason); err != nil {
		// The user/agent-facing error is one fixed, mode-agnostic message
		// (see errReadOnlyBindingDenied) — it never surfaces classifySQL's
		// reason, which named a specific sandbox-mode requirement
		// (e.g. "requires the full sandbox") that is actively false for a
		// full-mode caller hitting this same denial. The reason is still
		// useful for debugging a denial that looks surprising, so it's
		// logged here at debug level only.
		h.logger.Debug("render-time sql binding denied", "class", class, "reason", reason, "sql", sqlStr)
		return nil, false, err
	}

	if maxRows > 0 {
		trimmed := strings.TrimSuffix(strings.TrimSpace(sqlStr), ";")
		wrapped := fmt.Sprintf("SELECT * FROM (%s) AS _a2ui_limit LIMIT %d", trimmed, maxRows+1)
		if rs, err = h.engine.Execute(ctx, wrapped, params); err == nil {
			if len(rs.Rows) > maxRows {
				rs.Rows = rs.Rows[:maxRows]
				truncated = true
			}
			return rs, truncated, nil
		}
		h.logger.Debug("render-time sql binding: LIMIT push-down not supported for this statement shape, falling back to unwrapped execution", "sql", sqlStr, "wrap_error", err)
	}

	rs, err = h.engine.Execute(ctx, sqlStr, params)
	if err != nil {
		return nil, false, fmt.Errorf("query execution failed: %w", err)
	}
	if maxRows > 0 && len(rs.Rows) > maxRows {
		rs.Rows = rs.Rows[:maxRows]
		truncated = true
	}
	return rs, truncated, nil
}

func (h *Handler) handleRunQuery(ctx context.Context, req *Request) *Response {
	sqlStr, _ := req.Params["sql"].(string)
	var params map[string]any
	if p, ok := req.Params["params"].(map[string]any); ok {
		params = p
	}

	// Per-statement sandbox gate (review fix for #63): run_query's "data"
	// category alone let read-write execute CREATE/DROP/COPY/ATTACH — the
	// same operations the dedicated schema tools deny outside full sandbox.
	// The statement count and the read decision both come from DuckDB
	// itself, never hand-written lexing (round 3 review fix: statement
	// counting via duckdb_extract_statements, table references resolved
	// against the real catalog via duckdb_tables()/duckdb_views() on
	// userDB — not extension-guessing or ';' counting, both of which were
	// bypassable). classifySQL binds params itself (round 5 review fix) so
	// EXPLAIN can actually bind a parameterized query instead of choking on
	// a bare $name placeholder.
	userDB, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	sb := h.sandbox.Load()
	if err := sb.checkSQLClass(classifySQL(ctx, userDB, sqlStr, params)); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}

	rs, err := h.engine.Execute(ctx, sqlStr, params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("query execution failed: %v", err))
	}

	// Enforce max rows.
	rows := rs.Rows
	if sb.MaxQueryRows > 0 && len(rows) > sb.MaxQueryRows {
		rows = rows[:sb.MaxQueryRows]
	}

	cols := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = c.Name
	}

	result := map[string]any{
		"columns": cols,
		"rows":    rows,
	}

	// Empty-result explanation (#70): must run BEFORE the row budget below
	// (an empty result is never truncated anyway) and only ever issues
	// read-only, internally-constructed probes against real,
	// information_schema-verified columns — see explainEmptyResult's doc
	// comment. userDB is the same connection already gated by
	// checkSQLClass above; the probes themselves never touch classifySQL
	// because they are fixed SELECT DISTINCT templates this handler builds,
	// never the caller's SQL text executed a second time.
	if len(rows) == 0 {
		if note := explainEmptyResult(ctx, userDB, sqlStr); note != "" {
			result["note"] = note
		}
	}

	// Tool-result budget (#70): cap the row JSON at ~RowResultBudgetChars so
	// a large SELECT never floods the model's (or MCP client's) context.
	// Applied here at the handler layer so chat, task mode, and MCP all get
	// it uniformly — see budget.go.
	if budgeted, truncated := budgetRows(rows, RowResultBudgetChars); truncated != nil {
		result["rows"] = budgeted
		result["truncated"] = truncated
	}

	return NewResponse(req.ID, result)
}

// requireWriteStatement classifies a fully-built SQL statement (identifiers
// already quoted via engine.QuoteIdent, values addressed only via $name
// placeholders bound through params — never a raw attacker value
// concatenated in) through the SAME real-DuckDB classifier run_query uses
// (classifySQL), and requires it come back sqlClassWrite. This is what
// stops a `where` param like `1=0; DROP TABLE users` or `1=0; COPY (...) TO
// '<path>'`: DuckDB's own statement counter (via countStatements) sees two
// statements and classifySQL fails the whole thing to sqlClassOther, denied
// here — no hand-rolled semicolon/comment scanning involved. A
// single-statement subquery reaching outside the database (e.g. a `where`
// containing `IN (SELECT ... FROM read_csv(...))`) is caught by
// classifyFallback's own checkPlanExternalAccess (DuckDB's bound physical
// plan), not by this function — it just enforces the class this record tool
// needs.
func (h *Handler) requireWriteStatement(ctx context.Context, userDB *sql.DB, query string, params map[string]any) error {
	class, reason := classifySQL(ctx, userDB, query, params)
	if class != sqlClassWrite {
		if reason != "" {
			return fmt.Errorf("%s", reason)
		}
		return fmt.Errorf("this operation is not a single, plain write statement — denied")
	}
	return nil
}

// namedPlaceholders builds "$p0, $p1, ..." style placeholders (the same
// $name addressing engine.BindParams/classifySQL understand — see
// requireWriteStatement) for n values, returning the placeholder list and a
// params map assigning vals[i] to $p<i>. Both the classification call and
// the real execution use the identical (query, params) pair via
// engine.BindParams, so what gets classified is byte-for-byte what runs.
func namedPlaceholders(prefix string, vals []any) (placeholders []string, params map[string]any) {
	placeholders = make([]string, len(vals))
	params = make(map[string]any, len(vals))
	for i, v := range vals {
		name := fmt.Sprintf("%s%d", prefix, i)
		placeholders[i] = "$" + name
		params[name] = v
	}
	return placeholders, params
}

func (h *Handler) handleCreateRecord(ctx context.Context, req *Request) *Response {
	table, _ := req.Params["table"].(string)
	values, _ := req.Params["values"].(map[string]any)

	cols := make([]string, 0, len(values))
	vals := make([]any, 0, len(values))
	for k, v := range values {
		cols = append(cols, engine.QuoteIdent(k))
		vals = append(vals, v)
	}
	placeholders, params := namedPlaceholders("v", vals)

	db, errResp := h.duckDB(req.ID)
	if errResp != nil {
		return errResp
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", engine.QuoteIdent(table), strings.Join(cols, ", "), strings.Join(placeholders, ", "))
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
		cols = append(cols, engine.QuoteIdent(k))
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
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", engine.QuoteIdent(table), strings.Join(setClauses, ", "), where)
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
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", engine.QuoteIdent(table), where)
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
		colNames[i] = engine.QuoteIdent(fmt.Sprintf("%v", c))
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
	classifyQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", engine.QuoteIdent(table), strings.Join(colNames, ", "), strings.Join(classifyPlaceholders, ", "))
	if err := h.requireWriteStatement(ctx, db, classifyQuery, classifyParams); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, err.Error())
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", engine.QuoteIdent(table), strings.Join(colNames, ", "), rowPlaceholder)
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

// --- Schema handlers ---

// validColumnTypeText allow-lists what a DDL column-type string may
// contain before it's concatenated into CREATE TABLE / ALTER TABLE DDL:
// letters, digits, spaces, parens and commas — enough for "VARCHAR(64)",
// "DECIMAL(10,2)", or a friendly-type-plus-constraint like "INTEGER PRIMARY
// KEY" (create_table's ToolDef explicitly documents embedding constraints
// this way), while rejecting quotes, semicolons, and comment sequences.
// DuckDB still validates the type semantically at Exec time. Mirrors
// studio/schema_designer.go's isValidColumnType (kept separate: that file
// is package main and can't be imported from here).
func validColumnTypeText(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	hasAlpha := false
	for _, c := range t {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasAlpha = true
		case c >= '0' && c <= '9':
		case c == ' ' || c == '(' || c == ')' || c == ',':
		default:
			return false
		}
	}
	return hasAlpha
}

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
		if !validColumnTypeText(colType) {
			return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid column type %q", colType))
		}
		colDefs = append(colDefs, fmt.Sprintf("%s %s", engine.QuoteIdent(colName), colType))
	}

	query := fmt.Sprintf("CREATE TABLE %s (%s)", engine.QuoteIdent(name), strings.Join(colDefs, ", "))
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
	if !validColumnTypeText(colType) {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid column type %q", colType))
	}

	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", engine.QuoteIdent(table), engine.QuoteIdent(name), colType)
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

	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", engine.QuoteIdent(name))
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
		query := fmt.Sprintf("INSERT INTO %s VALUES (%s)", engine.QuoteIdent(table), strings.Join(placeholders, ", "))
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
// _vibe_queries registry (studio's SaveQuery binding does the same thing).
// Unlike build_query it does not render SQL from QBE conditions.
func (h *Handler) handleSaveQuery(ctx context.Context, req *Request) *Response {
	title, _ := req.Params["title"].(string)
	sqlStr, _ := req.Params["sql"].(string)

	db := h.container.SQLiteDB()
	registry := queries.NewRegistry(db, h.logger)

	q := &queries.Query{
		ID:         uuid.New().String(),
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := registry.Create(ctx, q); err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to save query: %v", err))
	}
	return NewResponse(req.ID, map[string]any{"id": q.ID, "title": title, "saved": true})
}

// --- A2UI handlers (#67) ---

// handlePresentData validates the "page" param against the A2UI schema and
// returns the validated page as-is. A failure surfaces as ErrInvalidParams
// with a2ui.Validate's newline-joined, JSON-path-prefixed error text — that
// text is what the model sees as the tool error, so it can correct its next
// attempt. present_data never runs SQL itself: sql-bound components
// (stat/table/barChart/lineChart with "sql") are executed later, per
// component, by the Vue renderer via RunA2UIQuery — which re-enters run_query's
// classifier — so a data-bound present_data result cannot be used to bypass
// the sandbox at validation time.
func (h *Handler) handlePresentData(_ context.Context, req *Request) *Response {
	page, err := a2ui.Validate(req.Params["page"])
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	return NewResponse(req.ID, page)
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
	return category == "data" || category == "content" || category == "schema"
}
