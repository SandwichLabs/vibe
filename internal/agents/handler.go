// Package agents is the one tool surface for the built-in agent and MCP
// clients: the ToolDef registry, sandbox tiers, the approval queue, the SQL
// allow-list policy and the JSON-RPC handler that dispatches tool calls.
package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/SandwichLabs/vibe/internal/crud"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/llm/skills"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/google/uuid"
)

// WorkspaceAccess provides access to the open workspace (interface to avoid circular imports).
type WorkspaceAccess interface {
	DuckDBDB() *sql.DB
	// Session is the catalog-aware DuckDB hub over DuckDBDB. Introspection
	// goes through it so no tool assumes the default catalog is "main".
	Session() session.Session
	// Notebooks, Queries and Skills are the content stores: file-backed in the
	// workspace folder (notebooks/, queries/, skills/). The agent never
	// touches the files directly.
	Notebooks() notebooks.Store
	Queries() queries.Store
	Skills() skills.Store
}

// ProtectedPathsProvider is implemented by a WorkspaceAccess that knows the
// files and folders of its attached connections (a DuckLake's catalog and data
// folder, duckdb and sqlite files): COPY ... TO never writes into them.
type ProtectedPathsProvider interface {
	ProtectedPaths() []string
}

// DataDirProvider is implemented by a WorkspaceAccess that has a workspace:
// the folder (data/) the agent may read files from and COPY ... TO. A legacy
// container does not implement it, so agent SQL there reads no files at all.
type DataDirProvider interface {
	DataDir() string
}

// EngineAccess provides query execution.
type EngineAccess interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error)
}

