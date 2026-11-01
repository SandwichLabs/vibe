package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SandwichLabs/slaw/internal/a2ui"
	"github.com/SandwichLabs/slaw/internal/notebooks"
)

// --- add_markdown_cell / update_cell / delete_cell / move_cell (#220) ---
//
// Like add_query_cell and create_dashboard these persist straight to the
// notebook store (the person's ask is the confirmation, and the approval gate
// still applies when it is on). Unlike them, the three that change cells the
// agent did not just write are guarded by the notebook's rev: the agent sends
// back the rev get_notebook returned, and a notebook that changed since is
// refused with a pointer to re-read, so an edit can never silently undo a
// person's. Every one honours a dry run: all validation first, then the write.

// maxCellIDsListed bounds the ids a "no such cell" message lists.
const maxCellIDsListed = 20

// cellIndex returns the position of the cell with this id, or -1.
func cellIndex(nb notebooks.Notebook, id string) int {
	return slices.IndexFunc(nb.Cells, func(c notebooks.Cell) bool { return c.ID == id })
}

func cellNotFoundMessage(nb notebooks.Notebook, id string) string {
	ids := make([]string, 0, len(nb.Cells))
	for _, c := range nb.Cells {
		ids = append(ids, c.ID)
	}
	more := ""
	if len(ids) > maxCellIDsListed {
		ids, more = ids[:maxCellIDsListed], ", ..."
	}
	return fmt.Sprintf("cell %q is not in notebook %q (its cells: %s%s). Call get_notebook for the current cells.", id, nb.ID, strings.Join(ids, ", "), more)
}

// notebookConflictMessage is the refusal for a stale rev.
func notebookConflictMessage(id string) string {
	return fmt.Sprintf("notebook %q changed since the rev you sent (a person or another tool edited it), so nothing was written. Call get_notebook again, apply your change to what it returns, and send the new \"rev\".", id)
}

// optionalCellView validates a model-supplied view against notebooks.Views. An
// empty value means "not given" and returns "".
func optionalCellView(raw any) (string, error) {
	v := strings.ToLower(strings.TrimSpace(stringParam(raw)))
	if v == "" {
		return "", nil
	}
	if !slices.Contains(notebooks.Views, v) {
		return "", fmt.Errorf("unknown view %q: use one of %s", v, strings.Join(notebooks.Views, ", "))
	}
	return v, nil
}

// checkMarkdown refuses blank or oversized markdown instead of letting Coerce
// truncate it.
func checkMarkdown(md string) error {
	if strings.TrimSpace(md) == "" {
		return errors.New("markdown must not be empty")
	}
	if len(md) > notebooks.MaxCellSourceBytes {
		return fmt.Errorf("markdown is %d bytes; a cell holds at most %d. Split it across cells", len(md), notebooks.MaxCellSourceBytes)
	}
	return nil
}

// markdownTitle is the title a notebook created by add_markdown_cell gets: the
// first non-empty line, without heading marks.
func markdownTitle(md string) string {
	for _, line := range strings.Split(md, "\n") {
		if t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")); t != "" {
			return t
		}
	}
	return ""
}

func (h *Handler) handleAddMarkdownCell(ctx context.Context, req *Request) *Response {
	md := stringParam(req.Params["markdown"])
	if err := checkMarkdown(md); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	store := h.access.Notebooks()
	nb, found, err := h.existingTarget(ctx, store, stringParam(req.Params["notebook_id"]))
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	insertAt := len(nb.Cells)
	if after := strings.TrimSpace(stringParam(req.Params["after_cell_id"])); after != "" {
		if !found {
			return NewErrorResponse(req.ID, ErrInvalidParams, "after_cell_id needs an existing notebook: pass notebook_id (see list_notebooks)")
		}
		i := cellIndex(nb, after)
		if i < 0 {
			return NewErrorResponse(req.ID, ErrInvalidParams, cellNotFoundMessage(nb, after))
		}
		insertAt = i + 1
	}
	if isDryRun(ctx) {
		return dryRunOK(req.ID)
	}
	if !found {
		title := markdownTitle(md)
		if title == "" {
			title = "Untitled notebook"
		}
		if nb, err = store.Create(ctx, title); err != nil {
			return NewErrorResponse(req.ID, ErrInternal, err.Error())
		}
	}
	cell, err := notebooks.NewCell(notebooks.CellMarkdown)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	cell.Source = md
	nb.Cells = slices.Insert(nb.Cells, insertAt, cell)
	saved, warnings, err := saveNotebook(ctx, store, nb)
	if err != nil {
		return saveFailure(req.ID, nb.ID, err)
	}
	h.logger.Info("agent added markdown cell", "notebook_id", saved.ID, "cell_id", cell.ID)
	result := map[string]any{
		"notebook_id": saved.ID, "cell_id": cell.ID, "title": saved.Title,
		"rev": saved.Rev, "warnings": warnings,
	}
	if insertAt < len(nb.Cells)-1 {
		result["inserted"] = true // before the end: Studio reloads the open copy
	}
	return NewResponse(req.ID, result)
}

