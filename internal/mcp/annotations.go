package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/SandwichLabs/vibe/internal/agents"
)

// connectionReaders are introspect tools that look at a connection's own
// catalog (and may connect to it on first use), so unlike the other
// introspect tools they can reach a remote system.
var connectionReaders = map[string]bool{
	"introspect_schema": true,
	"describe_table":    true,
}

// localWriters are data/schema-category tools that write only workspace files
// (queries/*.sql), never a connection, so they do not reach outside the
// workspace folder.
var localWriters = map[string]bool{
	"save_query":  true,
	"build_query": true,
}

// idempotentTools are the writes where sending the same call twice leaves the
// same state as sending it once: a replace of a whole artifact or skill (the
// second call is refused on its stale rev and changes nothing), an update to
// fixed values, and a delete by condition. Appends (cells, rows, run_query)
// and DDL are not listed.
var idempotentTools = map[string]bool{
	"write_artifact":         true,
	"create_or_update_skill": true,
	"update_record":          true,
	"delete_record":          true,
}

// annotationsFor derives a tool's MCP annotations from its category, the one
// source of truth for what a tool may do:
//
//   - introspect: read-only, not destructive, idempotent.
//   - content: writes files in the workspace folder; not read-only, not
//     destructive (a replace needs the rev it read, so it cannot clobber).
//   - data and schema: may change or drop what a connection holds, so
//     destructive. run_query is here too: its SQL may be a write.
//
// Content tools are not destructive on purpose: a replace needs the rev the
// caller read, so it cannot clobber a newer version, and a create refuses an
// existing name.
//
// openWorldHint says whether the tool reaches outside the workspace folder.
// Content and most introspect tools stay inside it (false), as do save_query
// and build_query, which write only queries/*.sql. Other data and schema
// tools run on a connection, which may be a remote database or bucket, and
// introspect_schema / describe_table read a connection's catalog, so those are
// true. A tool that merely classifies SQL against a connection (add_query_cell)
// does not change or read its data and stays false.
func annotationsFor(td agents.ToolDef) mcp.ToolAnnotation {
	readOnly := td.Category == "introspect"
	destructive := td.Category == "data" || td.Category == "schema"
	openWorld := (destructive && !localWriters[td.Name]) || connectionReaders[td.Name]
	idempotent := readOnly || idempotentTools[td.Name]
	return mcp.ToolAnnotation{
		ReadOnlyHint:    mcp.ToBoolPtr(readOnly),
		DestructiveHint: mcp.ToBoolPtr(destructive),
		IdempotentHint:  mcp.ToBoolPtr(idempotent),
		OpenWorldHint:   mcp.ToBoolPtr(openWorld),
	}
}