// Handler dispatches JSON-RPC requests to the appropriate tool handler.
type Handler struct {
	access WorkspaceAccess
	engine EngineAccess
	// sandbox is an atomic.Pointer, not a plain *SandboxConfig field, because
	// a long-lived Handler (StartMCPServer builds exactly one for the whole
	// life of the MCP server) can be dispatched to concurrently while
	// SetFilePolicy swaps in a live "agent_sandbox"/"ask before changes"
	// setting change — a caller that mutated a shared *SandboxConfig's
	// fields in place instead would race with every concurrent
	// Handle()/RunReadOnlySQL() reading those same fields. Load() once per
	// call, use the returned pointer for the rest of that call — never
	// re-Load mid-call, so one call sees one consistent sandbox even if
	// SetFilePolicy fires concurrently.
	sandbox atomic.Pointer[SandboxConfig]
	// approver, when set, is called to obtain the user's OK for a write
	// (#79) — see awaitApproval. nil means "nobody can answer": headless
	// `vibe mcp serve` and Studio's SSE MCP server never call SetApprover,
	// so a write needing approval on either of those is refused outright
	// rather than hanging forever waiting for an answer nobody can give.
	approver ApproveFunc
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
func (h *Handler) markPreviewed(connection, sql string) {
	h.previewedMu.Lock()
	if h.previewedSQL == nil {
		h.previewedSQL = make(map[string]bool, 4)
	}
	h.previewedSQL[connection+"\x00"+strings.TrimSpace(sql)] = true
	h.previewedMu.Unlock()
}

// wasPreviewed reports whether sql (trimmed) was previously recorded by
// markPreviewed.
func (h *Handler) wasPreviewed(connection, sql string) bool {
	h.previewedMu.Lock()
	defer h.previewedMu.Unlock()
	return h.previewedSQL[connection+"\x00"+strings.TrimSpace(sql)]
}

// duckDB returns the DuckDB connection or an error response if unavailable.
func (h *Handler) duckDB(reqID any) (*sql.DB, *Response) {
	db := h.access.DuckDBDB()
	if db == nil {
		return nil, NewErrorResponse(reqID, ErrInternal, "DuckDB not available")
	}
	return db, nil
}

// tableSQL renders a tool's table argument as fully resolved, quoted SQL
// (catalog.schema.table), so a write cannot be redirected by the connection's
// current catalog or shadowed by a same-named TEMP table. A name that is
// exactly an existing table of the default catalog and schema wins over
// dotted-path parsing (a legacy table may legitimately contain dots); a name
// that cannot be resolved falls back to engine.TableSQL's safe quoting.
func (h *Handler) tableSQL(ctx context.Context, name string) (string, error) {
	sess := h.access.Session()
	if sess == nil || sess.DB() == nil {
		return engine.TableSQL(name), nil
	}
	bare := session.TableRef{Name: name}
	if _, ok, _ := session.LookupTable(ctx, sess.DB(), bare); ok {
		if r, err := sess.Resolve(ctx, bare); err == nil {
			return r.SQL(), nil
		}
		return bare.SQL(), nil
	}
	ref, err := session.ParseTableRef(name)
	if err != nil {
		return engine.QuoteIdent(name), nil
	}
	r, err := sess.Resolve(ctx, ref)
	if err != nil && ref.Catalog == "" && ref.Schema == "" {
		// Nothing to resolve against (not a DuckDB handle): a bare name can
		// only mean the connection's own current table.
		return ref.SQL(), nil
	}
	if err != nil {
		// An unknown connection (or the internal "system"/"temp" catalogs)
		// is refused rather than passed to DuckDB.
		return "", err
	}
	return r.SQL(), nil
}

// sess returns the DuckDB session or an error response if unavailable.
func (h *Handler) sess(reqID any) (session.Session, *Response) {
	s := h.access.Session()
	if s == nil {
		return nil, NewErrorResponse(reqID, ErrInternal, "DuckDB not available")
	}
	return s, nil
}

// NewHandler creates a new request handler.
// connectReferenced dials the registered, not yet connected connections the
// statement names (lazy attach, #115) so the classifier's EXPLAIN can bind
// them. A failure is returned, not fatal: the caller carries on, and only when
// the classifier then denies the statement (it names a catalog that is not
// there) is the connection's own problem reported instead of a misleading
// "external access" denial (see withConnectError).
func (h *Handler) connectReferenced(ctx context.Context, catalog, sqlText string) error {
	if h.access == nil {
		return nil
	}
	if s := h.access.Session(); s != nil {
		return s.ConnectReferenced(ctx, catalog, sqlText)
	}
	return nil
}

// withConnectError prefers the connection's own failure over a policy denial
// that it caused.
func withConnectError(denial, connectErr error) error {
	if connectErr != nil {
		return connectErr
	}
	return denial
}

func NewHandler(access WorkspaceAccess, engine EngineAccess, sandbox *SandboxConfig, logger *slog.Logger) *Handler {
	h := &Handler{
		access: access,
		engine: engine,
		logger: logger,
	}
	h.sandbox.Store(sandbox)
	return h
}

// ApproveFunc obtains the user's OK for a pending write (#79). It is called
// synchronously from inside Handle/handleRunQuery — a blocking call is
// exactly the intended behaviour (the model's tool call waits for a human
// answer) — and returns nil only on approval. studio/app.go's
// agentToolHandler.enableApprovals is the only production implementation: it
// releases the caller's file lock for the duration of the wait (documented
// on that function) so Save/Close/Settings stay responsive while a card is
// pending.
type ApproveFunc func(ctx context.Context, req ApprovalRequest) error

// SetApprover installs (or, with nil, removes) the function Handle/
// handleRunQuery calls to obtain approval for a gated write. A Handler with
// no approver refuses every write that needs one (see awaitApproval) instead
// of blocking — that is the deliberate MCP behaviour: no
// approver is ever attached to a headless or SSE MCP server.
func (h *Handler) SetApprover(fn ApproveFunc) { h.approver = fn }

// SetFilePolicy atomically swaps in both the sandbox mode and the
// RequireApproval flag together, preserving every other SandboxConfig field
// (MaxQueryRows, AllowedTables, ...) from whatever is currently stored. One
// atomic swap — not two separate setters — so a concurrent Handle() call can
// never observe a mode/approval combination that was never actually the
// file's policy (e.g. the new mode with the old approval flag). Safe to call
// concurrently with Handle()/RunReadOnlySQL() — see the sandbox field's doc
// comment.
func (h *Handler) SetFilePolicy(mode SandboxMode, requireApproval bool) {
	cur := h.sandbox.Load()
	next := *cur
	next.Mode = mode
	next.RequireApproval = requireApproval
	h.sandbox.Store(&next)
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
	// concurrent SetFilePolicy can't make one request's checks straddle two
	// different sandbox configs.
	sb := h.sandboxFor(ctx)

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

	// Approval gate (#79): every write EXCEPT run_query asks here, using the
	// sandbox's own category gate above as "would this even be allowed" —
	// approval runs strictly after that check passes, so a call the sandbox
	// denies never shows a card (a read-only-mode delete_record fails with
	// ErrPermission above and never reaches this point). run_query is
	// handled separately, inside handleRunQuery, because whether it asks at
	// all depends on classifying the SQL text, not just its "data" category.
	if isWriteOperation(tool.Category) && req.Method != "run_query" {
		var nonLocal []string
		if t, ok := h.toolTarget(ctx, req); ok {
			// Three-gate writes (#116): the tier allowed the tool above; the
			// target connection must be writable; and anything that is not a
			// local file is always approved by the person.
			var gerr error
			if nonLocal, gerr = writeGates([]sqlTarget{t}); gerr != nil {
				return NewErrorResponse(req.ID, ErrPermission, gerr.Error())
			}
		}
		if sb.RequireApproval || len(nonLocal) > 0 {
			if r := h.awaitApproval(ctx, req, sb, "", nil, sqlClassWrite, nonLocal); r != nil {
				return r
			}
		}
	}

	return h.dispatch(ctx, req)
}

// awaitApproval is the one place Handle/handleRunQuery ask for the user's OK
// on a write that the sandbox has already allowed. sqlText/params/class are
// only meaningful for run_query (describeWrite's "run_query" case uses class
// to pick the DML-vs-DDL wording, and a post-approval re-check below
// reclassifies sqlText/params against the file's live sandbox); every other
// tool passes sqlText="" and params=nil and ignores class. Returns nil to
// mean "proceed"; any non-nil *Response is the final answer to send back to
// the caller without dispatching the tool at all.
//
// nonLocal names the connections this write changes that are not the
// workspace's own local files. Approval is forced for those whatever the
// person's "Ask before changes" setting is (the third write gate, #116).
func (h *Handler) awaitApproval(ctx context.Context, req *Request, sb *SandboxConfig, sqlText string, params map[string]any, class sqlClass, nonLocal []string) *Response {
	if h.approver == nil {
		if len(nonLocal) > 0 {
			return NewErrorResponse(req.ID, ErrApprovalNeeded, fmt.Sprintf(
				"%s changes %s, which is not a local file in this workspace, so it always needs your OK in Vibe Studio (even when \"Ask before changes\" is off), and there is no one to ask over MCP. "+
					"Nothing was changed. Open the workspace in Vibe Studio: with its MCP server on, `vibe mcp serve` passes through to it and Studio shows you an approval card, or make the change from Studio's agent chat.",
				req.Method, strings.Join(nonLocal, ", ")))
		}
		return NewErrorResponse(req.ID, ErrApprovalNeeded, fmt.Sprintf(
			"%s needs your OK before it runs: this workspace asks before the agent changes anything (sandbox: %s), and there is no one to ask over MCP. "+
				"Nothing was changed. Changes to any connection other than the workspace's local database always need Vibe Studio: open the workspace there and approve the change. "+
				`To let MCP clients change the local database without asking, turn off "Ask before changes" in Vibe Studio → Settings → Agent.`,
			req.Method, sb.Mode))
	}

	userDB := h.access.DuckDBDB()
	ar := describeWrite(ctx, req, userDB, class)
	if sqlText != "" {
		ar.SQL = sqlText
	}
	if len(nonLocal) > 0 {
		ar.Connections = nonLocal
		ar.Summary = fmt.Sprintf("%s (on %s, not a local file)", ar.Summary, strings.Join(nonLocal, ", "))
	}
	ar.Mode = string(sb.Mode)
	ar.ID = uuid.New().String()

	err := h.approver(ctx, ar)
	switch {
	case err == nil:
		// Round 2 review finding 7: the approver call above can block for
		// minutes with the caller's file lock released (see
		// studio/app.go's enableApprovals), during which SetFilePolicy may
		// have tightened the file's live mode. A human did approve this
		// exact change, but "approved" must still mean "still allowed" the
		// moment it actually runs, not "was allowed when the card first
		// appeared" — so re-check against the CURRENT sandbox (the studio
		// approver refreshes it via SetFilePolicy right before returning
		// nil) rather than trusting the sb loaded before the wait.
		fresh := h.sandboxFor(ctx)
		if err := fresh.Check(req.Method); err != nil {
			return NewErrorResponse(req.ID, ErrPermission, fmt.Sprintf(
				"the agent's access changed while this was waiting for approval: %s", err.Error()))
		}
		if req.Method == "run_query" {
			v := h.classify(ctx, userDB, sqlText, params)
			if err := fresh.checkSQLClass(v.Class, v.Reason); err != nil {
				return NewErrorResponse(req.ID, ErrPermission, fmt.Sprintf(
					"the agent's access changed while this was waiting for approval: %s", err.Error()))
			}
			if _, err := writeGates(v.Targets); err != nil {
				return NewErrorResponse(req.ID, ErrPermission, fmt.Sprintf(
					"a connection changed while this was waiting for approval: %s", err.Error()))
			}
		}
		return nil
	case errors.Is(err, ErrApprovalDeclined):
		return NewErrorResponse(req.ID, ErrApprovalNeeded, fmt.Sprintf(
			"The user declined this change (%s). Nothing was changed. Don't retry it unless the user asks again.", ar.Summary))
	case errors.Is(err, ErrApprovalExpired):
		return NewErrorResponse(req.ID, ErrApprovalNeeded, "No answer in time, so nothing was changed.")
	case errors.Is(err, ErrApprovalCancelled):
		return NewErrorResponse(req.ID, ErrApprovalNeeded, "The file was closed before this change was approved. Nothing was changed.")
	default:
		return NewErrorResponse(req.ID, ErrApprovalNeeded, err.Error())
	}
}

// dispatch routes the request to the appropriate handler method.
func (h *Handler) dispatch(ctx context.Context, req *Request) *Response {
	switch req.Method {
	// Introspect
	case "list_connections":
		return h.handleListConnections(ctx, req)
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
		if strings.Contains(err.Error(), "does not exist") {
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
	return h.RunReadOnlySQLIn(ctx, "", sqlStr, params, maxRows)
}

// ConnectionEngine is the optional engine capability behind a notebook cell's
// or A2UI binding's "connection" (notebook format v2): run a statement with
// that connection's catalog as the default for unqualified names.
type ConnectionEngine interface {
	ExecuteIn(ctx context.Context, connection, sql string, params map[string]any) (*engine.ResultSet, error)
}

// RunReadOnlySQLIn is RunReadOnlySQL with connection (a catalog name; "" is
// the session default) as the default for unqualified table names. The
// statement is classified on a connection pinned to that catalog, so the
// classifier resolves names exactly as the run will, and runs the same way.
func (h *Handler) RunReadOnlySQLIn(ctx context.Context, connection, sqlStr string, params map[string]any, maxRows int) (rs *engine.ResultSet, truncated bool, err error) {
	userDB := h.access.DuckDBDB()
	if userDB == nil {
		return nil, false, fmt.Errorf("DuckDB not available")
	}
	exec := func(ctx context.Context, q string, p map[string]any) (*engine.ResultSet, error) {
		return h.engine.Execute(ctx, q, p)
	}
	if connection != "" {
		ce, ok := h.engine.(ConnectionEngine)
		if !ok {
			return nil, false, fmt.Errorf("connections are not available here")
		}
		exec = func(ctx context.Context, q string, p map[string]any) (*engine.ResultSet, error) {
			return ce.ExecuteIn(ctx, connection, q, p)
		}
	}
	connectErr := h.connectReferenced(ctx, connection, sqlStr)
	v, err := h.classifyIn(ctx, connection, sqlStr, params)
	if err != nil {
		return nil, false, err
	}
	class, reason := v.Class, v.Reason
	if err := h.sandboxFor(ctx).checkSQLClassReadOnly(class, reason); err != nil {
		if connectErr != nil {
			return nil, false, connectErr
		}
		// The user/agent-facing error is one fixed, mode-agnostic message
		// (see ErrReadOnlyBindingDenied) — it never surfaces the policy's
		// reason, which can name a tier requirement that is false for a
		// full-tier caller. The reason is still useful for debugging a denial
		// that looks surprising, so it's logged here at debug level only.
		h.logger.Debug("render-time sql binding denied", "class", class, "reason", reason, "sql", sqlStr)
		return nil, false, err
	}

	if maxRows > 0 {
		trimmed := strings.TrimSuffix(strings.TrimSpace(sqlStr), ";")
		wrapped := fmt.Sprintf("SELECT * FROM (%s) AS _a2ui_limit LIMIT %d", trimmed, maxRows+1)
		if rs, err = exec(ctx, wrapped, params); err == nil {
			rs.Rows = dropDuckLakeInternalRows(rs.Rows)
			if len(rs.Rows) > maxRows {
				rs.Rows = rs.Rows[:maxRows]
				truncated = true
			}
			return rs, truncated, nil
		}
		h.logger.Debug("render-time sql binding: LIMIT push-down not supported for this statement shape, falling back to unwrapped execution", "sql", sqlStr, "wrap_error", err)
	}

	rs, err = exec(ctx, sqlStr, params)
	if err != nil {
		return nil, false, fmt.Errorf("query execution failed: %w", err)
	}
	rs.Rows = dropDuckLakeInternalRows(rs.Rows)
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
	connectErr := h.connectReferenced(ctx, "", sqlStr)
	sb := h.sandboxFor(ctx)
	v := h.classify(ctx, userDB, sqlStr, params)
	class := v.Class
	if err := sb.checkSQLClass(v.Class, v.Reason); err != nil {
		return NewErrorResponse(req.ID, ErrPermission, withConnectError(err, connectErr).Error())
	}
	// The second write gate: every connection the statement changes must be
	// writable.
	nonLocal, err := writeGates(v.Targets)
	if err != nil {
		return NewErrorResponse(req.ID, ErrPermission, "run_query: "+err.Error())
	}

	// Approval gate (#79): run_query asks only when the statement actually
	// changes something (DML) or reaches outside a plain read (DDL/ATTACH/
	// COPY/INSTALL/multi-statement) — never for a SELECT. Runs after
	// checkSQLClass, so a statement the sandbox denies outright never shows
	// a card.
	// The third write gate: the person's OK. It is forced on for anything that
	// is not a local file, whatever the "Ask before changes" setting is.
	if class != sqlClassRead && (sb.RequireApproval || len(nonLocal) > 0) {
		if r := h.awaitApproval(ctx, req, sb, sqlStr, params, class, nonLocal); r != nil {
			return r
		}
	}

	rs, err := h.engine.Execute(ctx, sqlStr, params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("query execution failed: %v", err))
	}

	// Enforce max rows. DuckLake's own metadata catalog never shows in what the
	// agent reads, not even as a name in duckdb_tables() or SHOW ALL TABLES.
	rows := dropDuckLakeInternalRows(rs.Rows)
	if sb.MaxQueryRows > 0 && len(rows) > sb.MaxQueryRows {
		rows = rows[:sb.MaxQueryRows]
	}

	cols, types := rs.NamesAndTypes()

	result := map[string]any{
		"columns": cols,
		// types is the DuckDB type name of each column, parallel to
		// columns — an agent or MCP client reading a run_query result needs
		// it to know "9007199254740993" is an exact BIGINT and
		// "12345678901234567.89" an exact DECIMAL, not a value that lost
		// precision in transit.
		"types": types,
		"rows":  rows,
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
	if budgeted, truncated := budgetRows(rows); truncated != nil {
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
	v := h.classify(ctx, userDB, query, params)
	if v.Class != sqlClassWrite {
		if v.Reason != "" {
			return fmt.Errorf("%s", v.Reason)
		}
		return fmt.Errorf("this operation is not a single, plain write statement — denied")
	}
	if _, err := writeGates(v.Targets); err != nil {
		return err
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
		colDefs = append(colDefs, fmt.Sprintf("%s %s", engine.QuoteIdent(colName), colType))
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
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tblSQL, engine.QuoteIdent(name), colType)
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

// dropDuckLakeInternalRows removes the rows that name a DuckLake metadata
// catalog (__ducklake_metadata_<lake>) in any text column.
func dropDuckLakeInternalRows(rows [][]any) [][]any {
	var out [][]any
	for i, r := range rows {
		hide := false
		for _, c := range r {
			if s, ok := c.(string); ok && isDuckLakeInternal(s) {
				hide = true
				break
			}
		}
		if hide && out == nil {
			out = append(make([][]any, 0, len(rows)), rows[:i]...)
		}
		if !hide && out != nil {
			out = append(out, r)
		}
	}
	if out == nil {
		return rows
	}
	return out
}
