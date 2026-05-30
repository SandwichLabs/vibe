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

// seedOrders creates the shared `orders` fixture used by the per-control tests.
func seedOrders(t *testing.T, app *App) {
	t.Helper()
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
}

// seedPage inserts a notebook page with the given content under the given id.
func seedPage(t *testing.T, app *App, id, content string) {
	t.Helper()
	_, err := app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		id, "Filter", content,
	)
	require.NoError(t, err)
}

// TestRenderPageContentButtonGroupFilter exercises the {button_group} control
// end-to-end: it renders option buttons carrying data-vibe-value, registers its
// variable on the wrapper, threads the current value through to the downstream
// SQL, and marks the active option.
func TestRenderPageContentButtonGroupFilter(t *testing.T) {
	app := newOpenApp(t)
	seedOrders(t, app)

	content := "" +
		"```sql regions_q\n" +
		"SELECT DISTINCT region FROM orders ORDER BY region\n" +
		"```\n\n" +
		"{button_group: region, source=regions_q, column=region, title=Region}\n\n" +
		"```sql orders_q\n" +
		"SELECT id, region, 'ORDER_' || region AS tag FROM orders\n" +
		"{#if region} WHERE region = ${region} {/if}\n" +
		"ORDER BY id\n" +
		"```\n\n" +
		"{table: orders_q}\n"
	seedPage(t, app, "bg", content)

	htmlAll, err := app.RenderPageContent("bg", map[string]string{})
	require.NoError(t, err)
	// The wrapper carries the variable name so the frontend collector finds it.
	assert.Regexp(t, `data-vibe-input="region"`, htmlAll,
		"button_group wrapper must carry data-vibe-input with the variable name")
	// Each option button carries its value in data-vibe-value (round-trip source).
	for _, region := range []string{"West", "East", "North"} {
		assert.Contains(t, htmlAll, `data-vibe-value="`+region+`"`,
			"button_group options must carry data-vibe-value (missing %q)", region)
	}
	assert.Contains(t, htmlAll, "ORDER_East", "unfiltered render includes all rows")

	htmlWest, err := app.RenderPageContent("bg", map[string]string{"region": "West"})
	require.NoError(t, err)
	assert.Contains(t, htmlWest, "ORDER_West", "threaded value filters downstream SQL")
	assert.NotContains(t, htmlWest, "ORDER_East", "non-matching rows excluded")
	// The active option reflects the threaded current value.
	assert.Regexp(t, `(?s)<button[^>]*class="[^"]*active[^"]*"[^>]*data-vibe-value="West"|(?s)<button[^>]*data-vibe-value="West"[^>]*class="[^"]*active`, htmlWest,
		"the current button_group value must be marked active after re-render")
}

// TestRenderPageContentTextInputFilter exercises {text_input}: it renders a text
// control carrying data-vibe-input, threads the current value into the control's
// value attribute (persistence), and an empty/absent value renders unfiltered.
func TestRenderPageContentTextInputFilter(t *testing.T) {
	app := newOpenApp(t)
	seedOrders(t, app)

	content := "" +
		"{text_input: region, title=Region, placeholder=Type a region}\n\n" +
		"```sql orders_q\n" +
		"SELECT id, region, 'ORDER_' || region AS tag FROM orders\n" +
		"{#if region} WHERE region = ${region} {/if}\n" +
		"ORDER BY id\n" +
		"```\n\n" +
		"{table: orders_q}\n"
	seedPage(t, app, "ti", content)

	htmlAll, err := app.RenderPageContent("ti", map[string]string{})
	require.NoError(t, err)
	assert.Regexp(t, `data-vibe-input="region"`, htmlAll,
		"text_input wrapper must carry data-vibe-input")
	assert.Regexp(t, `<input[^>]*type="text"[^>]*class="vibe-text-input"`, htmlAll,
		"text_input must render a text control")
	// Empty/absent param => unfiltered (all rows).
	assert.Contains(t, htmlAll, "ORDER_East", "absent text value => unfiltered")

	htmlWest, err := app.RenderPageContent("ti", map[string]string{"region": "West"})
	require.NoError(t, err)
	assert.Contains(t, htmlWest, "ORDER_West", "threaded text value filters downstream SQL")
	assert.NotContains(t, htmlWest, "ORDER_East", "non-matching rows excluded")
	// The current value persists in the control's value attribute.
	assert.Regexp(t, `<input[^>]*class="vibe-text-input"[^>]*value="West"|<input[^>]*value="West"[^>]*class="vibe-text-input"`, htmlWest,
		"the current text value must persist in the input's value attribute")
}

