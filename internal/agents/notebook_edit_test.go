package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/a2ui"
	"github.com/SandwichLabs/slaw/internal/notebooks"
)

// editFixture is a full-access handler over a file-backed notebook store (so
// notebooks carry a rev) with one notebook: a sql cell, a markdown cell and an
// a2ui cell.
type editFixture struct {
	t     *testing.T
	h     *Handler
	store notebooks.Store
	nbID  string
}

func newEditFixture(t *testing.T) *editFixture {
	t.Helper()
	h, _ := newRealDuckDBHandler(t, ModeFull)
	acc, ok := h.access.(*mockAccess)
	require.True(t, ok)
	store := notebooks.NewFileStore(t.TempDir())
	acc.nbs = store

	nb := notebooks.Empty("Sales")
	sqlCell, _ := notebooks.NewCell(notebooks.CellSQL)
	sqlCell.ID, sqlCell.Source, sqlCell.Label = "c_sql", "SELECT * FROM t", "All rows"
	mdCell, _ := notebooks.NewCell(notebooks.CellMarkdown)
	mdCell.ID, mdCell.Source = "c_md", "# Intro"
	pageCell, _ := notebooks.NewCell(notebooks.CellA2UI)
	pageCell.ID = "c_page"
	pageCell.Page = &a2ui.Page{Title: "Board", Components: []a2ui.Component{{ID: "h", Type: "heading", Fields: map[string]any{"text": "Hi"}}}}
	nb.Cells = []notebooks.Cell{sqlCell, mdCell, pageCell}
	saved, _, err := store.Insert(context.Background(), nb)
	require.NoError(t, err)
	return &editFixture{t: t, h: h, store: store, nbID: saved.ID}
}

func (f *editFixture) call(method string, params map[string]any) *Response {
	f.t.Helper()
	return f.h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: "1", Method: method, Params: params})
}

func (f *editFixture) notebook() notebooks.Notebook {
	f.t.Helper()
	nb, _, err := f.store.Get(context.Background(), f.nbID)
	require.NoError(f.t, err)
	return nb
}

// edit calls an edit tool on the notebook at its current rev.
func (f *editFixture) edit(method string, params map[string]any) *Response {
	f.t.Helper()
	p := map[string]any{"notebook_id": f.nbID, "rev": f.notebook().Rev}
	for k, v := range params {
		p[k] = v
	}
	return f.call(method, p)
}

func (f *editFixture) ids() []string {
	var out []string
	for _, c := range f.notebook().Cells {
		out = append(out, c.ID)
	}
	return out
}

func okResult(t *testing.T, resp *Response) map[string]any {
	t.Helper()
	require.Nil(t, resp.Error, errMsg(resp))
	m, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	return m
}

func TestUpdateCell_SQL(t *testing.T) {
	f := newEditFixture(t)
	before := f.notebook().Rev
	m := okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_sql", "sql": "SELECT 1 AS one;", "label": "One", "view": "BAR"}))
	assert.NotEqual(t, before, m["rev"], "the result carries the new rev")
	assert.Equal(t, f.notebook().Rev, m["rev"])
	c := f.notebook().Cells[0]
	assert.Equal(t, "SELECT 1 AS one;", c.Source)
	assert.Equal(t, "One", c.Label)
	assert.Equal(t, "bar", c.View)
	assert.True(t, c.ViewPinned)
	assert.Len(t, f.notebook().Cells, 3, "the other cells are untouched")
}

func TestUpdateCell_LabelAndViewSkipTheSQLVet(t *testing.T) {
	f := newEditFixture(t)
	// A stored cell whose SQL would no longer vet can still be relabelled.
	okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_sql", "label": ""}))
	assert.Equal(t, "", f.notebook().Cells[0].Label, "an empty label clears it")
}

func TestUpdateCell_Markdown(t *testing.T) {
	f := newEditFixture(t)
	okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_md", "markdown": "## New"}))
	assert.Equal(t, "## New", f.notebook().Cells[1].Source)
}

func TestUpdateCell_Page(t *testing.T) {
	f := newEditFixture(t)
	page := map[string]any{"title": "Board 2", "components": []any{map[string]any{"id": "h", "type": "heading", "text": "Bye"}}}
	okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_page", "page": page}))
	assert.Equal(t, "Board 2", f.notebook().Cells[2].Page.Title)
}

