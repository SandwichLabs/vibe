package agents

// ToolDef defines an agent tool method.
type ToolDef struct {
	Name        string
	Description string
	Category    string // introspect, data, content, schema
	Params      []ParamDef
	// Modes restricts which agent surfaces advertise this tool. Empty means
	// "every mode" (chat, mcp, task) — the vast majority of tools. A non-empty
	// list is a closed allow-list of mode names; today the only mode that
	// matters is "task" (#66's notebook cell-authoring loop): preview_query,
	// write_cell, and need_more_info are task-workflow tools that must never
	// appear in Studio chat's tool list or be callable over MCP — see
	// ListToolsForMode, which both internal/llm's chat runner and
	// internal/mcp's server call instead of the unfiltered ListTools/Tools.
	Modes []string
}

// ParamDef defines a parameter for a tool.
type ParamDef struct {
	Name     string
	Type     string // string | boolean | number | integer | object | array
	Required bool
	Desc     string
	// Items describes the schema of each element when Type == "array". Strict
	// JSON Schema consumers (notably Gemini) reject a bare {"type":"array"}
	// with no "items", so every array ParamDef should set this — see
	// ParamsToJSONSchema in schema.go, which is the single place both
	// internal/llm's fantasy tool wrapper and internal/mcp's MCP bridge build
	// a tool's input schema from Params, so they stay byte-identical.
	Items *ParamDef
	// Fields lists known sub-properties when Type == "object" (either a
	// top-level param or an array's Items). Optional: an object/array-of-object
	// with no declared Fields still renders as a bare {"type":"object"}.
	Fields []ParamDef
}