// TestRenderPageContentSliderFilter exercises {slider}: it renders a range
// control carrying data-vibe-input, threads the current value (persistence),
// and falls back to the configured default when no value is threaded.
func TestRenderPageContentSliderFilter(t *testing.T) {
	app := newOpenApp(t)
	seedOrders(t, app)

	content := "" +
		"{slider: min_total, title=Min Total, min=0, max=100, step=10, default=15}\n\n" +
		"```sql orders_q\n" +
		"SELECT id, region, 'ORDER_' || region AS tag FROM orders\n" +
		"{#if min_total} WHERE total >= ${min_total} {/if}\n" +
		"ORDER BY id\n" +
		"```\n\n" +
		"{table: orders_q}\n"
	seedPage(t, app, "sl", content)

	htmlAll, err := app.RenderPageContent("sl", map[string]string{})
	require.NoError(t, err)
	assert.Regexp(t, `data-vibe-input="min_total"`, htmlAll,
		"slider wrapper must carry data-vibe-input")
	assert.Regexp(t, `<input[^>]*type="range"[^>]*class="vibe-slider"`, htmlAll,
		"slider must render a range control")
	// With no threaded value the slider falls back to its configured default.
	assert.Regexp(t, `<input[^>]*type="range"[^>]*value="15"`, htmlAll,
		"slider must fall back to the configured default when no value is threaded")

	// total >= 35 keeps only the North row (total 40); the threaded value also
	// persists in the control.
	htmlHigh, err := app.RenderPageContent("sl", map[string]string{"min_total": "35"})
	require.NoError(t, err)
	assert.Contains(t, htmlHigh, "ORDER_North", "threaded slider value filters downstream SQL")
	assert.NotContains(t, htmlHigh, "ORDER_West", "rows below the threshold excluded")
	assert.Regexp(t, `<input[^>]*type="range"[^>]*value="35"`, htmlHigh,
		"the current slider value must persist after re-render")
}

// TestRenderPageContentToggleFilter exercises {toggle}: it renders a checkbox
// carrying data-vibe-input plus the checked/unchecked value mapping on the
// wrapper, reflects the checked state from the threaded value, and the absence
// of a value renders unchecked + unfiltered.
func TestRenderPageContentToggleFilter(t *testing.T) {
	app := newOpenApp(t)
	seedOrders(t, app)

	content := "" +
		"{toggle: west_only, title=West only}\n\n" +
		"```sql orders_q\n" +
		"SELECT id, region, 'ORDER_' || region AS tag FROM orders\n" +
		"{#if west_only} WHERE region = 'West' {/if}\n" +
		"ORDER BY id\n" +
		"```\n\n" +
		"{table: orders_q}\n"
	seedPage(t, app, "tg", content)

	htmlOff, err := app.RenderPageContent("tg", map[string]string{})
	require.NoError(t, err)
	assert.Regexp(t, `data-vibe-input="west_only"`, htmlOff,
		"toggle wrapper must carry data-vibe-input")
	assert.Regexp(t, `<input[^>]*type="checkbox"[^>]*class="vibe-toggle"`, htmlOff,
		"toggle must render a checkbox control")
	// The wrapper declares the round-trip value mapping so an unchecked toggle is
	// well-defined: checked => "true", unchecked => "" (param removed/unfiltered).
	assert.Contains(t, htmlOff, `data-vibe-checked-value="true"`,
		"toggle wrapper must declare its checked value")
	assert.Contains(t, htmlOff, `data-vibe-unchecked-value=""`,
		"toggle wrapper must declare its unchecked value (empty => param removed)")
	assert.NotRegexp(t, `<input[^>]*class="vibe-toggle"[^>]*checked`, htmlOff,
		"absent value => unchecked")
	assert.Contains(t, htmlOff, "ORDER_East", "absent toggle value => unfiltered")

	htmlOn, err := app.RenderPageContent("tg", map[string]string{"west_only": "true"})
	require.NoError(t, err)
	assert.Regexp(t, `<input[^>]*class="vibe-toggle"[^>]*checked`, htmlOn,
		"the checked state must persist from the threaded value")
	assert.Contains(t, htmlOn, "ORDER_West", "checked toggle filters downstream SQL")
	assert.NotContains(t, htmlOn, "ORDER_East", "non-matching rows excluded when toggled on")
}
