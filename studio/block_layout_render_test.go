package main

// TDD RED — Phase 3 / unit P3.5: "Block layout directives: stack, tabs, callout"
// (parity plan §3, Evidence WU-4 generalization), studio render-path slice.
//
// REAL integration test: opens a real .vibe container (via newOpenApp), seeds a
// DuckDB table, inserts a page nesting {stack}, {tabs}/{tab}, and {callout}
// blocks (each wrapping P3.2-P3.4-style components with their own SQL sources)
// into _vibe_pages, and drives the production Wails-bound RenderPageContent
// end-to-end.
//
// Asserts that the parser/executor/render path:
//   - parses each block, recursing into block children to resolve their SQL
//   - renders a stack as a vertical container wrapping its children
//   - renders tabs as switchable panels with one header + panel per {tab:}
//   - renders a callout as a warning-typed container carrying its prose
//   - resolves nested directives' SQL sources inside the blocks

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderPageContentBlockLayouts(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE sales (region VARCHAR, total INTEGER);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO sales (region, total) VALUES ('North', 100), ('South', 200);`)
	require.NoError(t, err)

	content := "" +
		"```sql revenue\n" +
		"SELECT SUM(total) AS total FROM sales\n" +
		"```\n\n" +
		"```sql orders_count\n" +
		"SELECT COUNT(*) AS n FROM sales\n" +
		"```\n\n" +
		"```sql by_region\n" +
		"SELECT region, total FROM sales ORDER BY region\n" +
		"```\n\n" +
		// Stack wraps two metrics vertically.
		"{stack}\n\n" +
		"{metric: revenue, label: \"Revenue\"}\n\n" +
		"{metric: orders_count, label: \"Rows\"}\n\n" +
		"{/stack}\n\n" +
		// Tabs with two tab sections, each containing a table.
		"{tabs}\n\n" +
		"{tab: \"Overview\"}\n\n" +
		"{table: revenue}\n\n" +
		"{tab: \"By Region\"}\n\n" +
		"{table: by_region}\n\n" +
		"{/tabs}\n\n" +
		// Callout with a warning type and prose.
		"{callout: type: warning}\n\n" +
		"CALLOUT_WARNING_TEXT here.\n\n" +
		"{/callout}\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"blocks", "Blocks", content,
	)
	require.NoError(t, err)

	html, err := app.RenderPageContent("blocks", nil)
	require.NoError(t, err)

	// --- Stack: vertical container wrapping its children. ---
	assert.Regexp(t, `class="[^"]*vibe-stack`, html,
		"stack block must render a vibe-stack container")
	// Both metric children resolved their SQL and rendered values.
	assert.Contains(t, html, "vibe-metric-value",
		"metric children inside the stack must render")
	assert.Contains(t, html, "300",
		"the revenue metric (SUM(total)=300) must resolve inside the stack")

	// --- Tabs: one header + one panel per {tab:}. ---
	assert.Regexp(t, `class="[^"]*vibe-tabs`, html,
		"tabs block must render a vibe-tabs container")
	// Tab headers carry the labels.
	assert.Contains(t, html, "Overview", "first tab header label must render")
	assert.Contains(t, html, "By Region", "second tab header label must render")
	tabPanels := regexp.MustCompile(`vibe-tab-panel`).FindAllString(html, -1)
	assert.Len(t, tabPanels, 2, "tabs must emit exactly one switchable panel per {tab:}")
	// Nested table directives inside the tabs resolved their SQL sources.
	assert.Contains(t, html, "North",
		"the by_region table inside the second tab must resolve its SQL source")
	assert.Contains(t, html, "South",
		"the by_region table inside the second tab must resolve its SQL source")

	// --- Callout: warning-typed container carrying its prose. ---
	assert.Regexp(t, `class="[^"]*vibe-callout[^"]*warning`, html,
		"callout block must render a warning-typed vibe-callout container")
	assert.Contains(t, html, "CALLOUT_WARNING_TEXT",
		"callout prose text must render inside the callout container")

	// Ordering sanity: stack renders before tabs renders before callout.
	stackIdx := strings.Index(html, "vibe-stack")
	tabsIdx := strings.Index(html, "vibe-tabs")
	calloutIdx := strings.Index(html, "vibe-callout")
	require.GreaterOrEqual(t, stackIdx, 0)
	require.GreaterOrEqual(t, tabsIdx, 0)
	require.GreaterOrEqual(t, calloutIdx, 0)
	assert.Less(t, stackIdx, tabsIdx, "stack must render before tabs")
	assert.Less(t, tabsIdx, calloutIdx, "tabs must render before callout")
}