func TestUpdateCell_StaleRevChangesNothing(t *testing.T) {
	for _, tool := range []struct {
		name   string
		params map[string]any
	}{
		{"update_cell", map[string]any{"cell_id": "c_md", "markdown": "x"}},
		{"delete_cell", map[string]any{"cell_id": "c_md"}},
		{"move_cell", map[string]any{"cell_id": "c_md", "position": 0}},
	} {
		t.Run(tool.name, func(t *testing.T) {
			f := newEditFixture(t)
			stale := f.notebook().Rev
			okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_md", "markdown": "someone else"}))
			p := map[string]any{"notebook_id": f.nbID, "rev": stale}
			for k, v := range tool.params {
				p[k] = v
			}
			resp := f.call(tool.name, p)
			require.NotNil(t, resp.Error)
			assert.Equal(t, ErrInvalidParams, resp.Error.Code)
			assert.Contains(t, resp.Error.Message, "changed since the rev you sent")
			assert.Contains(t, resp.Error.Message, "get_notebook")
			assert.Equal(t, "someone else", f.notebook().Cells[1].Source)
			assert.Len(t, f.ids(), 3)
		})
	}
}

func TestEditTools_RequireNotebookCellAndRev(t *testing.T) {
	f := newEditFixture(t)
	rev := f.notebook().Rev
	cases := map[string]map[string]any{
		`missing required parameter: "notebook_id"`: {"cell_id": "c_md", "rev": rev, "markdown": "x"},
		`missing required parameter: "cell_id"`:     {"notebook_id": f.nbID, "rev": rev, "markdown": "x"},
		"rev is required":                           {"notebook_id": f.nbID, "cell_id": "c_md", "rev": "", "markdown": "x"},
	}
	for want, p := range cases {
		resp := f.call("update_cell", p)
		require.NotNil(t, resp.Error, want)
		assert.Contains(t, resp.Error.Message, want)
	}
	resp := f.call("update_cell", map[string]any{"notebook_id": "nope", "cell_id": "c", "rev": "r", "markdown": "x"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "not found")
}

func TestEditTools_UnknownCellID(t *testing.T) {
	for _, tool := range []string{"update_cell", "delete_cell", "move_cell"} {
		f := newEditFixture(t)
		resp := f.edit(tool, map[string]any{"cell_id": "ghost", "markdown": "x", "position": 0})
		require.NotNil(t, resp.Error, tool)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, `"ghost"`)
		assert.Contains(t, resp.Error.Message, "c_sql, c_md, c_page", "the message lists the real ids")
	}
}

func TestUpdateCell_InvalidContentIsRefusedAndNothingWritten(t *testing.T) {
	f := newEditFixture(t)
	before := f.notebook()
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"write sql", map[string]any{"cell_id": "c_sql", "sql": "DELETE FROM t"}, "read-only"},
		{"two statements", map[string]any{"cell_id": "c_sql", "sql": "SELECT 1; SELECT 2"}, "single statement"},
		{"empty sql", map[string]any{"cell_id": "c_sql", "sql": " "}, "must not be empty"},
		{"unknown view", map[string]any{"cell_id": "c_sql", "view": "pie"}, "table, bar, line"},
		{"unknown connection", map[string]any{"cell_id": "c_sql", "connection": "nowhere"}, "no connection named"},
		{"markdown on a sql cell", map[string]any{"cell_id": "c_sql", "markdown": "x"}, "does not apply to a sql cell"},
		{"sql on a markdown cell", map[string]any{"cell_id": "c_md", "sql": "SELECT 1"}, "does not apply to a markdown cell"},
		{"blank markdown", map[string]any{"cell_id": "c_md", "markdown": ""}, "must not be empty"},
		{"huge markdown", map[string]any{"cell_id": "c_md", "markdown": strings.Repeat("x", notebooks.MaxCellSourceBytes+1)}, "at most"},
		{"nothing to change", map[string]any{"cell_id": "c_md"}, "nothing to change"},
		{"bad page", map[string]any{"cell_id": "c_page", "page": map[string]any{"title": "T", "components": []any{map[string]any{"id": "a", "type": "nope"}}}}, ""},
		{"page with write sql", map[string]any{"cell_id": "c_page", "page": map[string]any{"title": "T", "components": []any{map[string]any{"id": "a", "type": "stat", "label": "n", "sql": "DELETE FROM t"}}}}, "components[0].sql"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := f.edit("update_cell", c.params)
			require.NotNil(t, resp.Error)
			assert.Contains(t, resp.Error.Message, c.want)
			assert.Equal(t, before.Rev, f.notebook().Rev, "nothing was written")
		})
	}
}