// Tools contains all available agent tool definitions.
var Tools = []ToolDef{
	// Introspect tools
	{
		Name:        "list_connections",
		Description: "List the workspace's connections (places data lives): name, type, status, read_only, location (local file or remote), the storage scope for object stores, and the workspace data/ folder. Use the connection name as the catalog in SQL (name.schema.table). Never returns DSNs, credentials or paths outside the workspace.",
		Category:    "introspect",
		Params:      []ParamDef{},
	},
	{
		Name:        "introspect_schema",
		Description: "List the tables of a connection (catalog). Omit connection for the default one; tables of any other connection are returned fully qualified (catalog.schema.table) so they can be used as-is in other tools.",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "connection", Type: "string", Required: false, Desc: "Connection (catalog) name; defaults to the workspace default"},
		},
	},
	{
		Name:        "describe_table",
		Description: "Describe the schema of a specific table",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Name of the table to describe; catalog.schema.table for a table in another connection"},
		},
	},
	{
		Name:        "list_notebooks",
		Description: "List every notebook in the workspace: id, title, cell count, and last-updated time.",
		Category:    "introspect",
		Params:      []ParamDef{},
	},
	{
		Name:        "get_notebook",
		Description: "Get one notebook's cells as data: each cell's type plus its raw sql/markdown source (or a title/component-count summary for an a2ui cell) — never rendered output. Long cell sources are truncated.",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "id", Type: "string", Required: true, Desc: "Notebook id, from list_notebooks"},
		},
	},
	{
		Name:        "list_queries",
		Description: "List all saved queries in the workspace",
		Category:    "introspect",
		Params:      []ParamDef{},
	},
	{
		Name:        "list_skills",
		Description: "List the Skills available in this workspace: built-in skills plus any user- or import-authored ones in the workspace (skills/<name>/SKILL.md). Each entry gives a name and one-line description; call read_skill with a name to fetch its full instructions. Skills bound to a table only appear once that table exists and has rows.",
		Category:    "introspect",
		Params:      []ParamDef{},
	},
	{
		Name:        "read_skill",
		Description: "Fetch the full instructions for one Skill by name (see list_skills, or the <skills> block in the system prompt). Call this whenever a task matches a skill's description before proceeding.",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Skill name, e.g. \"data-profiling\" or \"workspace-sales\""},
		},
	},
	{
		Name:        "create_or_update_skill",
		Description: "Author or update a Skill: domain context (business rules, table meaning, terminology) stored in the workspace (skills/<name>/SKILL.md) and loaded on demand by the agent. Upserts by name. Cannot use a built-in skill's name or the \"task-\" prefix (reserved).",
		Category:    "content",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Lowercase kebab-case name, e.g. \"revenue-rules\" (routing key; must not shadow a built-in or start with \"task-\")"},
			{Name: "description", Type: "string", Required: true, Desc: "One-line description advertised in the system prompt (<= 300 characters)"},
			{Name: "instructions", Type: "string", Required: true, Desc: "Full markdown instructions, fetched on demand via read_skill"},
			{Name: "table_name", Type: "string", Required: false, Desc: "Optional: gate this skill so it only loads once this table exists and has at least one row"},
			{Name: "rev", Type: "string", Required: false, Desc: "Optional: the \"rev\" that list_skills or read_skill returned for this skill. If the skill changed since, the save is refused instead of overwriting the change"},
		},
	},
	{
		Name:        "help_search",
		Description: "Search built-in Vibe documentation for help on features, syntax, directives, templates, and more. Returns matching help topics with full content. Call with an empty query to list all topics.",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "query", Type: "string", Required: false, Desc: "Search keywords (e.g. 'chart directive', 'go template range', 'sql block')"},
		},
	},

	// Data tools
	{
		Name:        "run_query",
		Description: "Execute a SQL query and return results. Gated per statement by an allow-list, not just by sandbox category. Reads (SELECT, WITH, SHOW, DESCRIBE, SUMMARIZE, EXPLAIN but not EXPLAIN ANALYZE, a read-style PRAGMA) over any connection are allowed in every tier; files may be read only with a literal absolute path inside the workspace data/ folder, and URLs only inside a storage connection's scope. INSERT/UPDATE/DELETE additionally need the read-write or full tier. CREATE/DROP/ALTER and COPY ... TO a data/ file need the full tier. A write also needs the target connection to be writable (read_only: false) and the user's approval, which is always required for a connection that is not a local file. Never allowed in any tier: ATTACH/DETACH/USE, SET/RESET/PRAGMA changes, INSTALL/LOAD, secrets, EXPORT/IMPORT DATABASE, EXPLAIN ANALYZE, raw passthroughs (postgres_query, postgres_execute, ...), settings and environment functions. Every statement of a script is checked.",
		Category:    "data",
		Params: []ParamDef{
			{Name: "sql", Type: "string", Required: true, Desc: "SQL query to execute"},
			{Name: "params", Type: "object", Required: false, Desc: "Named parameters for the query"},
		},
	},
	{
		Name:        "create_record",
		Description: "Insert a new record into a table",
		Category:    "data",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Table to insert into"},
			{Name: "values", Type: "object", Required: true, Desc: "Column-value pairs for the new record"},
		},
	},
	{
		Name:        "update_record",
		Description: "Update existing records in a table",
		Category:    "data",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Table to update"},
			{Name: "values", Type: "object", Required: true, Desc: "Column-value pairs to set"},
			{Name: "where", Type: "string", Required: true, Desc: "WHERE clause for filtering records"},
		},
	},
	{
		Name:        "delete_record",
		Description: "Delete records from a table",
		Category:    "data",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Table to delete from"},
			{Name: "where", Type: "string", Required: true, Desc: "WHERE clause for filtering records"},
		},
	},
	{
		Name:        "bulk_insert",
		Description: "Insert multiple records into a table",
		Category:    "data",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Table to insert into"},
			{Name: "columns", Type: "array", Required: true, Desc: "Column names", Items: &ParamDef{Type: "string"}},
			{Name: "rows", Type: "array", Required: true, Desc: "Array of row arrays", Items: &ParamDef{Type: "array", Items: &ParamDef{Type: "string"}}},
		},
	},

	// Schema tools
	{
		Name:        "create_table",
		Description: "Create a new table in a writable connection (the default connection unless the name is qualified)",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the table"},
			{Name: "columns", Type: "array", Required: true, Desc: "Column definitions", Items: &ParamDef{Type: "object", Fields: []ParamDef{
				{Name: "name", Type: "string", Required: true, Desc: "Column name"},
				{Name: "type", Type: "string", Required: true, Desc: "DuckDB column type, e.g. TEXT, INTEGER, DOUBLE, DATE, BOOLEAN. Constraints (PRIMARY KEY, NOT NULL) may be embedded directly, e.g. \"INTEGER PRIMARY KEY\"."},
			}}},
		},
	},
	{
		Name:        "add_column",
		Description: "Add a column to an existing table",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Table to modify"},
			{Name: "name", Type: "string", Required: true, Desc: "Column name"},
			{Name: "type", Type: "string", Required: true, Desc: "Column type"},
		},
	},
	{
		Name:        "drop_table",
		Description: "Drop a table from a writable connection (the default connection unless the name is qualified)",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the table to drop"},
		},
	},
	{
		Name:        "import_csv",
		Description: "Import CSV data into a table",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Target table name"},
			{Name: "data", Type: "string", Required: true, Desc: "CSV data as a string"},
			{Name: "has_header", Type: "boolean", Required: false, Desc: "Whether the CSV has a header row (default: true)"},
		},
	},

	// Query Wizard tool (plan §2.4 / §4.3). Builds a named query from a QBE-style
	// filter spec and persists it to the _vibe_queries registry.
	{
		Name:        "build_query",
		Description: "Query Wizard (QBE): build and save a named query over a table from SQL-free filter conditions. Conditions are objects {column, op, value}; op is one of eq | neq | lt | gt | lte | gte | like | is_null | is_not_null. The rendered SQL is persisted to the named-query registry and runnable via run_query.",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name/title for the saved query"},
			{Name: "table", Type: "string", Required: true, Desc: "Table the query selects from"},
			{Name: "conditions", Type: "array", Required: false, Desc: "QBE filter conditions: objects {column, op, value}", Items: &ParamDef{Type: "object", Fields: []ParamDef{
				{Name: "column", Type: "string", Required: true, Desc: "Column to filter on"},
				{Name: "op", Type: "string", Required: true, Desc: "eq | neq | lt | gt | lte | gte | like | is_null | is_not_null"},
				{Name: "value", Type: "string", Required: false, Desc: "Comparison value (ignored for is_null/is_not_null)"},
			}}},
			{Name: "combinator", Type: "string", Required: false, Desc: "How to join conditions: AND | OR (default AND)"},
			{Name: "columns", Type: "array", Required: false, Desc: "Projection column names (default: all columns)", Items: &ParamDef{Type: "string"}},
		},
	},

	// propose_schema (ported from internal/llm's wizard-only tool, #63). This
	// is a draft-only tool: it never touches the database. There is no
	// dedicated diagram screen — the draft renders inline in chat (one
	// heading + column table per proposed table, plus a relationships
	// table), the same way present_data renders. The model asks the user to
	// confirm in a chat reply, then builds the draft itself by calling
	// create_table once per table. It stays in the "introspect" category so
	// it is available even in read-only sandboxes.
	{
		Name:        "propose_schema",
		Description: "PROPOSE a draft schema — tables with friendly field types plus the relationships linking them — rendered inline in chat as a readable table-by-table summary for the user to review. This does NOT modify the database. After the user confirms in chat (e.g. \"yes\", \"build it\"), create the tables yourself with create_table, one call per table in dependency order (parent tables before the children that reference them), declaring any Link/relationship as an inline foreign key in the child column's type (e.g. \"INTEGER REFERENCES customers(id)\"). Always send the COMPLETE current draft (all tables), not just the changed ones.",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "tables", Type: "array", Required: true, Desc: "Every table in the draft: objects {name, columns: [{name, type, primary_key, not_null}]}. Start every table with an ID primary-key column. Friendly field types: ID, Text, Long text, Number, Whole number, Money, Date, Time, Yes/No, Link, Pick (a raw DuckDB type is also accepted).", Items: &ParamDef{Type: "object", Fields: []ParamDef{
				{Name: "name", Type: "string", Required: true, Desc: "Table name, snake_case plural (e.g. service_calls)"},
				{Name: "columns", Type: "array", Required: true, Desc: "The table's columns. Start every table with an ID primary-key column.", Items: &ParamDef{Type: "object", Fields: []ParamDef{
					{Name: "name", Type: "string", Required: true, Desc: "Column name, snake_case"},
					{Name: "type", Type: "string", Required: true, Desc: "Friendly field type: ID, Text, Long text, Number, Whole number, Money, Date, Time, Yes/No, Link, Pick (a raw DuckDB type is also accepted)"},
					{Name: "primary_key", Type: "boolean", Required: false, Desc: "True for the table's ID column"},
					{Name: "not_null", Type: "boolean", Required: false, Desc: "Whether the column is required"},
				}}},
			}}},
			{Name: "relationships", Type: "array", Required: false, Desc: "Foreign-key links between tables in the draft: objects {child_table, child_column, parent_table, parent_column, optional}.", Items: &ParamDef{Type: "object", Fields: []ParamDef{
				{Name: "child_table", Type: "string", Required: true, Desc: "The table holding the Link column"},
				{Name: "child_column", Type: "string", Required: true, Desc: "The Link column on the child table"},
				{Name: "parent_table", Type: "string", Required: true, Desc: "The table being linked to"},
				{Name: "parent_column", Type: "string", Required: false, Desc: "The linked column on the parent (usually id)"},
				{Name: "optional", Type: "boolean", Required: false, Desc: "True when the link may be empty"},
			}}},
			{Name: "note", Type: "string", Required: false, Desc: "One short line summarizing the design; used as the draft's title."},
		},
	},

	// present_data (#67). Validates an A2UI page (internal/a2ui) and hands the
	// validated page straight back as the tool result — it writes nothing, so
	// it stays in the "introspect" category and is available in every
	// sandbox, including read-only. The Studio agent panel renders a
	// successful result inline in the chat transcript. Its sql-bound
	// components (stat/table/barChart/lineChart with a "sql" field) are NOT
	// executed here: the Vue renderer runs each one through RunA2UIQuery,
	// which re-enters this same per-statement SQL classifier via run_query —
	// present_data itself never touches the database.
	{
		Name:        "present_data",
		Description: "Render an A2UI page inline in the current reply. Call this WHENEVER you answer a data-driven question with a chart, table, or stat rather than plain prose — do not wait for the user to ask for a chart. This does not save anything; use inline data (rows/value/bars/points) rather than sql, since you already have the values and the reply is a one-off. See the <a2ui> system prompt block for the full component schema.",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "page", Type: "object", Required: true, Desc: `An A2UI page: {"title": "...", "components": [...]}. See the <a2ui> system prompt block for the component schema.`},
		},
	},

	// save_query (ported from internal/llm's wizard-only tool, #63). Unlike
	// build_query (QBE conditions rendered to SQL), this persists an
	// already-written SQL string as-is, mirroring studio's SaveQueryOn binding.
	// No "description" param: queries.Query (the _vibe_queries row type) has
	// no description column to persist it into (review fix — a param that
	// silently did nothing was worse than no param).
	{
		Name:        "save_query",
		Description: "Save a named SQL query for reuse. The SQL may be any valid DuckDB query; it is persisted as-is (not rendered from QBE conditions — use build_query for that).",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "title", Type: "string", Required: true, Desc: "Name for the saved query."},
			{Name: "sql", Type: "string", Required: true, Desc: "The SQL the query runs."},
		},
	},

	// --- Task mode (#66 part B): notebook cell authoring. preview_query,
	// write_cell, and need_more_info are Modes: []string{"task"} — visible
	// only to internal/llm's Task() loop, never to chat or MCP (see
	// ListToolsForMode). They are "introspect" category so they're never
	// denied by sandbox mode: previewing/proposing a cell never mutates the
	// database, and the user still has to click Apply for anything to be
	// persisted.

	{
		Name:        "preview_query",
		Description: "Run a SELECT (read-only) query and preview its shape before committing it as a notebook cell: row count, columns with types, a sample of rows (budgeted to fit the response), and a suggestedView (table/bar/line/stat; area/columns/rings/radar/sankey/heatmap also listed in viewFit) computed from the actual result shape. ALWAYS preview a query before calling write_cell with cell_type \"sql\" — write_cell rejects sql it has not been shown working.",
		Category:    "introspect",
		Modes:       []string{"task"},
		Params: []ParamDef{
			{Name: "sql", Type: "string", Required: true, Desc: "A single read-only statement (SELECT, WITH...SELECT, SHOW, DESCRIBE, EXPLAIN, SUMMARIZE)"},
			{Name: "connection", Type: "string", Required: false, Desc: "Connection (catalog) the cell runs on; unqualified table names resolve there. Defaults to the workspace default. See list_connections."},
		},
	},
	{
		Name:        "write_cell",
		Description: "Terminal call: record the notebook cell you are proposing and end the turn. cell_type \"sql\" requires non-empty sql that you have already run through preview_query this turn (not remembered SQL, not a lightly-edited copy — re-preview after every edit). cell_type \"markdown\" requires non-empty markdown text (put prose here, not in explanation).",
		Category:    "introspect",
		Modes:       []string{"task"},
		Params: []ParamDef{
			{Name: "cell_type", Type: "string", Required: true, Desc: "\"sql\" or \"markdown\""},
			{Name: "sql", Type: "string", Required: false, Desc: "The previewed SQL (sql cells only)"},
			{Name: "connection", Type: "string", Required: false, Desc: "The connection the SQL was previewed on (sql cells only); must match preview_query's connection"},
			{Name: "view", Type: "string", Required: false, Desc: "table | bar | line | stat | area | columns | rings | radar | sankey | heatmap (sql cells only; defaults to preview_query's suggestedView)"},
			{Name: "label", Type: "string", Required: false, Desc: "Short, specific label — a chart title, not \"Query 1\" (sql cells only)"},
			{Name: "markdown", Type: "string", Required: false, Desc: "The cell's markdown text (markdown cells only)"},
			{Name: "explanation", Type: "string", Required: false, Desc: "One or two sentences explaining the proposal, shown alongside it — never the cell's own content"},
		},
	},
	{
		Name:        "need_more_info",
		Description: "Terminal call: decline to write a cell and end the turn, because the loaded tables genuinely cannot answer the request (not because the SQL is hard). Requires at least one specific question, or non-empty notes/missing_data explaining what's missing and why — a call with none of those is rejected. Only call this after you have already checked with introspect_schema/describe_table.",
		Category:    "introspect",
		Modes:       []string{"task"},
		Params: []ParamDef{
			{Name: "questions", Type: "array", Required: false, Desc: "Up to three specific questions naming the missing table/column/vendor export", Items: &ParamDef{Type: "string"}},
			{Name: "missing_data", Type: "array", Required: false, Desc: "What data is missing", Items: &ParamDef{Type: "string"}},
			{Name: "notes", Type: "string", Required: false, Desc: "Any additional explanation"},
		},
	},

	// --- Chat/MCP-visible notebook-authoring tools (#67 deferred part).
	// Unlike write_cell (task-only, proposal-and-apply), these persist
	// directly — the agent panel's chat surface has no proposal dialog, so
	// the user's ask ("add a dashboard of X to my notebook") is the
	// confirmation. Category "content" (read-write and full): both append a
	// cell to a notebook file.

	{
		Name:        "add_query_cell",
		Description: "Append a SQL notebook cell to a notebook. sql must be a single read-only statement (classified the same way a live A2UI binding is — INSERT/UPDATE/DELETE/DDL are rejected). Appends to notebook_id if given, else the notebook currently open in Studio, else creates a new notebook titled from label.",
		Category:    "content",
		Params: []ParamDef{
			{Name: "notebook_id", Type: "string", Required: false, Desc: "Target notebook id. Omit to use the notebook currently open in Studio, or to create a new one if none is open."},
			{Name: "sql", Type: "string", Required: true, Desc: "A single read-only SQL statement"},
			{Name: "connection", Type: "string", Required: false, Desc: "Connection (catalog) the cell runs on; must be a registered connection (see list_connections). Defaults to the workspace default."},
			{Name: "view", Type: "string", Required: false, Desc: "table | bar | line | stat | area | columns | rings | radar | sankey | heatmap (default: auto-fit to the result shape the first time it runs)"},
			{Name: "label", Type: "string", Required: false, Desc: "Short label for the cell (also used as the new notebook's title, if one is created)"},
		},
	},
	{
		Name:        "create_dashboard",
		Description: "Append an A2UI dashboard (a page of one or more live/inline components) as a new cell in a notebook. See the <a2ui> system prompt block for the component schema. Appends to notebook_id if given, else the notebook currently open in Studio, else creates a new notebook titled from page.title.",
		Category:    "content",
		Params: []ParamDef{
			{Name: "notebook_id", Type: "string", Required: false, Desc: "Target notebook id. Omit to use the notebook currently open in Studio, or to create a new one if none is open."},
			{Name: "page", Type: "object", Required: true, Desc: `An A2UI page: {"title": "...", "components": [...]}. See the <a2ui> system prompt block for the component schema.`},
		},
	},
}

