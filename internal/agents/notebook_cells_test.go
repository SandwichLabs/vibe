package agents

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/notebooks"
)

// newNotebookTestHandler builds a newRealDuckDBHandler fixture and returns the
// notebooks.Store behind it (the in-memory Mock) alongside the handler.
func newNotebookTestHandler(t *testing.T, mode SandboxMode) (*Handler, notebooks.Store) {
	t.Helper()
	handler, _ := newRealDuckDBHandler(t, mode)
	return handler, handler.access.Notebooks()
}

// TestHandler_ConcurrentAddQueryCellDispatch_Race exercises the long-lived
// MCP-handler scenario for real: many goroutines calling SetActiveNotebook
// AND dispatching add_query_cell (which reads it via resolveTargetNotebook)
// concurrently on the SAME Handler — the exact shape of the review's finding
// 8 data race. Run with -race.
func TestHandler_ConcurrentAddQueryCellDispatch_Race(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Target")
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			handler.SetActiveNotebook(nb.ID)
		}()
		go func() {
			defer wg.Done()
			handler.Handle(context.Background(), &Request{
				JSONRPC: "2.0", ID: "1", Method: "add_query_cell",
				Params: map[string]any{"sql": "SELECT * FROM t"},
			})
		}()
	}
	wg.Wait()
}

func TestAddQueryCell_RejectsWriteSQL(t *testing.T) {
	handler, _ := newNotebookTestHandler(t, ModeFull)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "add_query_cell",
		Params: map[string]any{"sql": "DELETE FROM t", "label": "oops"},
	})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
}

func TestAddQueryCell_CreatesNewNotebookWhenNoneActive(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "add_query_cell",
		Params: map[string]any{"sql": "SELECT * FROM t", "label": "All rows", "view": "table"},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	nbID, _ := result["notebook_id"].(string)
	require.NotEmpty(t, nbID)
	assert.Equal(t, "All rows", result["title"], "a new notebook is titled from the cell label when none is open")

	nb, _, err := nbs.Get(ctx, nbID)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, notebooks.CellSQL, nb.Cells[0].Type)
	assert.Equal(t, "SELECT * FROM t", nb.Cells[0].Source)
}

func TestAddQueryCell_AppendsToActiveNotebookWithoutClobberingExistingCells(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()

	existing, err := nbs.Create(ctx, "My notebook")
	require.NoError(t, err)
	cell, err := notebooks.NewCell(notebooks.CellMarkdown)
	require.NoError(t, err)
	cell.Source = "# Notes\n\nUnsaved-in-spirit prior content."
	existing.Cells = append(existing.Cells, cell)
	saved, _, err := nbs.Insert(ctx, existing)
	require.NoError(t, err)

	handler.SetActiveNotebook(saved.ID)
	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "add_query_cell",
		Params: map[string]any{"sql": "SELECT * FROM t"},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	assert.Equal(t, saved.ID, result["notebook_id"], "must append to the active notebook, not create a new one")

	nb, _, err := nbs.Get(ctx, saved.ID)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 2, "the pre-existing cell must survive the append")
	assert.Equal(t, notebooks.CellMarkdown, nb.Cells[0].Type)
	assert.Equal(t, notebooks.CellSQL, nb.Cells[1].Type)
}

func TestAddQueryCell_ExplicitNotebookIDOverridesActive(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()

	active, err := nbs.Create(ctx, "Active")
	require.NoError(t, err)
	handler.SetActiveNotebook(active.ID)

	other, err := nbs.Create(ctx, "Other")
	require.NoError(t, err)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "add_query_cell",
		Params: map[string]any{"sql": "SELECT * FROM t", "notebook_id": other.ID},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	assert.Equal(t, other.ID, result["notebook_id"])
}