// saveFailure turns a failed save into the agent's response: a conflict is the
// agent's to resolve (re-read), anything else is internal.
func saveFailure(id any, notebookID string, err error) *Response {
	if errors.Is(err, notebooks.ErrConflict) {
		return NewErrorResponse(id, ErrInvalidParams, notebookConflictMessage(notebookID))
	}
	return NewErrorResponse(id, ErrInternal, err.Error())
}

// notebookEdit is a notebook loaded for a rev-guarded change, with the index
// of the cell it targets.
type notebookEdit struct {
	nb  notebooks.Notebook
	rev string
	idx int
}

// loadForEdit loads the named notebook and checks the rev and the cell id the
// request carries. The first two are always named (an edit never falls back on
// the notebook open in Studio: the rev belongs to one notebook).
func (h *Handler) loadForEdit(ctx context.Context, req *Request) (*notebookEdit, *Response) {
	id := strings.TrimSpace(stringParam(req.Params["notebook_id"]))
	cellID := strings.TrimSpace(stringParam(req.Params["cell_id"]))
	rev := strings.TrimSpace(stringParam(req.Params["rev"]))
	switch {
	case id == "":
		return nil, NewErrorResponse(req.ID, ErrInvalidParams, "notebook_id is required (see list_notebooks)")
	case cellID == "":
		return nil, NewErrorResponse(req.ID, ErrInvalidParams, "cell_id is required (see get_notebook)")
	case rev == "":
		return nil, NewErrorResponse(req.ID, ErrInvalidParams, "rev is required: send the \"rev\" get_notebook returned, so an edit cannot overwrite a change you have not seen")
	}
	nb, _, err := h.access.Notebooks().Get(ctx, id)
	if err != nil {
		return nil, NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("notebook %q not found: %v", id, err))
	}
	if nb.Rev != rev {
		return nil, NewErrorResponse(req.ID, ErrInvalidParams, notebookConflictMessage(id))
	}
	idx := cellIndex(nb, cellID)
	if idx < 0 {
		return nil, NewErrorResponse(req.ID, ErrInvalidParams, cellNotFoundMessage(nb, cellID))
	}
	return &notebookEdit{nb: nb, rev: rev, idx: idx}, nil
}

// commit saves the edited notebook (guarded by the rev) and builds the result.
func (h *Handler) commit(ctx context.Context, req *Request, e *notebookEdit, what string) *Response {
	cellID := stringParam(req.Params["cell_id"])
	saved, warnings, err := saveNotebook(ctx, h.access.Notebooks(), e.nb, notebooks.IfRev(e.rev))
	if err != nil {
		return saveFailure(req.ID, e.nb.ID, err)
	}
	h.logger.Info("agent changed notebook", "change", what, "notebook_id", saved.ID, "cell_id", cellID)
	return NewResponse(req.ID, map[string]any{
		"notebook_id": saved.ID, "cell_id": cellID, "rev": saved.Rev,
		"cell_count": len(saved.Cells), "warnings": warnings,
	})
}

func (h *Handler) handleDeleteCell(ctx context.Context, req *Request) *Response {
	e, errResp := h.loadForEdit(ctx, req)
	if errResp != nil {
		return errResp
	}
	if isDryRun(ctx) {
		return dryRunOK(req.ID)
	}
	e.nb.Cells = slices.Delete(e.nb.Cells, e.idx, e.idx+1)
	return h.commit(ctx, req, e, "delete_cell")
}

