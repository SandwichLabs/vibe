package main

// TDD RED — Phase 3 / unit P3.1: "Page-level conditional ({if}) evaluation + render"
// (parity plan §3, Evidence WU-5 close-out), studio render-path slice.
//
// REAL integration test: opens a real .vibe container (via newOpenApp), seeds a
// DuckDB table, inserts a page containing {if}/{/if} blocks into _vibe_pages,
// and drives the production Wails-bound RenderPageContent end-to-end.
//
// Asserts that renderNodeList honors ConditionalNode: the surviving branch's
// text appears in the rendered HTML and the dropped branch's text is absent.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderPageContentEvaluatesConditional(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE orders (id INTEGER, total DECIMAL(10,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders (id, total) VALUES (1, 10.00), (2, 20.00);`)
	require.NoError(t, err)

	content := "" +
		"```sql orders\n" +
		"SELECT id, total FROM orders\n" +
		"```\n\n" +
		"{if: \"orders\"}\n\n" +
		"ORDERS_PRESENT_BRANCH\n\n" +
		"{/if}\n\n" +
		"{if: \"orders\", condition: \"no_rows\"}\n\n" +
		"NO_ORDERS_BRANCH\n\n" +
		"{/if}\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"cond", "Conditional", content,
	)
	require.NoError(t, err)

	html, err := app.RenderPageContent("cond", nil)
	require.NoError(t, err)

	assert.True(t, strings.Contains(html, "ORDERS_PRESENT_BRANCH"),
		"rendered HTML must include the surviving rows-present branch text")
	assert.False(t, strings.Contains(html, "NO_ORDERS_BRANCH"),
		"rendered HTML must omit the dropped no_rows branch text")
}