func TestCreateDashboard_RejectsInvalidPage(t *testing.T) {
	handler, _ := newNotebookTestHandler(t, ModeFull)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "create_dashboard",
		Params: map[string]any{"page": map[string]any{"title": "Empty", "components": []any{}}},
	})
	require.NotNil(t, resp.Error, "a2ui.Validate rejects a page with zero components")
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
}

func TestCreateDashboard_AppendsA2UICell(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()

	page := map[string]any{
		"title": "Revenue overview",
		"components": []any{
			map[string]any{"id": "c1", "type": "stat", "label": "Total revenue", "value": 100},
		},
	}
	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "create_dashboard",
		Params: map[string]any{"page": page},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	nbID, _ := result["notebook_id"].(string)
	assert.Equal(t, "Revenue overview", result["title"], "a new notebook is titled from page.title when none is open")

	nb, _, err := nbs.Get(ctx, nbID)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, notebooks.CellA2UI, nb.Cells[0].Type)
	require.NotNil(t, nb.Cells[0].Page)
	assert.Equal(t, "Revenue overview", nb.Cells[0].Page.Title)
}

func TestListNotebooks_ReturnsEveryNotebook(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeReadOnly)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Sales")
	require.NoError(t, err)
	cell, err := notebooks.NewCell(notebooks.CellSQL)
	require.NoError(t, err)
	cell.Source = "SELECT 1"
	_, _, err = saveWithNewCell(ctx, nbs, nb, cell)
	require.NoError(t, err)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "list_notebooks",
	})
	require.Nil(t, resp.Error, "list_notebooks is introspect-category and must be allowed under read-only")
	result, _ := resp.Result.(map[string]any)
	list, _ := result["notebooks"].([]map[string]any)
	require.Len(t, list, 1)
	assert.Equal(t, "Sales", list[0]["title"])
	assert.Equal(t, 1, list[0]["cell_count"])
}

func TestGetNotebook_ReturnsCellSourcesAsData(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeReadOnly)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Sales")
	require.NoError(t, err)

	sqlCell, err := notebooks.NewCell(notebooks.CellSQL)
	require.NoError(t, err)
	sqlCell.Source = "SELECT 1 AS one"
	sqlCell.Label = "one"
	nb, _, err = saveWithNewCell(ctx, nbs, nb, sqlCell)
	require.NoError(t, err)

	mdCell, err := notebooks.NewCell(notebooks.CellMarkdown)
	require.NoError(t, err)
	mdCell.Source = "# Hello"
	nb, _, err = saveWithNewCell(ctx, nbs, nb, mdCell)
	require.NoError(t, err)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "get_notebook",
		Params: map[string]any{"id": nb.ID},
	})
	require.Nil(t, resp.Error, "get_notebook is introspect-category and must be allowed under read-only")
	result, _ := resp.Result.(map[string]any)
	assert.Equal(t, "Sales", result["title"])
	cells, _ := result["cells"].([]map[string]any)
	require.Len(t, cells, 2)
	assert.Equal(t, "sql", cells[0]["type"])
	assert.Equal(t, "SELECT 1 AS one", cells[0]["source"])
	assert.Equal(t, "markdown", cells[1]["type"])
	assert.Equal(t, "# Hello", cells[1]["source"])
}

func TestGetNotebook_UnknownIDIsInvalidParams(t *testing.T) {
	handler, _ := newNotebookTestHandler(t, ModeReadOnly)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "get_notebook",
		Params: map[string]any{"id": "does-not-exist"},
	})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
}

func TestGetNotebook_TruncatesLongSource(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeReadOnly)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Big")
	require.NoError(t, err)

	mdCell, err := notebooks.NewCell(notebooks.CellMarkdown)
	require.NoError(t, err)
	mdCell.Source = strings.Repeat("x", notebookCellSourceBudgetRunes+500)
	nb, _, err = saveWithNewCell(ctx, nbs, nb, mdCell)
	require.NoError(t, err)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "get_notebook",
		Params: map[string]any{"id": nb.ID},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	cells, _ := result["cells"].([]map[string]any)
	require.Len(t, cells, 1)
	source, _ := cells[0]["source"].(string)
	assert.Len(t, []rune(source), notebookCellSourceBudgetRunes)
	assert.Equal(t, true, cells[0]["source_truncated"])
}