func (h *Handler) handleMoveCell(ctx context.Context, req *Request) *Response {
	e, errResp := h.loadForEdit(ctx, req)
	if errResp != nil {
		return errResp
	}
	target, err := moveTarget(e.nb, e.idx, req.Params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	if isDryRun(ctx) {
		return dryRunOK(req.ID)
	}
	if target == e.idx {
		return NewResponse(req.ID, map[string]any{
			"notebook_id": e.nb.ID, "cell_id": e.nb.Cells[e.idx].ID, "rev": e.rev,
			"cell_count": len(e.nb.Cells), "warnings": []string{}, "unchanged": true,
		})
	}
	cell := e.nb.Cells[e.idx]
	e.nb.Cells = slices.Delete(e.nb.Cells, e.idx, e.idx+1)
	e.nb.Cells = slices.Insert(e.nb.Cells, target, cell)
	return h.commit(ctx, req, e, "move_cell")
}

// moveTarget resolves after_cell_id | position into the cell's final index.
func moveTarget(nb notebooks.Notebook, idx int, params map[string]any) (int, error) {
	after := strings.TrimSpace(stringParam(params["after_cell_id"]))
	pos, hasPos := params["position"]
	switch {
	case after != "" && hasPos && pos != nil:
		return 0, errors.New("send either after_cell_id or position, not both")
	case after != "":
		if after == nb.Cells[idx].ID {
			return 0, errors.New("after_cell_id names the cell being moved")
		}
		i := cellIndex(nb, after)
		if i < 0 {
			return 0, errors.New(cellNotFoundMessage(nb, after))
		}
		if i > idx {
			i-- // removing the cell first shifts the anchor left
		}
		return i + 1, nil
	case hasPos && pos != nil:
		n, ok := intValue(pos)
		if !ok || n < 0 || n >= len(nb.Cells) {
			return 0, fmt.Errorf("position must be a whole number from 0 (first) to %d (last)", len(nb.Cells)-1)
		}
		return n, nil
	default:
		return 0, errors.New("send after_cell_id (the cell to place this one after) or position (0 = first)")
	}
}

// intValue reads a JSON number (or a Go int, in tests) that is a whole number.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

// cellFields are the update_cell parameters that change a cell's content, and
// which cell types each applies to.
var cellFields = map[string][]notebooks.CellType{
	"sql":        {notebooks.CellSQL},
	"label":      {notebooks.CellSQL},
	"view":       {notebooks.CellSQL},
	"connection": {notebooks.CellSQL},
	"markdown":   {notebooks.CellMarkdown},
	"page":       {notebooks.CellA2UI},
}

// checkUpdateFields refuses a parameter that does not apply to the cell's type
// (a markdown on a sql cell would otherwise be silently ignored) and a request
// that changes nothing.
func checkUpdateFields(params map[string]any, cellType notebooks.CellType) error {
	given := 0
	names := make([]string, 0, len(cellFields))
	for name := range cellFields {
		names = append(names, name)
	}
	slices.Sort(names) // a deterministic error when several fields are wrong
	for _, name := range names {
		types := cellFields[name]
		if v, ok := params[name]; !ok || v == nil {
			continue
		}
		given++
		if !slices.Contains(types, cellType) {
			return fmt.Errorf("%s does not apply to a %s cell (a %s cell takes %s)", name, cellType, cellType, strings.Join(fieldsFor(cellType), ", "))
		}
	}
	if given == 0 {
		return fmt.Errorf("nothing to change: send %s for this %s cell", strings.Join(fieldsFor(cellType), ", "), cellType)
	}
	return nil
}

func fieldsFor(t notebooks.CellType) []string {
	var out []string
	for name, types := range cellFields {
		if slices.Contains(types, t) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func (h *Handler) handleUpdateCell(ctx context.Context, req *Request) *Response {
	e, errResp := h.loadForEdit(ctx, req)
	if errResp != nil {
		return errResp
	}
	cell := &e.nb.Cells[e.idx]
	if err := checkUpdateFields(req.Params, cell.Type); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, "update_cell: "+err.Error())
	}
	var errResp2 *Response
	switch cell.Type {
	case notebooks.CellSQL:
		errResp2 = h.updateSQLCell(ctx, req, cell)
	case notebooks.CellMarkdown:
		errResp2 = updateMarkdownCell(req, cell)
	case notebooks.CellA2UI:
		errResp2 = h.updatePageCell(ctx, req, cell)
	}
	if errResp2 != nil {
		return errResp2
	}
	if isDryRun(ctx) {
		return dryRunOK(req.ID)
	}
	return h.commit(ctx, req, e, "update_cell")
}

func (h *Handler) updateSQLCell(ctx context.Context, req *Request, cell *notebooks.Cell) *Response {
	view, err := optionalCellView(req.Params["view"])
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	newSQL, connection := cell.Source, cell.Connection
	revet := false
	if v, ok := req.Params["sql"]; ok && v != nil {
		newSQL = stringParam(req.Params["sql"])
		if strings.TrimSpace(newSQL) == "" {
			return NewErrorResponse(req.ID, ErrInvalidParams, "sql must not be empty")
		}
		revet = newSQL != cell.Source
	}
	if v, ok := req.Params["connection"]; ok && v != nil {
		connection = strings.TrimSpace(stringParam(v))
		revet = revet || !strings.EqualFold(connection, cell.Connection)
	}
	if revet {
		if _, errResp := h.duckDB(req.ID); errResp != nil {
			return errResp
		}
		validated, vf := h.vetNotebookSQL(ctx, connection, newSQL, "notebook sql cells")
		if vf != nil {
			return NewErrorResponse(req.ID, vf.code, vf.msg)
		}
		cell.Source, cell.Connection = newSQL, validated
	}
	if v, ok := req.Params["label"]; ok && v != nil {
		cell.Label = strings.TrimSpace(stringParam(v))
	}
	if view != "" {
		cell.View, cell.ViewPinned = view, true
	}
	return nil
}

func updateMarkdownCell(req *Request, cell *notebooks.Cell) *Response {
	md := stringParam(req.Params["markdown"])
	if err := checkMarkdown(md); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	cell.Source = md
	return nil
}

func (h *Handler) updatePageCell(ctx context.Context, req *Request, cell *notebooks.Cell) *Response {
	page, err := a2ui.Validate(req.Params["page"])
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	if failures := h.vetDashboardSQL(ctx, page); len(failures) > 0 {
		return NewErrorResponse(req.ID, ErrInvalidParams, "update_cell refused: "+strings.Join(failures, "\n"))
	}
	cell.Page = &page
	return nil
}

// describeCellEdit names the cell an update_cell, delete_cell or move_cell is
// about on the approval card (its type, label and a source snippet), instead of
// an opaque id. When the notebook or cell can't be found the id stays.
func (h *Handler) describeCellEdit(ctx context.Context, req *Request, ar *ApprovalRequest) {
	verb := map[string]string{"update_cell": "Change", "delete_cell": "Delete", "move_cell": "Move"}[req.Method]
	if verb == "" {
		return
	}
	nbID := strings.TrimSpace(stringParam(req.Params["notebook_id"]))
	cellID := strings.TrimSpace(stringParam(req.Params["cell_id"]))
	nb, _, err := h.access.Notebooks().Get(ctx, nbID)
	if err != nil {
		return
	}
	i := cellIndex(nb, cellID)
	if i < 0 {
		return
	}
	c := nb.Cells[i]
	desc := string(c.Type) + " cell"
	if c.Label != "" {
		desc += fmt.Sprintf(" %q", c.Label)
	}
	if snippet := strings.Join(strings.Fields(c.Source), " "); snippet != "" {
		short, cut := truncateRunes(snippet, 80)
		if cut {
			short += "..."
		}
		desc += fmt.Sprintf(" (%s)", short)
	}
	prep := "in"
	if req.Method == "delete_cell" {
		prep = "from"
	}
	ar.Summary = fmt.Sprintf("%s %s %s notebook %q", verb, desc, prep, nb.Title)
}
