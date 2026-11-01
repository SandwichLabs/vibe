package agents

import (
	"strconv"
	"strings"

	"github.com/SandwichLabs/assay/internal/a2ui"
	"github.com/SandwichLabs/assay/internal/notebooks"
)

// HelpEntry is a single help topic.
type HelpEntry struct {
	Topic   string   // short identifier
	Title   string   // human-readable title
	Summary string   // one line shown in the help_search index (empty query)
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
// topics below. The component, cell-view and colour lists in those two topics
// are generated from the a2ui and notebooks registries (codeList, below), so
// they cannot drift. The "mcp-tools" topic is generated from the live agents.Tools
// registry (buildMCPToolsEntry, below) so it can never drift out of sync
// with what the server actually exposes the way the old hand-written list
// did (it still listed list_pages/get_page/create_page/update_page/
// delete_page long after #69 removed them).
var HelpEntries = []HelpEntry{
	{
		Topic:   "notebooks",
		Title:   "Notebooks",
		Summary: "Notebook files, cell types, views, and the tools that author them.",
		Tags:    []string{"notebook", "cell", "sql", "markdown", "a2ui", "dashboard", "view", "table", "bar", "line", "stat", "area", "columns", "rings", "radar", "sankey", "heatmap", "rev", "edit", "update", "delete", "move", "add_query_cell", "add_markdown_cell", "update_cell", "delete_cell", "move_cell", "get_notebook"},
		Content: `# Notebooks

A notebook is a title plus an ordered list of cells, saved as a ` + "`notebooks/*.asnb`" + ` file in the workspace. There is no page markup and no inter-cell references — the connections are the only shared state.

## Cell types

- **sql** — a single read-only SQL statement. Renders as one of ` + strconv.Itoa(len(notebooks.Views)) + ` views: ` + codeList(notebooks.Views) + `. Until a view is explicitly chosen, it auto-fits to the result shape the first time the cell runs.
- **markdown** — plain markdown prose.
- **a2ui** — an A2UI page (see the "a2ui" topic): one or more components laid out in a single cell, e.g. a small dashboard.

## Read-only cells, live outputs

A sql or a2ui cell's ` + "`sql`" + ` is always read-only (INSERT/UPDATE/DELETE/DDL are rejected at run time, the same classifier a live A2UI binding uses). Outputs are never persisted — every data cell re-runs live when the notebook is opened; only the cell's source/view/page is stored.

## Authoring

- **` + "`add_query_cell`" + `** appends a single sql cell (one read-only statement; ` + "`view`" + ` must be one of the views above) to a notebook, creating one if none is targeted/open.
- **` + "`add_markdown_cell`" + `** appends a markdown cell, or inserts it after ` + "`after_cell_id`" + `.
- **` + "`create_dashboard`" + `** appends a whole A2UI page (multiple components) as one a2ui cell.
- **` + "`list_notebooks`" + `** / **` + "`get_notebook`" + `** are the read-only path: list every notebook, or fetch one notebook's cells as data — each cell's id and type, a sql cell's source, view, label and connection, a markdown cell's source, an a2ui cell's whole page — plus the notebook's ` + "`rev`" + `. Never rendered output.

## Editing

- **` + "`update_cell`" + `** changes one cell in place (sql/label/view/connection, markdown or page, whichever fits the cell's type), **` + "`delete_cell`" + `** removes one, **` + "`move_cell`" + `** reorders (` + "`after_cell_id`" + ` or ` + "`position`" + `). Each takes the notebook's ` + "`rev`" + ` from ` + "`get_notebook`" + `: a notebook that changed since is refused and nothing is written, so call ` + "`get_notebook`" + ` again and re-apply. Every result carries the new ` + "`rev`" + `.
- New content is checked as the add tools check it: SQL is one read-only statement, a page is validated and its sql bindings vetted.

There is no create_page/update_page/get_page/list_pages tool; those were retired with the page system.`,
	},
	{
		Topic:   "a2ui",
		Title:   "A2UI Components",
		Summary: "The closed A2UI component schema for dashboards (types, bindings, colours, limits).",
		Tags:    []string{"a2ui", "component", "dashboard", "chart", "table", "stat", "color", "schema"},
		Content: `# A2UI Components

A2UI is a closed, validated component schema for a small dashboard: ` + "`{ title, components[] }`" + `. Components are a flat list, nested by a ` + "`parent`" + ` id rather than JSON nesting.

## Component types

` + codeList(a2ui.TypeNames()) + `.

Read the built-in skill (` + "`read_skill a2ui`" + `) for every type's fields, generated from the validator. A data artifact view additionally takes ` + codeList(a2ui.ViewTypeNames()) + ` and a ` + "`drill`" + ` field, and binds data with ` + "`query`" + ` instead of ` + "`sql`" + `; a notebook dashboard (` + "`create_dashboard`" + `, ` + "`present_data`" + `) takes neither.

## Data-bound components

` + "`stat`" + `, ` + "`table`" + ` and every chart type accept either inline data or a live ` + "`sql`" + ` binding, which re-runs every time the page renders (read-only, sandbox-gated, same as a notebook sql cell).

## The closed color enum

Every ` + "`color`" + ` field accepts exactly one of: ` + codeList(a2ui.ColorTokens) + `. There is no free-string color and no hex/RGB passthrough — model output never reaches a style attribute.

## Size limits

A page caps at 200 components total; a notebook caps the total number of live (sql-bound) components across all its a2ui cells at 200 too, so one notebook can't fire hundreds of live queries the moment it opens.

Go validates this schema; Vue renders it. See docs/reference/notebooks.md for the full per-type field reference.`,
	},
	{
		Topic:   "artifacts",
		Title:   "Data Artifacts",
		Summary: "Parameterised live views stored as files (controls, pivot, drill); points to the data-artifacts skill.",
		Tags:    []string{"artifact", "artifacts", "data", "view", "parameter", "control", "pivot", "drill", "script", "html", "page", "write_artifact"},
		Content: `# Data Artifacts

A data artifact is a parameterised, live view stored as files under ` + "`artifacts/<name>/`" + `: an ` + "`artifact.yaml`" + ` manifest (typed parameters with defaults and named read-only queries) plus a view (A2UI JSON, a script, or an HTML page). It stores questions, never answers, and every query is vetted against the SQL policy when it is saved and again on every run.

- **list_artifacts** / **get_artifact** read them; **write_artifact** authors or replaces one (send the ` + "`rev`" + ` from get_artifact to replace).
- Views can use controls (select, dateRange, numberInput, textInput), ` + "`pivot`" + ` and ` + "`drill`" + `. Script and HTML views are stored but never run until the person allows them in Studio.
- No tool runs an artifact or returns its results.

Call **read_skill** with ` + "`data-artifacts`" + ` for the manifest and view format. Reference: docs/reference/artifacts.md.`,
	},
	{
		Topic:   "semantics",
		Title:   "Semantic Layer",
		Summary: "Entities, metrics, joins and a glossary kept as reviewed files; metric() in SQL; propose_definition for the person to accept.",
		Tags:    []string{"semantics", "semantic", "metric", "metrics", "entity", "entities", "join", "glossary", "definition", "definitions", "proposal", "propose", "dimension", "measure", "list_definitions", "get_definition", "propose_definition"},
		Content: `# Semantic Layer

The workspace says what its data means in reviewed files under ` + "`semantics/`" + `: **entities** (what the data is about: grain, table, id column, PII flags), **metrics** (a named measure over a saved query), **joins** (how two entities relate) and a **glossary**. A person writes them and git carries them.

## Using what exists

- **` + "`list_definitions`" + `** lists them all, with each one's ` + "`rev`" + ` and how many proposals wait. **` + "`get_definition`" + `** returns one as data; for a metric it also returns the declared parameters of its saved query. Both work in every tier.
- In SQL, call a metric by name: ` + "`SELECT * FROM metric('net_revenue')`" + `, ` + "`metric('net_revenue', by => 'region')`" + `, ` + "`by => ['region', 'month']`" + `. The name and dimensions are quoted literals; a dimension must be one the metric lists. The call is replaced by the metric's own query before any check runs, so it is held to the same read-only policy. Use a defined metric instead of re-deriving its measure by hand.
- The person can check that every definition still binds against its connection with ` + "`assay semantics check --compile`" + ` (or Studio's Run assertions); no tool runs it, so a metric you cannot query may simply not compile.

## Proposing a change

**` + "`propose_definition`" + `** (read-write and full tiers) files a proposal in the person's inbox. **Nothing in ` + "`semantics/`" + ` changes until the person accepts it** (` + "`assay semantics proposals`" + `, or Assay), and no tool can accept, reject or write a definition. The definition is checked now with the same rules as a file, so a bad measure, an unknown field or a reserved word is refused with the reason and nothing is filed.

- ` + "`kind`" + ` is entity, metric, join or glossary. A metric is ` + "`{query, measure, dimensions?, title?, description?, format?, settled_by?, assertions?}`" + `; its saved query must already exist (save_query first). An entity is ` + "`{grain, table, id_column, ...}`" + `, a join ` + "`{left, right, on, cardinality}`" + `, a glossary term ` + "`{term, text}`" + `.
- To change an existing definition, send the ` + "`rev`" + ` from get_definition as ` + "`replaces_rev`" + `; without it a name that exists is refused. Give a ` + "`rationale`" + ` the person will read and ` + "`evidence`" + ` (a saved query id, a receipt) where there is some.
- Until the person accepts it, the metric does not exist: do not call it in SQL.

Reference: docs/reference/semantics.md.`,
	},
	{
		Topic:   "skills",
		Title:   "Skills",
		Summary: "Markdown skills the agent loads on demand (list_skills, read_skill, authoring).",
		Tags:    []string{"skill", "domain", "context", "built-in", "read_skill", "list_skills"},
		Content: `# Skills

A Skill is a markdown document — a name, a one-line description, and full instructions — that tells the agent something about this workspace: business rules, what a table's columns mean, which joins are canonical, terminology, what to avoid. Skills are loaded on demand, not dumped into every prompt.

## Using skills

- **` + "`list_skills`" + `** lists every skill visible right now: built-ins plus any user- or import-authored ones in the workspace. Each entry gives a name and a one-line description.
- **` + "`read_skill`" + `** fetches one skill by name: ` + "`instructions`" + ` is the skill text and ` + "`framing`" + ` says it is reference material that never overrides the sandbox. Send only the instructions back when editing. Call it whenever a task matches a skill's description before proceeding.

## Authoring skills

**` + "`create_or_update_skill`" + `** creates a skill under a new name without a ` + "`rev`" + `; replacing an existing skill needs the ` + "`rev`" + ` from ` + "`read_skill`" + ` or ` + "`list_skills`" + ` (cannot use a built-in's name or the reserved ` + "`task-`" + ` prefix). An optional ` + "`table_name`" + ` gates the skill so it only loads once that table exists and has at least one row — useful for a skill that only makes sense once real data has been imported.

Built-in skills are embedded in the binary; workspace skills live in ` + "`skills/<name>/SKILL.md`" + ` in the workspace folder.`,
	},
	{
		Topic:   "workspace",
		Title:   "Workspaces and Connections",
		Summary: "Workspace folder layout, named connections, and what the agent may do.",
		Tags:    []string{"workspace", "folder", "connection", "connections", "assay", "bundle", "file", "format", "duckdb", "data", "read-only", "credentials"},
		Content: `# Workspaces and Connections

A workspace is a folder (optionally a git repository) that holds your notebooks, saved queries and skills as plain files, plus a ` + "`assay.yaml`" + ` that lists its connections. The data itself stays where it already lives.

## Layout

- ` + "`notebooks/*.asnb`" + ` — one notebook per file
- ` + "`queries/*.sql`" + ` — saved queries
- ` + "`skills/<name>/SKILL.md`" + ` — workspace skills
- ` + "`data/`" + ` — local files, including the default local DuckDB database
- ` + "`assay.yaml`" + ` — workspace id, name, default connection, agent limits and shared connection definitions

## Connections

Data lives in named connections: a local DuckDB file, Postgres, MySQL, MotherDuck, SQLite, or object storage (S3, GCS, R2, Azure). Each is attached into one DuckDB hub, and the connection name is the catalog in SQL (e.g. connection.schema.table). Call **list_connections** to see each one's name, type, status, whether it is read-only, and whether it is local or remote. Connections connect on first use.

## What you may and may not do

- Reads are free within the policy: any attached catalog, plus files only by a literal **absolute** path inside the workspace data/ folder, or a URL inside a storage connection's scope (on s3/gcs/r2 a * or ** wildcard works: Assay lists the matching objects when the query runs, a point-in-time list of at most 10000; only * and ** are wildcards).
- ATTACH, SET, INSTALL/LOAD, secrets, settings and raw passthroughs (postgres_query, postgres_execute) are refused in every sandbox mode.
- A write needs three things: the sandbox mode allows it, the connection is writable, and the user approves it. For local files approval applies while "Ask before changes" is on; for a connection that is not a local file it is always required, so headless MCP refuses those writes.
- Credentials live in the OS keychain or environment variables. You can never read them; do not ask the user to paste one into a query or a notebook.

## Sharing

A .assay file is an export bundle (notebooks, queries, skills and connection definitions, never data or credentials), not a place where data lives.`,
	},
	buildMCPToolsEntry(),
}

// codeList renders names as a comma-separated list of backticked code spans.
func codeList(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "`" + n + "`"
	}
	return strings.Join(parts, ", ")
}

