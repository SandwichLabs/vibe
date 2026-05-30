package main

// P3.10 (plan §3.4 / mockup #2): the Studio Home / Dashboard must render the
// seeded `_dashboard` system page through the page pipeline (no hardcoded
// stats). REAL integration test against a real .vibe container + DuckDB.

import (
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDashboardSystemPageSeededAndExcludedFromListPages asserts the `_dashboard`
// page is seeded on container creation and hidden from the user-facing Pages
// list.
func TestDashboardSystemPageSeededAndExcludedFromListPages(t *testing.T) {
	app := newOpenApp(t)

	// The seeded system page is fetchable by its stable id.
	page, err := app.GetPage(container.DashboardPageID)
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Dashboard", page.Title)

	// But it does NOT pollute the user-facing Pages list.
	list, err := app.ListPages()
	require.NoError(t, err)
	for _, p := range list {
		assert.NotEqual(t, container.DashboardPageID, p.ID, "_dashboard must be hidden from Pages list")
	}
}

// TestDashboardRendersThroughPagePipeline asserts RenderPageContent on the
// `_dashboard` page executes its SQL blocks + {metric:}/{table:} directives on
// the real engine — i.e. the dashboard is data-driven, not hardcoded.
func TestDashboardRendersThroughPagePipeline(t *testing.T) {
	app := newOpenApp(t)

	// Seed a couple of tables so the live counts/list have something to show.
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE invoices (id INTEGER);`)
	require.NoError(t, err)

	out, err := app.RenderPageContent(container.DashboardPageID, map[string]string{})
	require.NoError(t, err)

	// The {metric:} directive rendered the live table count (2).
	assert.Contains(t, out, "Tables", "metric label rendered")
	// The {table:} directive listed the live tables by name.
	assert.Contains(t, out, "customers", "live table list rendered through the pipeline")
	assert.Contains(t, out, "invoices")
}
