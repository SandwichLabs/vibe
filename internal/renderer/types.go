package renderer

// Column represents a data column.
type Column struct {
	Name string
	Type string
}

// ResultSet represents query results for rendering.
type ResultSet struct {
	Columns []Column
	Rows    [][]any
}

// TableConfig holds table directive configuration.
type TableConfig struct {
	Source     string
	PageSize  int
	Sortable  bool
	Searchable bool
	Result    *ResultSet
}

// ChartConfig holds chart directive configuration.
type ChartConfig struct {
	Source string
	Type   string // bar, line, area, scatter, pie
	X      string
	Y      string
	Title  string
	Color  string
	Result *ResultSet
}

// MetricConfig holds metric directive configuration.
type MetricConfig struct {
	Label  string
	Format string // number, currency, percent, bytes, duration
	Value  any
}

// FormField holds form field configuration.
type FormField struct {
	Name     string
	Label    string
	Type     string // text, number, currency, date, datetime, select, checkbox, textarea, email, url
	Default  any
	Required bool
	Source   string // SQL for select options
	Options  []SelectOption
}

// SelectOption represents an option in a select field.
type SelectOption struct {
	Value string
	Label string
}

// FormConfig holds form directive configuration.
type FormConfig struct {
	Table    string
	Fields   []FormField
	OnSubmit string // refresh | redirect:<page_id> | noop
	PageID   string
	FormID   string
}

// DetailConfig holds detail directive configuration.
type DetailConfig struct {
	Source string
	Layout string // card | list
	Result *ResultSet
}

// RenderContext provides context for rendering.
type RenderContext struct {
	PageID    string
	PageTitle string
	BaseURL   string
}
