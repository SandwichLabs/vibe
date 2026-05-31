package agents

import "strings"

// HelpEntry is a single help topic.
type HelpEntry struct {
	Topic   string   // short identifier
	Title   string   // human-readable title
	Tags    []string // keywords for matching
	Content string   // full help text (markdown)
}

// HelpEntries contains all built-in help documentation.
var HelpEntries = []HelpEntry{
	{
		Topic: "sql-blocks",
		Title: "SQL Blocks",
		Tags:  []string{"sql", "query", "block", "fenced", "code", "engine", "duckdb", "sqlite", "cache", "result"},
		Content: `# SQL Blocks

SQL blocks are fenced code blocks that execute queries and produce named result sets.

## Syntax

` + "~~~" + `
` + "```sql block_name cache=5m" + `
SELECT * FROM my_table WHERE status = :status
` + "```" + `
` + "~~~" + `

## Parameters

- **block_name** (required): A unique name used to reference this result set in directives and templates
- **cache** (optional): Cache duration (e.g. ` + "`5m`" + `, ` + "`1h`" + `)

## Named Parameters

Use ` + "`:param_name`" + ` in SQL to reference page parameters from the URL or frontmatter.

## Execution

SQL blocks execute top-to-bottom. Each block produces a named ResultSet that can be referenced by directives below it via the ` + "`source`" + ` config key, or by Go templates via ` + "`.Results.block_name`" + `.`,
	},
	{
		Topic: "directives",
		Title: "Directives Overview",
		Tags:  []string{"directive", "table", "chart", "metric", "form", "detail", "component", "render"},
		Content: `# Directives

Directives are single-line tags that render SQL results as interactive components. They must appear on their own line.

## Syntax

` + "`{type: source, option: value}`" + `

The first key is the directive type, its value is the source SQL block name, and remaining keys are options.

## Available Directives

| Directive | Purpose |
|-----------|---------|
| ` + "`{table: source}`" + ` | Renders a data table |
| ` + "`{chart: source, type: bar}`" + ` | Renders a chart |
| ` + "`{metric: source, label: Title}`" + ` | Renders a single metric card |
| ` + "`{form: table, fields: [...]}`" + ` | Renders an input form |
| ` + "`{detail: source}`" + ` | Renders a detail/card view |

Each directive references a SQL block by name via the ` + "`source`" + ` key. The SQL block must appear before the directive in the page.`,
	},
	{
		Topic: "directive-table",
		Title: "Table Directive",
		Tags:  []string{"table", "directive", "sortable", "searchable", "grid", "data"},
		Content: `# Table Directive

Renders a SQL result set as an HTML table.

## Syntax

` + "`{table: source_name, sortable: true, searchable: true}`" + `

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| ` + "`source`" + ` | string | — | Name of the SQL block to render |
| ` + "`sortable`" + ` | bool | false | Enable column sorting |
| ` + "`searchable`" + ` | bool | false | Show a search/filter input |
| ` + "`page_size`" + ` | int | 50 | Rows per page |`,
	},
	{
		Topic: "directive-chart",
		Title: "Chart Directive",
		Tags:  []string{"chart", "directive", "bar", "line", "pie", "scatter", "area", "visualization", "graph"},
		Content: `# Chart Directive

Renders a SQL result set as a Chart.js chart.

## Syntax

` + "`{chart: source_name, type: bar, x: column_name, y: column_name, title: My Chart}`" + `

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| ` + "`source`" + ` | string | — | Name of the SQL block |
| ` + "`type`" + ` | string | bar | Chart type: bar, line, area, scatter, pie |
| ` + "`x`" + ` | string | — | Column for x-axis |
| ` + "`y`" + ` | string | — | Column for y-axis |
| ` + "`title`" + ` | string | — | Chart title |
| ` + "`color`" + ` | string | — | Color override |`,
	},
	{
		Topic: "directive-metric",
		Title: "Metric Directive",
		Tags:  []string{"metric", "directive", "kpi", "number", "card", "inline", "sql"},
		Content: `# Metric Directive

Renders a single value as a styled metric card.

## Syntax

` + "`{metric: source_name, label: Total Revenue, format: currency}`" + `

Or with inline SQL (no separate SQL block needed):

` + "`{metric: label, label: Order Count, sql: \"SELECT COUNT(*) AS value FROM orders\", engine: sqlite}`" + `

## Options

| Option | Type | Description |
|--------|------|-------------|
| ` + "`source`" + ` | string | SQL block name (first row, first column used as value) |
| ` + "`label`" + ` | string | Display label |
| ` + "`format`" + ` | string | Formatter: currency, percent, bytes, duration, number |
| ` + "`sql`" + ` | string | Inline SQL query (overrides source) |
| ` + "`engine`" + ` | string | Engine for inline SQL (default: duckdb) |`,
	},
	{
		Topic: "directive-form",
		Title: "Form Directive",
		Tags:  []string{"form", "directive", "input", "submit", "fields", "insert", "create"},
		Content: `# Form Directive

Renders an HTML form that submits data to a table.

## Syntax

` + "```" + `
{form: table_name, fields: [{name: field1, type: text}, {name: field2, type: number}]}
` + "```" + `

## Field Types

text, number, currency, date, datetime, select, checkbox, textarea, email, url

## Field Options

| Option | Type | Description |
|--------|------|-------------|
| ` + "`name`" + ` | string | Column/field name |
| ` + "`type`" + ` | string | Input type |
| ` + "`required`" + ` | bool | Whether field is required |
| ` + "`options`" + ` | array | Options for select fields |`,
	},
	{
		Topic: "directive-detail",
		Title: "Detail Directive",
		Tags:  []string{"detail", "directive", "card", "list", "single", "record"},
		Content: `# Detail Directive

Renders a SQL result as a key-value detail view (definition list).

## Syntax

` + "`{detail: source_name, layout: card}`" + `

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| ` + "`source`" + ` | string | — | SQL block name |
| ` + "`layout`" + ` | string | card | Layout mode: card or list |

Each row is rendered as a separate detail card with column names as labels and values as content.`,
	},
	{
		Topic: "go-templates",
		Title: "Go Template Syntax",
		Tags:  []string{"template", "go", "range", "if", "loop", "conditional", "format", "function", "variable"},
		Content: `# Go Template Syntax

Vibe pages support full Go ` + "`text/template`" + ` syntax in text blocks. Templates execute after SQL blocks, so query results are available as data.

## Data Context

- ` + "`{{.Results.block_name}}`" + ` — a TemplateResult with .Rows, .Columns, .ColNames
- ` + "`{{.Params.key}}`" + ` — page parameters
- ` + "`{{.Meta.title}}`" + ` — frontmatter metadata

## Loops

` + "```" + `
{{range .Results.data.Rows}}
- {{index . 0}}: {{index . 1}}
{{end}}
` + "```" + `

## Conditionals

` + "```" + `
{{if .Results.alerts.Rows}}
Alerts found: {{count .Results.alerts}}
{{else}}
No alerts.
{{end}}
` + "```" + `

## Built-in Functions

| Function | Example | Description |
|----------|---------|-------------|
| index | {{index .row 0}} | Get value at position |
| cell | {{cell .row 0}} | Nil-safe index |
| col | {{col .Results.data 0}} | Column name by index |
| count | {{count .Results.data}} | Row count |
| sum | {{sum .Results.data 1}} | Sum a column |
| fmt_currency | {{42.5 | fmt_currency}} | → $42.50 |
| fmt_percent | {{0.85 | fmt_percent}} | → 85.0% |
| fmt_number | {{1234 | fmt_number}} | → 1,234 |
| json | {{json .Results.data}} | Marshal to JSON |
| upper / lower | {{"hi" | upper}} | Case conversion |
| add / sub / mul | {{add 1 2}} | Integer math |
| default | {{default "N/A" .val}} | Fallback for nil/empty |
| join | {{join .list ", "}} | Join strings |`,
	},
	{
		Topic: "frontmatter",
		Title: "Page Frontmatter",
		Tags:  []string{"frontmatter", "yaml", "title", "type", "params", "parameters", "refresh", "metadata"},
		Content: `# Page Frontmatter

Pages start with optional YAML frontmatter between ` + "`---`" + ` delimiters.

## Fields

| Field | Type | Description |
|-------|------|-------------|
| ` + "`title`" + ` | string | Page title |
| ` + "`type`" + ` | string | Page type: markdown (default) or html |
| ` + "`refresh`" + ` | string | Auto-refresh interval (e.g. "30s") |
| ` + "`params`" + ` | array | Parameter definitions |

## Parameter Definitions

` + "```yaml" + `
---
title: Sales Report
params:
  - name: month
    type: text
    required: true
    default: "2025-01"
  - name: limit
    type: integer
    default: "100"
---
` + "```" + `

Parameters are passed via URL query string or form submissions and available in SQL blocks as ` + "`:param_name`" + ` and in templates as ` + "`{{.Params.param_name}}`" + `.`,
	},
	{
		Topic: "page-types",
		Title: "Page Types",
		Tags:  []string{"page", "type", "markdown", "html", "raw"},
		Content: `# Page Types

## Markdown (default)

Text nodes are converted from markdown to HTML via goldmark. Supports standard markdown syntax plus Vibe extensions (SQL blocks, directives, Go templates).

## HTML

Set ` + "`type: html`" + ` in frontmatter. Text nodes are rendered as raw HTML without markdown conversion. SQL blocks and directives still work normally.

HTML pages are useful for:
- Pixel-perfect layouts with custom CSS
- Embedding external widgets
- Complex table structures
- Custom styling beyond what markdown offers

Go template syntax works in both page types.`,
	},
	{
		Topic: "vibe-file",
		Title: ".vibe File Format",
		Tags:  []string{"vibe", "file", "format", "container", "encrypted", "sqlcipher", "duckdb", "passphrase"},
		Content: `# .vibe File Format

A .vibe file is an encrypted SQLite database (SQLCipher) that bundles data, pages, queries, and configuration into a single portable file.

## Key Properties

- **Encrypted by default** — AES-256-CBC with PBKDF2-HMAC-SHA512 key derivation
- **Self-contained** — no external dependencies; the file is the app
- **Dual engines** — SQLite for transactional data, DuckDB for analytics
- **Single-user** — no concurrent access or conflict resolution needed

## Internal Tables

All system tables use the ` + "`_vibe_`" + ` prefix:

| Table | Purpose |
|-------|---------|
| _vibe_meta | Key-value metadata (title, version, timestamps) |
| _vibe_pages | Markdown/HTML pages with content and metadata |
| _vibe_queries | Named saved queries |
| _vibe_components | UI component definitions |
| _vibe_assets | Embedded binary files |
| _vibe_duckdb | Serialized DuckDB database blob |

User data tables have no prefix and can be created freely.`,
	},
	{
		Topic: "mcp-tools",
		Title: "MCP Server & Tools",
		Tags:  []string{"mcp", "server", "tool", "agent", "api", "introspect", "schema", "query", "crud"},
		Content: `# MCP Server & Tools

Vibe Studio exposes an MCP (Model Context Protocol) server that AI agents can connect to for reading and modifying .vibe files.

## Starting the Server

In Vibe Studio: Settings → Start MCP Server. The server runs on localhost with a random port and provides an SSE endpoint.

## Available Tools

### Introspect (read-only)
- **introspect_schema** — List all tables and columns
- **describe_table** — Get table schema details
- **list_pages** — List all pages
- **get_page** — Get page content by name
- **list_queries** — List saved queries
- **help_search** — Search built-in help documentation

### Data (read-write)
- **run_query** — Execute SQL (specify engine: sqlite or duckdb)
- **create_record** — Insert a row
- **update_record** — Update rows matching a WHERE clause
- **delete_record** — Delete rows matching a WHERE clause
- **bulk_insert** — Batch insert multiple rows

### Pages (read-write)
- **create_page** — Create a new page
- **update_page** — Update page content
- **delete_page** — Delete a page

### Schema (full access)
- **create_table** — Create a new table
- **add_column** — Add a column to a table
- **drop_table** — Drop a table
- **import_csv** — Import CSV data into a table`,
	},
}

// SearchHelp searches help entries by keyword query. Returns matching entries
// ranked by relevance (number of matching tags + title/content matches).
func SearchHelp(query string) []HelpEntry {
	if query == "" {
		return HelpEntries
	}

	terms := strings.Fields(strings.ToLower(query))

	type scored struct {
		entry HelpEntry
		score int
	}

	var results []scored
	for _, entry := range HelpEntries {
		score := 0
		titleLower := strings.ToLower(entry.Title)
		contentLower := strings.ToLower(entry.Content)

		for _, term := range terms {
			// Tag match (highest weight).
			for _, tag := range entry.Tags {
				if strings.Contains(tag, term) {
					score += 3
				}
			}
			// Title match.
			if strings.Contains(titleLower, term) {
				score += 2
			}
			// Content match.
			if strings.Contains(contentLower, term) {
				score += 1
			}
		}

		if score > 0 {
			results = append(results, scored{entry: entry, score: score})
		}
	}

	// Sort by score descending (simple insertion sort for small slice).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	entries := make([]HelpEntry, len(results))
	for i, r := range results {
		entries[i] = r.entry
	}
	return entries
}