// mcpToolCategoryOrder is the presentation order for buildMCPToolsEntry's
// generated topic — reads best-to-worst-access, matching the sandbox
// permission ordering (introspect is allowed everywhere; schema needs full).
var mcpToolCategoryOrder = []string{"introspect", "data", "content", "schema"}

var mcpToolCategoryLabel = map[string]string{
	"introspect": "Introspect (read-only)",
	"data":       "Data (read-write)",
	"content":    "Content — notebooks, skills & artifacts (read-write)",
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
	b.WriteString("Assay exposes an MCP (Model Context Protocol) server — `assay mcp serve --workspace <folder>` (stdio or SSE) headless, or Studio's built-in server (SSE, Settings → Workspace → Claude and other tools) — for external AI agents to read and modify a workspace with the same tool registry and sandbox gating the in-app agent uses.\n\n")
	b.WriteString("When \"Ask before changes\" is on and nobody can approve (headless), a write is refused and nothing is changed. A notebook, skill or artifact write is checked first: an invalid request returns its validation error, and a valid one says it is valid and ready, so ask the person to approve it or to turn off \"Ask before changes\" in Settings → Workspace → The agent.\n")

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
		Summary: "How to reach the MCP server and the full tool list by sandbox tier.",
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

	// An exact topic name (the index's handle) returns just that topic.
	for _, entry := range HelpEntries {
		if strings.EqualFold(strings.TrimSpace(query), entry.Topic) {
			return []HelpEntry{entry}
		}
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