func TestUpdateCell_DryRunValidatesAndWritesNothing(t *testing.T) {
	f := newEditFixture(t)
	rev := f.notebook().Rev
	p := map[string]any{"notebook_id": f.nbID, "rev": rev, "cell_id": "c_sql", "sql": "SELECT 2"}

	resp := f.h.dispatch(withDryRun(context.Background()), &Request{ID: "1", Method: "update_cell", Params: p})
	require.Nil(t, resp.Error)
	assert.Equal(t, dryRunOK("1").Result, resp.Result)
	assert.Equal(t, rev, f.notebook().Rev)

	p["sql"] = "DROP TABLE t"
	resp = f.h.dispatch(withDryRun(context.Background()), &Request{ID: "1", Method: "update_cell", Params: p})
	require.NotNil(t, resp.Error, "an invalid change is refused in a dry run too")
}

func TestDeleteCell(t *testing.T) {
	f := newEditFixture(t)
	m := okResult(t, f.edit("delete_cell", map[string]any{"cell_id": "c_md"}))
	assert.Equal(t, []string{"c_sql", "c_page"}, f.ids())
	assert.EqualValues(t, 2, m["cell_count"])
	assert.Equal(t, f.notebook().Rev, m["rev"])
}

func TestMoveCell(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{"to the front", map[string]any{"cell_id": "c_page", "position": 0}, []string{"c_page", "c_sql", "c_md"}},
		{"float position from JSON", map[string]any{"cell_id": "c_sql", "position": float64(2)}, []string{"c_md", "c_page", "c_sql"}},
		{"after a later cell", map[string]any{"cell_id": "c_sql", "after_cell_id": "c_md"}, []string{"c_md", "c_sql", "c_page"}},
		{"after an earlier cell", map[string]any{"cell_id": "c_page", "after_cell_id": "c_sql"}, []string{"c_sql", "c_page", "c_md"}},
		{"already there", map[string]any{"cell_id": "c_sql", "position": 0}, []string{"c_sql", "c_md", "c_page"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newEditFixture(t)
			okResult(t, f.edit("move_cell", c.params))
			assert.Equal(t, c.want, f.ids())
		})
	}
}

func TestMoveCell_Refusals(t *testing.T) {
	f := newEditFixture(t)
	cases := map[string]map[string]any{
		"send after_cell_id":         {"cell_id": "c_sql"},
		"not both":                   {"cell_id": "c_sql", "position": 1, "after_cell_id": "c_md"},
		"from 0 (first) to 2":        {"cell_id": "c_sql", "position": 3},
		"names the cell being moved": {"cell_id": "c_sql", "after_cell_id": "c_sql"},
		`"ghost"`:                    {"cell_id": "c_sql", "after_cell_id": "ghost"},
	}
	for want, p := range cases {
		resp := f.edit("move_cell", p)
		require.NotNil(t, resp.Error, want)
		assert.Contains(t, resp.Error.Message, want)
	}
	assert.Equal(t, []string{"c_sql", "c_md", "c_page"}, f.ids())
}

func TestAddMarkdownCell(t *testing.T) {
	f := newEditFixture(t)
	m := okResult(t, f.call("add_markdown_cell", map[string]any{"notebook_id": f.nbID, "markdown": "Closing notes"}))
	assert.Equal(t, f.notebook().Rev, m["rev"])
	ids := f.ids()
	require.Len(t, ids, 4)
	assert.Equal(t, m["cell_id"], ids[3])
	assert.Equal(t, notebooks.CellMarkdown, f.notebook().Cells[3].Type)

	m = okResult(t, f.call("add_markdown_cell", map[string]any{"notebook_id": f.nbID, "markdown": "Second", "after_cell_id": "c_sql"}))
	assert.Equal(t, m["cell_id"], f.ids()[1], "inserted right after c_sql")
}

