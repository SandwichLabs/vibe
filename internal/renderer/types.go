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
	ColumnFmt map[string]string // Evidence-compatible format codes per column name
	Result    *ResultSet
}

// ChartConfig holds chart directive configuration.
type ChartConfig struct {
	Source string
	Type   string // bar, line, area, scatter, pie
	X      string
	Y      string
	XFmt   string // Evidence-compatible format code for X axis labels
	YFmt   string // Evidence-compatible format code for Y axis labels
	Title  string
	Color  string
	Result *ResultSet
}

// MetricConfig holds metric directive configuration.
type MetricConfig struct {
	Label  string
	Format string // number, currency, percent, bytes, duration
	Fmt    string // Evidence-compatible format code (e.g., num0, usd2, pct1)
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

	// Access-parity bound-control metadata (plan §1.4).
	ControlType string // combo, lookup, checkbox, date, datetime, textarea, ...
	LookupSQL   string // SQL resolved server-side to populate combo/lookup options
	ValueCol    string // column whose value is submitted (the hidden/stored value)
	DisplayCol  string // column whose text is shown to the user
	ControlTip  string // help text surfaced as the control's title attribute
}

// SelectOption represents an option in a select field.
type SelectOption struct {
	Value string
	Label string
}

// FormConfig holds form directive configuration.
type FormConfig struct {
	Table     string
	Fields    []FormField
	OnSubmit  string // refresh | redirect:<page_id> | noop
	PageID    string
	FormID    string
	Archetype string // columnar | continuous | datasheet | master_detail (plan §1.5)
}

// DetailConfig holds detail directive configuration.
type DetailConfig struct {
	Source string
	Layout string // card | list
	Result *ResultSet
}

// SubformConfig holds master-detail subform directive configuration (plan §1.5).
// The child datasheet is auto-filtered to the current master record via the FK
// link child.<ChildKey> = master.<ParentKey> = MasterKey.
//
// STUB (TDD RED P1.6): shape only — no renderer behavior implemented yet.
type SubformConfig struct {
	ChildTable string // child table/source bound to the subform
	ParentKey  string // master record key column (e.g. orders.id)
	ChildKey   string // child FK column (e.g. order_items.order_id)
	MasterKey  any    // the current master record's key value
	Title      string // optional caption
}

// RenderContext provides context for rendering.
type RenderContext struct {
	PageID    string
	PageTitle string
	BaseURL   string
}
