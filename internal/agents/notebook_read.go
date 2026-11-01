package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/SandwichLabs/assay/internal/notebooks"
)

// --- list_notebooks / get_notebook (read-only, introspect category) ---
//
// These are the read path a chat/task/MCP client uses to see a workspace's
// notebooks without going through Studio at all, e.g. `assay mcp serve`. Both
// are introspect-category (chat and MCP visible in every sandbox mode,
// including read-only) and return DATA, never rendered output: a cell's raw
// source, or an a2ui cell's whole page, plus the notebook's rev, which the
// edit tools (update_cell, delete_cell, move_cell) send back.

// notebookCellSourceBudgetRunes caps how much of a single cell's source text
// (sql or markdown) is handed back per notebook cell, so one notebook with a
// handful of huge markdown cells can't blow the tool-result budget the way
// an unbounded run_query result could (see budget.go's RowResultBudgetChars
// for the equivalent on the data side). Generous enough for any real cell;
// this is a safety cap, not a normal-case truncation. get_notebook with a
// cell_id returns that one cell uncapped.
const notebookCellSourceBudgetRunes = 4000

// maxOmittedCellsListed bounds the list of cells a budget-truncated
// get_notebook names so the agent can fetch them one at a time.
const maxOmittedCellsListed = 50

func (h *Handler) handleListNotebooks(ctx context.Context, req *Request) *Response {
	list, err := h.access.Notebooks().List(ctx)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to list notebooks: %v", err))
	}
	out := make([]map[string]any, 0, len(list))
	for _, nb := range list {
		out = append(out, map[string]any{
			"id":         nb.ID,
			"title":      nb.Title,
			"cell_count": nb.CellCount,
			"updated_at": nb.UpdatedAt,
		})
	}
	return NewResponse(req.ID, map[string]any{"notebooks": out})
}

func (h *Handler) handleGetNotebook(ctx context.Context, req *Request) *Response {
	id := strings.TrimSpace(stringParam(req.Params["id"]))
	if id == "" {
		return NewErrorResponse(req.ID, ErrInvalidParams, "id is required")
	}

	nb, warnings, err := h.access.Notebooks().Get(ctx, id)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("notebook %q not found: %v", id, err))
	}
	for _, w := range warnings {
		h.logger.Warn("notebook coercion warning on get_notebook", "notebook_id", id, "warning", w)
	}

	result := map[string]any{
		"id":         nb.ID,
		"title":      nb.Title,
		"rev":        nb.Rev,
		"updated_at": nb.UpdatedAt,
	}
	if cellID := strings.TrimSpace(stringParam(req.Params["cell_id"])); cellID != "" {
		idx := cellIndex(nb, cellID)
		if idx < 0 {
			return NewErrorResponse(req.ID, ErrInvalidParams, cellNotFoundMessage(nb, cellID))
		}
		result["cells"] = []map[string]any{notebookCellData(nb.Cells[idx], false)}
		return NewResponse(req.ID, result)
	}

	cells, omitted := budgetNotebookCells(nb.Cells)
	result["cells"] = cells
	if len(omitted) > 0 {
		list := omitted
		if len(list) > maxOmittedCellsListed {
			list = list[:maxOmittedCellsListed]
		}
		result["truncated"] = map[string]any{
			"shown": len(cells), "total": len(nb.Cells),
			"omitted": list,
			"note":    "Stopped at the tool-result budget. Fetch an omitted cell whole with get_notebook {id, cell_id}.",
		}
	}
	return NewResponse(req.ID, result)
}

// budgetNotebookCells keeps cells until their combined JSON reaches roughly
// RowResultBudgetChars, the same tool-result budget run_query enforces on
// rows (budget.go), so a notebook with hundreds of cells can't blow the same
// context/UI-event budget a giant run_query result would. At least one cell is
// always kept even if it alone exceeds the budget (budgetRows' reasoning:
// zero cells for a notebook that has real ones is worse than one visibly
// large cell). The rest are named in omitted.
func budgetNotebookCells(all []notebooks.Cell) (cells []map[string]any, omitted []map[string]any) {
	cells = make([]map[string]any, 0, len(all))
	used := 0
	for i, c := range all {
		cell := notebookCellData(c, true)
		size := estimateCellJSONSize(cell)
		if len(cells) > 0 && used+size > RowResultBudgetChars {
			for _, rest := range all[i:] {
				omitted = append(omitted, map[string]any{"id": rest.ID, "type": string(rest.Type)})
			}
			break
		}
		cells = append(cells, cell)
		used += size
	}
	return cells, omitted
}

// notebookCellData is one cell as get_notebook reports it: everything the
// edit tools take back. capSource truncates long sql/markdown source.
func notebookCellData(c notebooks.Cell, capSource bool) map[string]any {
	cell := map[string]any{"id": c.ID, "type": string(c.Type)}
	source := func() {
		src, truncated := c.Source, false
		if capSource {
			src, truncated = truncateRunes(c.Source, notebookCellSourceBudgetRunes)
		}
		cell["source"] = src
		if truncated {
			cell["source_truncated"] = true
		}
	}
	switch c.Type {
	case notebooks.CellSQL:
		source()
		cell["view"] = c.View
		cell["view_pinned"] = c.ViewPinned
		cell["label"] = c.Label
		cell["connection"] = c.Connection
	case notebooks.CellMarkdown:
		source()
	case notebooks.CellA2UI:
		if c.Page != nil {
			cell["title"] = c.Page.Title
			cell["component_count"] = len(c.Page.Components)
			cell["page"] = c.Page
		}
	}
	return cell
}

// estimateCellJSONSize approximates a cell map's size once JSON-encoded in
// the tool result, for get_notebook's running budget check. Best-effort: a
// marshal failure (shouldn't happen for these plain values) just estimates
// 0, which only risks slightly overrunning the budget on a single
// pathological cell, never underrunning it enough to matter.
func estimateCellJSONSize(cell map[string]any) int {
	b, err := json.Marshal(cell)
	if err != nil {
		return 0
	}
	return len(b)
}

// truncateRunes caps s at maxRunes runes, reporting whether it cut anything.
func truncateRunes(s string, maxRunes int) (string, bool) {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s, false
	}
	r := []rune(s)
	return string(r[:maxRunes]), true
}
