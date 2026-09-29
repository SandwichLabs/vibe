package agents

import (
	"context"
	"database/sql"
)

// writeToolCategories are the ToolDef categories that ALWAYS write DuckDB
// user data when the tool call succeeds, EXCEPT "data": run_query is also
// category "data" but is very often a plain SELECT, so it needs its own
// classification (in IsWriteCall below) rather than a blanket category
// match. The non-run_query "data" tools (create_record/update_record/
// delete_record/bulk_insert) are unconditionally writes — handled as a
// special case in IsWriteCall, not lumped in here.
//
// "content" (add_query_cell, create_or_update_skill, create_dashboard) is
// deliberately excluded: those tools write SQLite only (write-through, does
// not affect DuckDB user data), in both callers of IsWriteCall — Studio's
// dirty tracking (studio/app.go's agentToolHandler.Handle) and `vibe mcp
// serve`'s debounced saver (cmd/mcp_helpers.go) — so IsWriteCall means
// exactly "wrote DuckDB user data" for every caller, with no per-caller
// carve-out needed on top of it.
var writeToolCategories = map[string]bool{
	"schema": true,
}

// IsWriteCall reports whether req (already dispatched successfully) wrote
// DuckDB user data — moved from cmd/mcp_helpers.go's
// saveHookHandler.wroteData (#81) so Studio's dirty tracking can share it
// instead of duplicating the classification. run_query is reclassified per
// call using IsReadOnlyStatement — the SAME classifier handleRunQuery
// itself uses, so this can never drift from what the handler actually
// decided was safe to execute. Every other "data"-category tool
// (create_record/update_record/delete_record/bulk_insert) has no read form
// at all, so it is unconditionally a write; "schema" tools are likewise
// unconditionally writes (writeToolCategories) — "schema" also contains
// save_query/build_query, which are actually SQLite-only, a known and
// accepted false positive (harmless: it only costs an extra debounced Save
// or a spurious dirty flag). Anything else (introspect, content, etc.) is
// never a write.
func IsWriteCall(ctx context.Context, duckDB *sql.DB, req *Request) bool {
	tool := GetTool(req.Method)
	if tool == nil {
		return false
	}
	if tool.Category == "data" {
		if req.Method != "run_query" {
			return true // create_record/update_record/delete_record/bulk_insert
		}
		sqlText, _ := req.Params["sql"].(string)
		var params map[string]any
		if p, ok := req.Params["params"].(map[string]any); ok {
			params = p
		}
		if duckDB == nil {
			// Can't classify — fail toward treating it as a write rather than
			// silently missing one we couldn't verify was safe to skip.
			return true
		}
		return !IsReadOnlyStatement(ctx, duckDB, sqlText, params)
	}
	return writeToolCategories[tool.Category]
}
