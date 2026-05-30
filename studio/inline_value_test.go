package main

// TDD RED — Phase 3 / unit P3.2: "Inline {value} directive"
// (parity plan §3.1, Evidence WU-6), studio render-path slice.
//
// REAL integration test: opens a real .vibe container (via newOpenApp), seeds a
// DuckDB table, inserts a page that exercises BOTH forms of the new {value}
// directive into _vibe_pages, and drives the production Wails-bound
// RenderPageContent end-to-end through the real DuckDB engine and the shared
// renderer.Format() codes.
//
// Form 1 ({value: source=<block>, column: <col>, fmt: usd2}) resolves a column
// from a previously-executed named SQL result and formats it.
// Form 2 ({value: sql: "SELECT 1234.5", fmt: num0}) executes inline SQL (like
// the metric directive) and formats the scalar.
//
// Acceptance behaviors asserted:
//   - both forms resolve and format via renderer.Format using existing fmt codes
//     ($1,234.50 for usd2, 1,235 for num0)
//   - the value renders as an INLINE element (a <span>), not a block, so it can
//     embed within prose / report bands
//   - a missing column / empty result renders a safe placeholder, not a crash
//
// This currently FAILS because the {value} directive type is neither parsed
// (it is absent from the parser's known directive types), executed (no inline
// SQL handling for "value"), nor dispatched in renderDirective.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderPageContentInlineValueDirective(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE sales (id INTEGER, amount DECIMAL(12,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO sales (id, amount) VALUES (1, 1000.00), (2, 234.50);`)
	require.NoError(t, err)

	content := "" +
		"```sql totals\n" +
		"SELECT SUM(amount) AS total FROM sales\n" +
		"```\n\n" +
		"Revenue so far is {value: source=totals, column: total, fmt: usd2} this period.\n\n" +
		"Inline literal: {value: sql: \"SELECT 1234.5\", fmt: num0} units.\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"val", "InlineValue", content,
	)
	require.NoError(t, err)

	html, err := app.RenderPageContent("val", nil)
	require.NoError(t, err)

	// Form 1: referenced source+column formatted with usd2 (1234.50 => $1,234.50).
	assert.Contains(t, html, "$1,234.50",
		"source/column {value} must format the referenced aggregate via renderer.Format usd2")
	// Form 2: inline SQL formatted with num0 (1234.5 => 1,235).
	assert.Contains(t, html, "1,235",
		"inline-SQL {value} must execute through the engine and format via renderer.Format num0")

	// The value must render as an inline <span>, not a block element, so it can
	// embed in prose / report bands.
	assert.Contains(t, strings.ToLower(html), "<span",
		"{value} must render as an inline <span> element")
}

// TestRenderPageContentInlineValueMissingColumn asserts the directive degrades
// gracefully: referencing a non-existent column (or an empty result) renders a
// safe placeholder rather than crashing or erroring out the whole page.
func TestRenderPageContentInlineValueMissingColumn(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE sales (id INTEGER, amount DECIMAL(12,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO sales (id, amount) VALUES (1, 1000.00);`)
	require.NoError(t, err)

	content := "" +
		"```sql totals\n" +
		"SELECT SUM(amount) AS total FROM sales\n" +
		"```\n\n" +
		"Missing: {value: source=totals, column: nonexistent, fmt: usd2} end.\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"valmiss", "InlineValueMissing", content,
	)
	require.NoError(t, err)

	// Must not crash / panic; must not surface a hard render error for the page.
	html, err := app.RenderPageContent("valmiss", nil)
	require.NoError(t, err)

	// Surrounding prose still renders, proving the page wasn't aborted.
	assert.Contains(t, html, "Missing:")
	assert.Contains(t, html, "end.")
}

// TestRenderPageContentStandaloneValueDirective covers a {value: ...} that
// occupies a full line on its own — the natural form for embedding a single
// scalar in a report detail band. isDirectiveLine parses this as a
// DirectiveNode{Type:"value"}, so it must be resolved by the executor and
// dispatched by renderDirective's `case "value":` (NOT the inline-prose path).
// The source+column form is the realistic standalone form (inline-SQL with its
// nested colons cannot be a YAML directive line — same limitation as the metric
// directive — so that form stays inline-in-prose). A standalone {value}
// previously CRASHED the whole page (executor treated fmt/column as undefined
// source references) — this asserts it renders an inline <span> instead.
func TestRenderPageContentStandaloneValueDirective(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE sales (id INTEGER, amount DECIMAL(12,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO sales (id, amount) VALUES (1, 1000.00), (2, 234.50);`)
	require.NoError(t, err)

	content := "" +
		"```sql totals\n" +
		"SELECT SUM(amount) AS total FROM sales\n" +
		"```\n\n" +
		"{value: source=totals, column: total, fmt: usd2}\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"valstand", "StandaloneValue", content,
	)
	require.NoError(t, err)

	html, err := app.RenderPageContent("valstand", nil)
	require.NoError(t, err, "a standalone-line {value} must not abort the page render")

	// source+column formatted via renderer.Format usd2.
	assert.Contains(t, html, "$1,234.50",
		"standalone source/column {value} must resolve + format via renderer.Format usd2")
	// Must render as an inline <span>, mirroring the inline-prose path.
	assert.Contains(t, strings.ToLower(html), "<span",
		"standalone {value} must render via renderDirective as an inline <span>")
	// Must NOT fall through to the default branch's literal text.
	assert.NotContains(t, html, "Unknown directive",
		"standalone {value} must be dispatched, not hit renderDirective's default branch")
}

// TestRenderPageContentStandaloneValueMissingColumn asserts a standalone-line
// {value} referencing a non-existent column degrades to a safe em-dash
// placeholder rather than erroring out / aborting the page.
func TestRenderPageContentStandaloneValueMissingColumn(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE sales (id INTEGER, amount DECIMAL(12,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO sales (id, amount) VALUES (1, 1000.00);`)
	require.NoError(t, err)

	content := "" +
		"```sql totals\n" +
		"SELECT SUM(amount) AS total FROM sales\n" +
		"```\n\n" +
		"Before line.\n\n" +
		"{value: source=totals, column: nonexistent, fmt: usd2}\n\n" +
		"After line.\n"

	_, err = app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		"valstandmiss", "StandaloneValueMissing", content,
	)
	require.NoError(t, err)

	html, err := app.RenderPageContent("valstandmiss", nil)
	require.NoError(t, err, "a standalone-line {value} with a missing column must not abort the page")

	// Surrounding prose still renders, proving the page wasn't aborted.
	assert.Contains(t, html, "Before line.")
	assert.Contains(t, html, "After line.")
	// The missing value degrades to the safe em-dash placeholder span.
	assert.Contains(t, html, "—",
		"missing column in a standalone {value} must render the em-dash safe placeholder")
	assert.NotContains(t, html, "Unknown directive")
}