// toolIndex is a lookup map built at init time.
var toolIndex map[string]*ToolDef

func init() {
	toolIndex = make(map[string]*ToolDef, len(Tools))
	for i := range Tools {
		toolIndex[Tools[i].Name] = &Tools[i]
	}
}

// GetTool returns the tool definition for the given name, or nil if not found.
func GetTool(name string) *ToolDef {
	return toolIndex[name]
}

// ListTools returns all available tool definitions.
func ListTools() []ToolDef {
	result := make([]ToolDef, len(Tools))
	copy(result, Tools)
	return result
}

// Mode names for ListToolsForMode / ToolDef.Modes.
const (
	ModeChat = "chat"
	ModeMCP  = "mcp"
	ModeTask = "task"
)

// TaskAllowedToolNames is the CLOSED allow-list of tools task mode may see
// or dispatch (#66 part B review fix, CRITICAL): task mode never mutates
// anything directly — every proposal goes through write_cell/need_more_info,
// and only the user's own Apply click (a separate write path: SaveNotebook)
// commits it. Before this fix, ListToolsForMode(ModeTask) returned every
// Modes-unrestricted tool in the WHOLE registry (introspect_schema alongside
// create_table, drop_table, bulk_insert, import_csv, add_query_cell, ...)
// because "empty Modes = visible everywhere including task" was too broad —
// combined with AgentTask running the file's own (often "full") sandbox
// mode, a task turn could call drop_table directly from prompt text alone.
// Task mode's toolset is now build ONLY from this explicit list — deliberately
// NOT derived from ToolDef.Modes/Category, so a future unrelated tool added
// to the registry can never silently become task-reachable.
var TaskAllowedToolNames = map[string]bool{
	"introspect_schema": true,
	"describe_table":    true,
	"run_query":         true,
	"list_skills":       true,
	"read_skill":        true,
	"preview_query":     true,
	"write_cell":        true,
	"need_more_info":    true,
}

