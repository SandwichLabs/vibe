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
