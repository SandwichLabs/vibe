package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/SandwichLabs/vibe/internal/notebooks"
)

// --- add_query_cell / create_dashboard (#67 deferred part) ---
//
// Unlike write_cell (task mode: propose, dialog shows a preview, user clicks
// Apply), these persist directly to the notebook store — the agent panel's
// chat/MCP surface has no proposal dialog, so the user's own ask ("add a
// dashboard of X to my notebook") is the confirmation. Both go through
// notebooks.Coerce (via notebooks.Save) exactly like every other notebook
// write, so a malformed cell can never be smuggled in.

// resolveTargetNotebook returns the notebook to append to: explicitID if
// given, else the Studio-tracked active notebook (h.resolveActiveNotebook()), else
// a brand-new notebook titled fallbackTitle. Mirrors the contract
// add_query_cell/create_dashboard's ToolDef descriptions promise.
func (h *Handler) resolveTargetNotebook(ctx context.Context, store notebooks.Store, explicitID, fallbackTitle string) (notebooks.Notebook, error) {
	id := strings.TrimSpace(explicitID)
	if id == "" {
		id = strings.TrimSpace(h.resolveActiveNotebook())
	}
	if id != "" {
		nb, warnings, err := store.Get(ctx, id)
		if err != nil {
			return notebooks.Notebook{}, fmt.Errorf("notebook %q not found: %w", id, err)
		}
		for _, w := range warnings {
			h.logger.Warn("notebook coercion warning", "notebook_id", id, "warning", w)
		}
		return nb, nil
	}
	title := strings.TrimSpace(fallbackTitle)
	if title == "" {
		title = "Untitled notebook"
	}
	return store.Create(ctx, title)
}

// saveWithNewCell appends cell to nb and persists it through
// notebooks.Save/Coerce (never a direct INSERT), returning the saved
// notebook and any coercion warnings.
func saveWithNewCell(ctx context.Context, store notebooks.Store, nb notebooks.Notebook, cell notebooks.Cell) (notebooks.Notebook, []string, error) {
	nb.Cells = append(nb.Cells, cell)
	raw, err := json.Marshal(nb)
	if err != nil {
		return notebooks.Notebook{}, nil, fmt.Errorf("failed to marshal notebook: %w", err)
	}
	return store.Save(ctx, raw)
}

func (h *Handler) handleAddQueryCell(ctx context.Context, req *Request) *Response {
	sqlStr := stringParam(req.Params["sql"])
	if strings.TrimSpace(sqlStr) == "" {
		return NewErrorResponse(req.ID, ErrInvalidParams, "sql is required")
	}

	if _, errResp := h.duckDB(req.ID); errResp != nil {
		return errResp
	}
	// An agent-authored cell may name the connection it runs on, validated
	// against the registered names; the cell is classified on that connection
	// so its unqualified names resolve the way they will when it runs.
	connection, cerr := h.validConnection(stringParam(req.Params["connection"]))
	if cerr != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, cerr.Error())
	}
	connectErr := h.connectReferenced(ctx, connection, sqlStr)
	v, verr := h.classifyIn(ctx, connection, sqlStr, nil)
	if verr != nil {
		return NewErrorResponse(req.ID, ErrInternal, verr.Error())
	}
	if err := h.sandboxFor(ctx).checkSQLClassReadOnly(v.Class, v.Reason); err != nil {
		h.logger.Debug("notebook cell denied", "reason", v.Reason)
		return NewErrorResponse(req.ID, ErrPermission, fmt.Sprintf("notebook sql cells are read-only queries: %v: %s", withConnectError(err, connectErr), v.Reason))
	}

	label := strings.TrimSpace(stringParam(req.Params["label"]))
	store := h.access.Notebooks()
	nb, err := h.resolveTargetNotebook(ctx, store, stringParam(req.Params["notebook_id"]), label)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}

	cell, err := notebooks.NewCell(notebooks.CellSQL)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	cell.Source = sqlStr
	cell.Connection = connection
	cell.Label = label
	if view := strings.TrimSpace(stringParam(req.Params["view"])); view != "" {
		cell.View = normalizeCellView(view)
		cell.ViewPinned = true
	}

	saved, warnings, err := saveWithNewCell(ctx, store, nb, cell)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	h.logger.Info("agent added query cell", "notebook_id", saved.ID, "cell_id", cell.ID)
	return NewResponse(req.ID, map[string]any{
		"notebook_id": saved.ID,
		"cell_id":     cell.ID,
		"title":       saved.Title,
		"warnings":    warnings,
	})
}