// ListToolsForMode returns the tools visible to a given agent surface.
// mode == ModeTask returns EXACTLY TaskAllowedToolNames (intersected with
// the live registry) — never "every unrestricted tool" (see
// TaskAllowedToolNames's doc comment for why that was wrong). For every
// other mode (ModeChat, ModeMCP, ""), a ToolDef with an empty Modes list is
// visible; one with a non-empty Modes list (today only the task-only
// terminal/preview tools) is hidden, since those tools' Modes is
// ["task"] and mode != "task" here.
func ListToolsForMode(mode string) []ToolDef {
	all := ListTools()
	out := make([]ToolDef, 0, len(all))
	for _, td := range all {
		if mode == ModeTask {
			if TaskAllowedToolNames[td.Name] {
				out = append(out, td)
			}
			continue
		}
		if len(td.Modes) == 0 {
			out = append(out, td)
			continue
		}
		for _, m := range td.Modes {
			if m == mode {
				out = append(out, td)
				break
			}
		}
	}
	return out
}

// IsModeVisible reports whether a tool named name is visible in the given
// mode (see ListToolsForMode). Used by Handler.Handle so task-only dispatch
// is enforced a SECOND time, independent of whatever tool list the caller
// advertised to the model — a caller that bypasses the runner/MCP tool list
// and dispatches "drop_table" (or any tool outside TaskAllowedToolNames)
// directly while mode == ModeTask is still denied here.
func IsModeVisible(name, mode string) bool {
	td := GetTool(name)
	if td == nil {
		return false
	}
	if mode == ModeTask {
		return TaskAllowedToolNames[td.Name]
	}
	if len(td.Modes) == 0 {
		return true
	}
	for _, m := range td.Modes {
		if m == mode {
			return true
		}
	}
	return false
}
