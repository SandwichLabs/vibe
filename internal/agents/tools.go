package agents

// ToolDef defines an agent tool method.
type ToolDef struct {
	Name        string
	Description string
	Category    string // introspect, data, page, schema
	Params      []ParamDef
}

// ParamDef defines a parameter for a tool.
type ParamDef struct {
	Name     string
	Type     string
	Required bool
	Desc     string
}

// Tools contains all available agent tool definitions.
var Tools = []ToolDef{
	// Introspect tools
	{
		Name:        "introspect_schema",
		Description: "List all tables and their columns in the container",
		Category:    "introspect",
		Params:      []ParamDef{},
	},
	{
		Name:        "describe_table",
		Description: "Describe the schema of a specific table",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "table", Type: "string", Required: true, Desc: "Name of the table to describe"},
		},
	},
	{
		Name:        "list_pages",
		Description: "List all pages in the container",
		Category:    "introspect",
		Params:      []ParamDef{},
	},
	{
		Name:        "get_page",
		Description: "Get the content of a specific page",
		Category:    "introspect",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the page to retrieve"},
		},
	},
	{
		Name:        "list_queries",
		Description: "List all named queries in the container",
		Category:    "introspect",
		Params:      []ParamDef{},
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
		Description: "Execute a SQL query and return results",
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
			{Name: "columns", Type: "array", Required: true, Desc: "Column names"},
			{Name: "rows", Type: "array", Required: true, Desc: "Array of row arrays"},
		},
	},

	// Page tools
	{
		Name:        "create_page",
		Description: "Create a new page in the container",
		Category:    "page",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the page"},
			{Name: "content", Type: "string", Required: true, Desc: "Markdown content of the page"},
		},
	},
	{
		Name:        "update_page",
		Description: "Update an existing page",
		Category:    "page",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the page to update"},
			{Name: "content", Type: "string", Required: true, Desc: "New markdown content"},
		},
	},
	{
		Name:        "delete_page",
		Description: "Delete a page from the container",
		Category:    "page",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the page to delete"},
		},
	},

	// Schema tools
	{
		Name:        "create_table",
		Description: "Create a new table in the container",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Name of the table"},
			{Name: "columns", Type: "array", Required: true, Desc: "Column definitions"},
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
		Description: "Drop a table from the container",
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

	// Form Wizard tool (plan §1.6 / §4.3). Generates a bound form spec for a
	// table, seeds humanized field-meta captions, and persists to _vibe_forms.
	{
		Name:        "create_form",
		Description: "Form Wizard: generate a bound form for a table. Supports archetypes columnar | continuous | datasheet | master_detail (with a subform link to a child table), seeds humanized field-meta captions, and persists the spec to _vibe_forms.",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "name", Type: "string", Required: true, Desc: "Form name / title"},
			{Name: "table", Type: "string", Required: true, Desc: "Table the form is bound to"},
			{Name: "archetype", Type: "string", Required: true, Desc: "Form archetype: columnar | continuous | datasheet | master_detail"},
			{Name: "fields", Type: "array", Required: false, Desc: "Bound master fields; objects with a 'name' key. Defaults to all table columns."},
			{Name: "on_submit", Type: "string", Required: false, Desc: "On-submit behavior: refresh | redirect:<page> | noop (default: refresh)"},
			{Name: "subform", Type: "object", Required: false, Desc: "master_detail subform link: {child_table, child_key, parent_key, fields}"},
		},
	},

	// Schema Wizard tool (plan §2.1 / §4.3). Applies a real DuckDB foreign key
	// (child.<child_column> -> parent.<parent_column>) and persists the
	// relationship to _vibe_relationships.
	{
		Name:        "create_relationship",
		Description: "Schema Wizard: create a relationship (foreign key) linking a child table column to a parent table column. Applies a real DuckDB foreign-key constraint (RESTRICT) so orphan child records are rejected, and persists the relationship to _vibe_relationships.",
		Category:    "schema",
		Params: []ParamDef{
			{Name: "parent_table", Type: "string", Required: true, Desc: "The referenced (one) table"},
			{Name: "parent_column", Type: "string", Required: true, Desc: "The referenced column (must be PRIMARY KEY or UNIQUE)"},
			{Name: "child_table", Type: "string", Required: true, Desc: "The referencing (many) table"},
			{Name: "child_column", Type: "string", Required: true, Desc: "The referencing column"},
			{Name: "on_delete", Type: "string", Required: false, Desc: "Referential action on delete: RESTRICT | CASCADE | SET NULL | NO ACTION (DuckDB enforces RESTRICT)"},
			{Name: "on_update", Type: "string", Required: false, Desc: "Referential action on update (stored as metadata)"},
		},
	},
	{
		Name:        "list_relationships",
		Description: "List all stored foreign-key relationships in the container.",
		Category:    "introspect",
		Params:      []ParamDef{},
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
			{Name: "conditions", Type: "array", Required: false, Desc: "QBE filter conditions: objects {column, op, value}"},
			{Name: "combinator", Type: "string", Required: false, Desc: "How to join conditions: AND | OR (default AND)"},
			{Name: "columns", Type: "array", Required: false, Desc: "Projection column names (default: all columns)"},
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