func (h *Handler) handleCreateDashboard(ctx context.Context, req *Request) *Response {
	page, err := a2ui.Validate(req.Params["page"])
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}

	store := h.access.Notebooks()
	nb, err := h.resolveTargetNotebook(ctx, store, stringParam(req.Params["notebook_id"]), page.Title)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}

	cell, err := notebooks.NewCell(notebooks.CellA2UI)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	cell.Page = &page

	saved, warnings, err := saveWithNewCell(ctx, store, nb, cell)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	h.logger.Info("agent added dashboard cell", "notebook_id", saved.ID, "cell_id", cell.ID)
	return NewResponse(req.ID, map[string]any{
		"notebook_id": saved.ID,
		"cell_id":     cell.ID,
		"title":       saved.Title,
		"warnings":    warnings,
	})
}

// --- list_notebooks / get_notebook (read-only, introspect category) ---
//
// These are the read path a chat/task/MCP client uses to see a workspace's
// notebooks without going through Studio at all — e.g. `vibe mcp serve`. Both are introspect-category (chat and MCP visible in every
// sandbox mode, including read-only) and return DATA, never rendered
// output: a sql/markdown cell's raw source, or a summary for an a2ui cell
// (there is no HTML/markdown renderer in this package to render it with).

// notebookCellSourceBudgetRunes caps how much of a single cell's source text
// (sql or markdown) is handed back per notebook cell, so one notebook with a
// handful of huge markdown cells can't blow the tool-result budget the way
// an unbounded run_query result could (see budget.go's RowResultBudgetChars
// for the equivalent on the data side). Generous enough for any real cell —
// this is a safety cap, not a normal-case truncation.
const notebookCellSourceBudgetRunes = 4000

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

	// Stop adding cells once the combined output reaches roughly
	// RowResultBudgetChars — the same tool-result budget run_query enforces
	// on rows (budget.go), applied here so a notebook with hundreds of
	// cells can't blow the same context/UI-event budget a giant run_query
	// result would. Always include at least one cell even if it alone
	// exceeds the budget (budgetRows' same reasoning: handing back zero
	// cells for a notebook that has real ones is worse than one visibly
	// large cell), and report exactly how much was cut via "truncated".
	cells := make([]map[string]any, 0, len(nb.Cells))
	used := 0
	for _, c := range nb.Cells {
		cell := map[string]any{"id": c.ID, "type": string(c.Type)}
		switch c.Type {
		case notebooks.CellSQL:
			source, truncated := truncateRunes(c.Source, notebookCellSourceBudgetRunes)
			cell["source"] = source
			cell["view"] = c.View
			cell["label"] = c.Label
			if truncated {
				cell["source_truncated"] = true
			}
		case notebooks.CellMarkdown:
			source, truncated := truncateRunes(c.Source, notebookCellSourceBudgetRunes)
			cell["source"] = source
			if truncated {
				cell["source_truncated"] = true
			}
		case notebooks.CellA2UI:
			// No renderer here (Go validates, Vue renders — #67) — a summary,
			// not the full component tree, keeps this from ballooning on a
			// large dashboard cell while still telling the caller it's there.
			title, count := "", 0
			if c.Page != nil {
				title = c.Page.Title
				count = len(c.Page.Components)
			}
			cell["title"] = title
			cell["component_count"] = count
		}

		size := estimateCellJSONSize(cell)
		if len(cells) > 0 && used+size > RowResultBudgetChars {
			break
		}
		cells = append(cells, cell)
		used += size
	}

	result := map[string]any{
		"id":         nb.ID,
		"title":      nb.Title,
		"updated_at": nb.UpdatedAt,
		"cells":      cells,
	}
	if len(cells) < len(nb.Cells) {
		result["truncated"] = map[string]any{"shown": len(cells), "total": len(nb.Cells)}
	}
	return NewResponse(req.ID, result)
}

// estimateCellJSONSize approximates a cell map's size once JSON-encoded in
// the tool result, for get_notebook's running budget check. Best-effort: a
// marshal failure (shouldn't happen for these plain map[string]any values)
// just estimates 0, which only risks slightly overrunning the budget on a
// single pathological cell, never underrunning it enough to matter.
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
