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
//
// #69 retired the page system (markdown + named SQL blocks + directives +
// Go templates + frontmatter + page types) along with every topic that
// documented it — those topics described syntax that no longer parses or
// renders anywhere in the product. Notebooks (#66) and A2UI (#67) are the
// only content-authoring surface now; see the "notebooks" and "a2ui"
// topics below. The "mcp-tools" topic is generated from the live agents.Tools
// registry (buildMCPToolsEntry, below) so it can never drift out of sync
// with what the server actually exposes the way the old hand-written list
// did (it still listed list_pages/get_page/create_page/update_page/
// delete_page long after #69 removed them).
var HelpEntries = []HelpEntry{
	{
		Topic: "notebooks",
		Title: "Notebooks",
		Tags:  []string{"notebook", "cell", "sql", "markdown", "a2ui", "dashboard", "view", "table", "bar", "line", "stat"},
		Content: `# Notebooks

A notebook is a title plus an ordered list of cells, saved as a ` + "`notebooks/*.vnb`" + ` file in the workspace. There is no page markup, no directives, and no inter-cell references — the connections are the only shared state.

## Cell types

- **sql** — a single read-only SQL statement. Renders as one of four views: ` + "`table`" + `, ` + "`bar`" + `, ` + "`line`" + `, ` + "`stat`" + `. Until a view is explicitly chosen, it auto-fits to the result shape the first time the cell runs.
- **markdown** — plain markdown prose. No directives, no named SQL blocks, no Go template syntax.
- **a2ui** — an A2UI page (see the "a2ui" topic): one or more components laid out in a single cell, e.g. a small dashboard.

## Read-only cells, live outputs

A sql or a2ui cell's ` + "`sql`" + ` is always read-only (INSERT/UPDATE/DELETE/DDL are rejected at run time, the same classifier a live A2UI binding uses). Outputs are never persisted — every data cell re-runs live when the notebook is opened; only the cell's source/view/page is stored.

## Authoring

- **` + "`add_query_cell`" + `** appends a single sql cell to a notebook (creates one if none is targeted/open).
- **` + "`create_dashboard`" + `** appends a whole A2UI page (multiple components) as one a2ui cell.
- **` + "`list_notebooks`" + `** / **` + "`get_notebook`" + `** are the read-only path: list every notebook, or fetch one notebook's cells as data (raw sql/markdown source, or a title/component-count summary for an a2ui cell) — never rendered output.

There is no create_page/update_page/get_page/list_pages tool; those were retired with the page system.`,
	},
	{
		Topic: "a2ui",
		Title: "A2UI Components",
		Tags:  []string{"a2ui", "component", "dashboard", "chart", "table", "stat", "color", "schema"},
		Content: `# A2UI Components

A2UI is a closed, validated component schema for a small dashboard: ` + "`{ title, components[] }`" + `. Components are a flat list, nested by a ` + "`parent`" + ` id rather than JSON nesting.

## Component types

` + "`heading`" + `, ` + "`text`" + `, ` + "`divider`" + `, ` + "`stat`" + `, ` + "`statRow`" + `, ` + "`table`" + `, ` + "`barChart`" + `, ` + "`lineChart`" + `, ` + "`list`" + `, ` + "`code`" + `, ` + "`callout`" + `, ` + "`badge`" + `, ` + "`container`" + `.

## Data-bound components

` + "`stat`" + `, ` + "`table`" + `, ` + "`barChart`" + `, and ` + "`lineChart`" + ` accept either inline data or a live ` + "`sql`" + ` binding, which re-runs every time the page renders (read-only, sandbox-gated, same as a notebook sql cell).

## The closed color enum

Every ` + "`color`" + ` field accepts exactly one of: ` + "`yellow`" + `, ` + "`black`" + `, ` + "`error`" + `, ` + "`muted`" + `. There is no free-string color and no hex/RGB passthrough — model output never reaches a style attribute.

## Size limits

A page caps at 200 components total; a notebook caps the total number of live (sql-bound) components across all its a2ui cells at 200 too, so one notebook can't fire hundreds of live queries the moment it opens.

Go validates this schema; Vue renders it. See docs/reference/notebooks.md for the full per-type field reference.`,
	},
	{
		Topic: "skills",
		Title: "Skills",
		Tags:  []string{"skill", "domain", "context", "built-in", "read_skill", "list_skills"},
		Content: `# Skills

A Skill is a markdown document — a name, a one-line description, and full instructions — that tells the agent something about this workspace: business rules, what a table's columns mean, which joins are canonical, terminology, what to avoid. Skills are loaded on demand, not dumped into every prompt.

## Using skills

- **` + "`list_skills`" + `** lists every skill visible right now: built-ins plus any user- or import-authored ones in the workspace. Each entry gives a name and a one-line description.
- **` + "`read_skill`" + `** fetches one skill's full instructions by name. Call it whenever a task matches a skill's description before proceeding.

## Authoring skills

**` + "`create_or_update_skill`" + `** upserts a skill by name (cannot use a built-in's name or the reserved ` + "`task-`" + ` prefix). An optional ` + "`table_name`" + ` gates the skill so it only loads once that table exists and has at least one row — useful for a skill that only makes sense once real data has been imported.

Built-in skills are embedded in the binary; workspace skills live in ` + "`skills/<name>/SKILL.md`" + ` in the workspace folder.`,
	},
	{
		Topic: "workspace",
		Title: "Workspaces and Connections",
		Tags:  []string{"workspace", "folder", "connection", "connections", "vibe", "bundle", "file", "format", "duckdb", "data", "read-only", "credentials"},
		Content: `# Workspaces and Connections

A workspace is a folder (optionally a git repository) that holds your notebooks, saved queries and skills as plain files, plus a ` + "`vibe.yaml`" + ` that lists its connections. The data itself stays where it already lives.

## Layout

- ` + "`notebooks/*.vnb`" + ` — one notebook per file
- ` + "`queries/*.sql`" + ` — saved queries
- ` + "`skills/<name>/SKILL.md`" + ` — workspace skills
- ` + "`data/`" + ` — local files, including the default local DuckDB database
- ` + "`vibe.yaml`" + ` — workspace id, name, default connection, agent limits and shared connection definitions

## Connections

Data lives in named connections: a local DuckDB file, Postgres, MySQL, MotherDuck, SQLite, or object storage (S3, GCS, R2, Azure). Each is attached into one DuckDB hub, and the connection name is the catalog in SQL (e.g. connection.schema.table). Call **list_connections** to see each one's name, type, status, whether it is read-only, and whether it is local or remote. Connections connect on first use.

## What you may and may not do

- Reads are free within the policy: any attached catalog, plus files only by a literal **absolute** path inside the workspace data/ folder, or a URL inside a storage connection's scope.
- ATTACH, SET, INSTALL/LOAD, secrets, settings and raw passthroughs (postgres_query, postgres_execute) are refused in every sandbox mode.
- A write needs three things: the sandbox mode allows it, the connection is writable, and the user approves it. For local files approval applies while "Ask before changes" is on; for a connection that is not a local file it is always required, so headless MCP refuses those writes.
- Credentials live in the OS keychain or environment variables. You can never read them; do not ask the user to paste one into a query or a notebook.

## Sharing

A .vibe file is an export bundle (notebooks, queries, skills and connection definitions, never data or credentials), not a place where data lives.`,
	},
	buildMCPToolsEntry(),
}

