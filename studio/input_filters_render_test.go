package main

// TDD RED — Phase 3 / unit P3.6: "Interactive input filters (dropdown +
// button_group/slider/text_input/toggle) with re-render wiring"
// (parity plan §3, Evidence WU-10 + WU-11), studio render-path slice.
//
// REAL integration test: opens a real .vibe container (via newOpenApp), seeds a
// DuckDB `orders` table with a `region` column, inserts a page containing a
// {dropdown} input control (sourced from a regions query) and a downstream SQL
// block filtered through the EXISTING ${var}/{#if} substitution, feeding a
// {table}. It drives the production Wails-bound RenderPageContent end-to-end
// twice — once with no params (unfiltered) and once with params={"region":"West"}.
//
// Asserts Option A reactivity (full re-render via the params map):
//   - dropdown <select> options are populated from its `source` query
//   - absent variable => unfiltered (all rows render)
//   - the params map injects the variable into the downstream SQL via {#if}/${var}
//     so only matching rows render
//   - the selected value persists in the control (the matching <option> is marked
//     selected)
//
// This exercises input -> variable -> SQL substitution end-to-end on the real
// engine, NOT a mock.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderPageContentDropdownFilter(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE orders (id INTEGER, region VARCHAR, total DECIMAL(10,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders (id, region, total) VALUES
		(1, 'West', 10.00),
		(2, 'East', 20.00),
		(3, 'West', 30.00),
		(4, 'North', 40.00);`)
	require.NoError(t, err)

	// Page: a regions query feeds the {dropdown} options; the dropdown registers a
	// `region` variable; the downstream orders_q SQL filters via the existing
	// {#if region} / ${region} mechanism; a {table} renders the filtered rows.
	// ORDER_<region> sentinels let us assert which rows survive in the HTML.
	content := "" +
		"```sql regions_q\n" +
		"SELECT DISTINCT region FROM orders ORDER BY region\n" +
		"```\n\n" +
		"{dropdown: region, source=regions_q, column=region, title=Region}\n\n" +
		"```sql orders_q\n" +
		"SELECT id, region, 'ORDER_' || region AS tag FROM orders\n" +
		"{#if region} WHERE region = ${region} {/if}\n" +
		"ORDER BY id\n" +
		"```\n\n" +
		"{table: orders_q}\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"dd", "Dropdown Filter", content,
	)
	require.NoError(t, err)

	// First render: no params => unfiltered. All region rows present, and the
	// dropdown <select> is populated with options from the regions query.
	htmlAll, err := app.RenderPageContent("dd", map[string]string{})
	require.NoError(t, err)

	assert.Contains(t, htmlAll, "<select",
		"dropdown must render an HTML <select> control")
	assert.Contains(t, htmlAll, "ORDER_West",
		"unfiltered render must include West rows")
	assert.Contains(t, htmlAll, "ORDER_East",
		"unfiltered render must include East rows (absent variable = unfiltered)")
	assert.Contains(t, htmlAll, "ORDER_North",
		"unfiltered render must include North rows (absent variable = unfiltered)")

	// The <select> options must come from the regions_q source query.
	for _, region := range []string{"West", "East", "North"} {
		assert.Contains(t, htmlAll, ">"+region+"<",
			"dropdown options must be populated from the source query (missing %q)", region)
	}

	// Second render: params={"region":"West"} => only West rows survive the
	// {#if region}/${region} substitution, and the West option is marked selected.
	htmlWest, err := app.RenderPageContent("dd", map[string]string{"region": "West"})
	require.NoError(t, err)

	assert.Contains(t, htmlWest, "ORDER_West",
		"filtered render must include West rows")
	assert.NotContains(t, htmlWest, "ORDER_East",
		"filtered render must exclude East rows (variable injected into SQL via ${var}/{#if})")
	assert.NotContains(t, htmlWest, "ORDER_North",
		"filtered render must exclude North rows (variable injected into SQL via ${var}/{#if})")

	// Selected value persists in the control (Option A full re-render): the West
	// <option> is marked selected.
	assert.Regexp(t, `(?s)<option[^>]*value="West"[^>]*selected`, htmlWest,
		"the selected dropdown value must persist as a selected <option> after re-render")
}