func TestAddMarkdownCell_UsesTheOpenNotebookOrCreatesOne(t *testing.T) {
	f := newEditFixture(t)
	f.h.SetActiveNotebook(f.nbID)
	okResult(t, f.call("add_markdown_cell", map[string]any{"markdown": "into the open one"}))
	assert.Len(t, f.ids(), 4)

	f.h.SetActiveNotebook("")
	m := okResult(t, f.call("add_markdown_cell", map[string]any{"markdown": "## Fresh start\n\nbody"}))
	assert.Equal(t, "Fresh start", m["title"])
	assert.NotEqual(t, f.nbID, m["notebook_id"])
}

func TestAddMarkdownCell_Refusals(t *testing.T) {
	f := newEditFixture(t)
	cases := map[string]map[string]any{
		"must not be empty":     {"markdown": " "},
		"at most":               {"markdown": strings.Repeat("x", notebooks.MaxCellSourceBytes+1)},
		`"ghost"`:               {"markdown": "x", "notebook_id": f.nbID, "after_cell_id": "ghost"},
		"needs an existing":     {"markdown": "x", "after_cell_id": "c_sql"},
		"notebook \"nope\" not": {"markdown": "x", "notebook_id": "nope"},
	}
	for want, p := range cases {
		resp := f.call("add_markdown_cell", p)
		require.NotNil(t, resp.Error, want)
		assert.Contains(t, resp.Error.Message, want)
	}
	assert.Len(t, f.ids(), 3)
	list, err := f.store.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1, "a refusal creates no notebook")
}