// mcpToolCategoryOrder is the presentation order for buildMCPToolsEntry's
// generated topic — reads best-to-worst-access, matching the sandbox
// permission ordering (introspect is allowed everywhere; schema needs full).
var mcpToolCategoryOrder = []string{"introspect", "data", "content", "schema"}

var mcpToolCategoryLabel = map[string]string{
	"introspect": "Introspect (read-only)",
	"data":       "Data (read-write)",
	"content":    "Content — notebooks & skills (read-write)",
	"schema":     "Schema (full access)",
}

// buildMCPToolsEntry generates the "mcp-tools" help topic from the live
// agents.Tools registry (ModeMCP's view, the exact list an MCP client sees)
// instead of a hand-maintained list — the hand-written version this
// replaced silently drifted (it still advertised list_pages/get_page/
// create_page/update_page/delete_page long after #69 removed those tools).
// Every tool name here is wrapped in ** (bold), not backticks, by
// convention — see help_test.go's TestHelpTopics_ToolMentionsAreRegistered,
// which scans for that exact convention to verify no help topic mentions a
// retired or otherwise-unregistered tool name.
func buildMCPToolsEntry() HelpEntry {
	byCategory := map[string][]ToolDef{}
	for _, td := range ListToolsForMode(ModeMCP) {
		byCategory[td.Category] = append(byCategory[td.Category], td)
	}

	var b strings.Builder
	b.WriteString("# MCP Server & Tools\n\n")
	b.WriteString("Vibe exposes an MCP (Model Context Protocol) server — `vibe mcp serve --workspace <folder>` (stdio or SSE) headless, or Settings -> Start MCP Server in Studio (SSE) — for external AI agents to read and modify a workspace with the same tool registry and sandbox gating the in-app agent uses.\n")

	tags := []string{"mcp", "server", "tool", "agent", "api"}
	for _, cat := range mcpToolCategoryOrder {
		tools := byCategory[cat]
		if len(tools) == 0 {
			continue
		}
		label := mcpToolCategoryLabel[cat]
		if label == "" {
			label = cat
		}
		b.WriteString("\n## " + label + "\n\n")
		for _, td := range tools {
			b.WriteString("- **" + td.Name + "** — " + td.Description + "\n")
			tags = append(tags, td.Name, cat)
		}
	}

	return HelpEntry{
		Topic:   "mcp-tools",
		Title:   "MCP Server & Tools",
		Tags:    tags,
		Content: b.String(),
	}
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