func TestGetNotebook_A2UICellSummarizedNotRendered(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Dash")
	require.NoError(t, err)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "create_dashboard",
		Params: map[string]any{
			"notebook_id": nb.ID,
			"page": map[string]any{
				"title": "Overview",
				"components": []any{
					map[string]any{"id": "h1", "type": "heading", "text": "Hi"},
				},
			},
		},
	})
	require.Nil(t, resp.Error)

	resp = handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "2", Method: "get_notebook",
		Params: map[string]any{"id": nb.ID},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	cells, _ := result["cells"].([]map[string]any)
	require.Len(t, cells, 1)
	assert.Equal(t, "a2ui", cells[0]["type"])
	assert.Equal(t, "Overview", cells[0]["title"])
	assert.Equal(t, 1, cells[0]["component_count"])
	_, hasSource := cells[0]["source"]
	assert.False(t, hasSource, "an a2ui cell must be summarized, never rendered")
}

// TestGetNotebook_CapsTotalOutputAtRowResultBudget asserts get_notebook
// stops adding cells once the combined output reaches roughly
// RowResultBudgetChars, and reports exactly how much was cut via
// "truncated": {shown, total} — the same tool-result budget run_query
// enforces on rows, applied here so a notebook with hundreds of cells (up
// to the 500-cell cap) can't blow the same context/UI-event budget a giant
// run_query result would.
func TestGetNotebook_CapsTotalOutputAtRowResultBudget(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Big notebook")
	require.NoError(t, err)

	// 500 markdown cells, each ~200 bytes of source — well over
	// RowResultBudgetChars (15,000) in total, so get_notebook must stop
	// early rather than return everything.
	source := strings.Repeat("x", 200)
	for i := 0; i < 500; i++ {
		cell, cellErr := notebooks.NewCell(notebooks.CellMarkdown)
		require.NoError(t, cellErr)
		cell.Source = source
		nb.Cells = append(nb.Cells, cell)
	}
	raw, err := json.Marshal(nb)
	require.NoError(t, err)
	saved, _, err := nbs.Save(ctx, raw)
	require.NoError(t, err)
	require.Len(t, saved.Cells, 500, "all 500 cells must survive Coerce (the cap is exactly 500)")

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "get_notebook",
		Params: map[string]any{"id": saved.ID},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	cells, _ := result["cells"].([]map[string]any)

	require.NotEmpty(t, cells)
	assert.Less(t, len(cells), 500, "500 cells at ~200 bytes each must not all fit under the budget")

	truncated, ok := result["truncated"].(map[string]any)
	require.True(t, ok, "response must report truncated: {shown, total}")
	assert.Equal(t, len(cells), truncated["shown"])
	assert.Equal(t, 500, truncated["total"])
}

// TestGetNotebook_NoTruncatedFieldWhenEverythingFits asserts a small
// notebook (well under the budget) gets no "truncated" field at all.
func TestGetNotebook_NoTruncatedFieldWhenEverythingFits(t *testing.T) {
	handler, nbs := newNotebookTestHandler(t, ModeFull)
	ctx := context.Background()
	nb, err := nbs.Create(ctx, "Small notebook")
	require.NoError(t, err)
	cell, err := notebooks.NewCell(notebooks.CellMarkdown)
	require.NoError(t, err)
	cell.Source = "hello"
	saved, _, err := saveWithNewCell(ctx, nbs, nb, cell)
	require.NoError(t, err)

	resp := handler.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: "1", Method: "get_notebook",
		Params: map[string]any{"id": saved.ID},
	})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	_, hasTruncated := result["truncated"]
	assert.False(t, hasTruncated)
}