func TestAddQueryCell_RefusesUnknownViewAndMultipleStatements(t *testing.T) {
	f := newEditFixture(t)
	resp := f.call("add_query_cell", map[string]any{"notebook_id": f.nbID, "sql": "SELECT 1", "view": "pie"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, strings.Join(notebooks.Views, ", "))

	resp = f.call("add_query_cell", map[string]any{"notebook_id": f.nbID, "sql": "SELECT 1; SELECT 2"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "single statement")
	assert.Len(t, f.ids(), 3)

	m := okResult(t, f.call("add_query_cell", map[string]any{"notebook_id": f.nbID, "sql": "SELECT 1;", "view": " Line "}))
	assert.NotEmpty(t, m["rev"])
	last := f.notebook().Cells[3]
	assert.Equal(t, "line", last.View)
	assert.True(t, last.ViewPinned)
}

func TestGetNotebook_FullCells(t *testing.T) {
	f := newEditFixture(t)
	okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_sql", "connection": "", "view": "stat"}))
	m := okResult(t, f.call("get_notebook", map[string]any{"id": f.nbID}))
	assert.Equal(t, f.notebook().Rev, m["rev"])

	raw, err := json.Marshal(m["cells"])
	require.NoError(t, err)
	var cells []map[string]any
	require.NoError(t, json.Unmarshal(raw, &cells))
	require.Len(t, cells, 3)

	assert.Equal(t, "c_sql", cells[0]["id"])
	assert.Equal(t, "SELECT * FROM t", cells[0]["source"])
	assert.Equal(t, "stat", cells[0]["view"])
	assert.Equal(t, true, cells[0]["view_pinned"])
	assert.Equal(t, "All rows", cells[0]["label"])
	assert.Contains(t, cells[0], "connection")

	assert.Equal(t, "# Intro", cells[1]["source"])

	page, ok := cells[2]["page"].(map[string]any)
	require.True(t, ok, "the whole a2ui page is returned")
	assert.Equal(t, "Board", page["title"])
	comps, _ := page["components"].([]any)
	require.Len(t, comps, 1)
	assert.Equal(t, "Hi", as[map[string]any](t, comps[0])["text"])
	assert.EqualValues(t, 1, cells[2]["component_count"])
}

func TestGetNotebook_PageRoundTripsIntoUpdateCell(t *testing.T) {
	f := newEditFixture(t)
	m := okResult(t, f.call("get_notebook", map[string]any{"id": f.nbID, "cell_id": "c_page"}))
	cells := as[[]map[string]any](t, m["cells"])
	require.Len(t, cells, 1)
	raw, err := json.Marshal(cells[0]["page"])
	require.NoError(t, err)
	var page map[string]any
	require.NoError(t, json.Unmarshal(raw, &page))
	page["title"] = "Edited"
	okResult(t, f.call("update_cell", map[string]any{"notebook_id": f.nbID, "rev": m["rev"], "cell_id": "c_page", "page": page}))
	assert.Equal(t, "Edited", f.notebook().Cells[2].Page.Title)
}

func TestGetNotebook_SingleCellIsUncutAndUnknownCellRefused(t *testing.T) {
	f := newEditFixture(t)
	long := strings.Repeat("a", notebookCellSourceBudgetRunes+500)
	okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_md", "markdown": long}))

	m := okResult(t, f.call("get_notebook", map[string]any{"id": f.nbID}))
	cut := as[[]map[string]any](t, m["cells"])[1]
	assert.Equal(t, true, cut["source_truncated"])

	m = okResult(t, f.call("get_notebook", map[string]any{"id": f.nbID, "cell_id": "c_md"}))
	one := as[[]map[string]any](t, m["cells"])
	require.Len(t, one, 1)
	assert.Equal(t, long, one[0]["source"])
	assert.NotContains(t, one[0], "source_truncated")

	resp := f.call("get_notebook", map[string]any{"id": f.nbID, "cell_id": "ghost"})
	require.NotNil(t, resp.Error)
}

func TestGetNotebook_BudgetListsOmittedCells(t *testing.T) {
	f := newEditFixture(t)
	for i := 0; i < 8; i++ {
		okResult(t, f.call("add_markdown_cell", map[string]any{"notebook_id": f.nbID, "markdown": strings.Repeat("w", notebookCellSourceBudgetRunes)}))
	}
	m := okResult(t, f.call("get_notebook", map[string]any{"id": f.nbID}))
	tr, ok := m["truncated"].(map[string]any)
	require.True(t, ok, "11 cells of this size exceed the result budget")
	omitted := as[[]map[string]any](t, tr["omitted"])
	assert.Equal(t, 11, tr["total"])
	assert.Equal(t, 11-as[int](t, tr["shown"]), len(omitted))
	assert.Contains(t, tr["note"], "cell_id")
	assert.Equal(t, f.ids()[as[int](t, tr["shown"])], omitted[0]["id"])
}

// as asserts v has type T.
func as[T any](t *testing.T, v any) T {
	t.Helper()
	out, ok := v.(T)
	require.True(t, ok, "unexpected type %T", v)
	return out
}

func TestApprovalCard_NamesTheCellAndFlagsDelete(t *testing.T) {
	f := newEditFixture(t)
	req := &Request{Method: "delete_cell", Params: map[string]any{"notebook_id": f.nbID, "cell_id": "c_sql"}}
	ar := describeWrite(context.Background(), req, nil, sqlClassWrite)
	assert.True(t, ar.Destructive)
	f.h.describeCellEdit(context.Background(), req, &ar)
	assert.Contains(t, ar.Summary, `Delete sql cell "All rows" (SELECT * FROM t) from notebook "Sales"`)

	req = &Request{Method: "update_cell", Params: map[string]any{"notebook_id": f.nbID, "cell_id": "ghost"}}
	ar = describeWrite(context.Background(), req, nil, sqlClassWrite)
	f.h.describeCellEdit(context.Background(), req, &ar)
	assert.Contains(t, ar.Summary, "ghost", "an unknown cell falls back to the id")
}

func TestMoveCell_ToItsCurrentIndexWritesNothing(t *testing.T) {
	f := newEditFixture(t)
	rev := f.notebook().Rev
	m := okResult(t, f.edit("move_cell", map[string]any{"cell_id": "c_md", "position": 1}))
	assert.Equal(t, true, m["unchanged"])
	assert.Equal(t, rev, f.notebook().Rev)
}

func TestUpdateCell_NullSQLIsAbsent(t *testing.T) {
	f := newEditFixture(t)
	okResult(t, f.edit("update_cell", map[string]any{"cell_id": "c_sql", "sql": nil, "label": "Renamed"}))
	assert.Equal(t, "Renamed", f.notebook().Cells[0].Label)
	assert.Equal(t, "SELECT * FROM t", f.notebook().Cells[0].Source)
}
