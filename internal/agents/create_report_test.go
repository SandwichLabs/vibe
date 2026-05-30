package agents_test

// P3.10 — Agent Report Wizard tool (plan §3.4, Evidence WU-13/WU-14).
//
// INTEGRATION test: stands up the real agent Handler over a real .vibe
// container (SQLCipher) + real DuckDB engine, and drives the new create_report
// wizard tool through the JSON-RPC tool surface (Handler.Handle) — NOT by
// calling any Go backend method directly, mirroring wizard_tools_test.go.
//
// Acceptance behavior asserted here:
//   - create_report is a registered agent tool (catalog + schema) in the
//     "schema" category, so it is gated by the existing sandbox.
//   - create_report, dispatched through Handler.Handle against a real .vibe +
//     DuckDB, persists a valid _vibe_reports spec readable via
//     reports.ReportStore.Get with the requested title/source/group/aggregates.
//   - The persisted spec immediately produces banded HTML (report header, group
//     headers, detail rows, and the requested group-footer subtotals) when run
//     through ComputeBands + RenderReport (the RenderReportContent pipeline).
//   - create_report is denied with a permission error under a read-only sandbox.

import (
	"context"
	"strings"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	htmlrenderer "github.com/sandwich-labs/vibe-app/internal/renderer/html"
	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createReportParams describes a "sales by rep" report over the orders table:
// grouped by customer_id, detail showing order_id + total, with a per-group
// subtotal of total.
func createReportParams() map[string]any {
	return map[string]any{
		"id":     "sales_by_rep",
		"title":  "Sales by Rep",
		"source": "orders",
		"group_by": []any{
			map[string]any{"field": "customer_id", "sort_dir": "asc"},
		},
		"sort": []any{
			map[string]any{"field": "total", "dir": "desc"},
		},
		"columns": []any{
			map[string]any{"field": "order_id"},
			map[string]any{"field": "total"},
		},
		"aggregates": []any{
			map[string]any{"name": "subtotal", "func": "sum", "field": "total"},
		},
	}
}

// TestCreateReportTool_RegisteredWithSchema asserts create_report is discoverable
// on the tool surface with a documented schema, in the gated "schema" category.
func TestCreateReportTool_RegisteredWithSchema(t *testing.T) {
	tool := agents.GetTool("create_report")
	require.NotNil(t, tool, "create_report must be registered as an agent tool")
	assert.NotEmpty(t, tool.Description, "create_report must have a documented description")
	assert.Equal(t, "schema", tool.Category, "create_report must be in the gated 'schema' category")

	found := false
	for _, td := range agents.ListTools() {
		if td.Name == "create_report" {
			found = true
			break
		}
	}
	assert.True(t, found, "create_report must appear in ListTools()")

	byName := map[string]agents.ParamDef{}
	for _, p := range tool.Params {
		byName[p.Name] = p
	}
	for _, p := range []string{"title", "source"} {
		require.Contains(t, byName, p, "create_report must document a %q param", p)
		assert.True(t, byName[p].Required, "%q should be required", p)
	}
}

// TestCreateReportTool_PersistsAndRendersBanded drives create_report through the
// JSON-RPC surface against a real .vibe + DuckDB, then asserts a valid report
// spec lands in _vibe_reports (via ReportStore.Get) and immediately renders to
// banded HTML carrying the requested per-group subtotal.
func TestCreateReportTool_PersistsAndRendersBanded(t *testing.T) {
	handler, c := newWizardFixture(t, agents.ModeFull)
	ctx := context.Background()

	// Seed a second order for the same customer so the subtotal is non-trivial.
	_, err := c.DuckDBDB().Exec(
		`INSERT INTO orders (order_id, customer_id, total) VALUES (11, 1, 100.5);`,
	)
	require.NoError(t, err)

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "create_report",
		Params:  createReportParams(),
	})
	require.NotNil(t, resp)
	require.Nil(t, resp.Error, "create_report should succeed in full sandbox: %+v", resp.Error)

	// The spec must be persisted and readable via the ReportStore.
	store := reports.NewReportStore(c.SQLiteDB())
	spec, err := store.Get("sales_by_rep")
	require.NoError(t, err)
	require.NotNil(t, spec, "create_report must persist a _vibe_reports row readable via ReportStore.Get")

	assert.Equal(t, "Sales by Rep", spec.Title)
	assert.Equal(t, "orders", spec.Source)
	require.Len(t, spec.Groups, 1, "the requested group_by must be persisted")
	assert.Equal(t, "customer_id", spec.Groups[0].Field)
	require.Len(t, spec.GroupFooterAggregates, 1, "the requested aggregate must be persisted")
	assert.Equal(t, reports.AggSum, spec.GroupFooterAggregates[0].Func)
	assert.Equal(t, "total", spec.GroupFooterAggregates[0].Field)

	// The persisted spec must immediately render to banded HTML with the subtotal.
	eng := engine.NewManager(c.DuckDBDB(), engine.WithLogger(wizLogger()))
	rs, err := eng.Execute(ctx, "SELECT order_id, customer_id, total FROM orders", nil)
	require.NoError(t, err)

	tree, err := reports.ComputeBands(rs, *spec)
	require.NoError(t, err)

	r := htmlrenderer.NewRenderer(wizLogger())
	out, err := r.RenderReport(tree, *spec, htmlrenderer.RenderReportOptions{})
	require.NoError(t, err)

	assert.Contains(t, out, "vibe-report-header", "banded output must include the report header band")
	assert.Contains(t, out, "vibe-group-header", "banded output must include a group header band")
	assert.Contains(t, out, "vibe-group-footer", "banded output must include the group-footer subtotal band")
	assert.Contains(t, out, `data-agg="subtotal"`, "the requested subtotal aggregate must be rendered")
	// Customer 1's two orders (99.5 + 100.5) subtotal to 200.
	assert.True(t, strings.Contains(out, "200"),
		"the group-footer subtotal for customer 1 must be 200, got:\n%s", out)
}

// TestCreateReportTool_DeniedReadOnly asserts create_report is rejected with a
// capability/permission error under a read-only sandbox.
func TestCreateReportTool_DeniedReadOnly(t *testing.T) {
	handler, _ := newWizardFixture(t, agents.ModeReadOnly)
	ctx := context.Background()

	resp := handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      100,
		Method:  "create_report",
		Params:  createReportParams(),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "create_report must be denied in read-only sandbox")
	assert.Equal(t, agents.ErrPermission, resp.Error.Code,
		"create_report denial should be a capability/permission error")
}
