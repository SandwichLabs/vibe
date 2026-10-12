package agents

import (
	"context"

	"github.com/SandwichLabs/vibe/internal/a2ui"
)

// --- A2UI handlers (#67) ---

// handlePresentData validates the "page" param against the A2UI schema and
// returns the validated page as-is. A failure surfaces as ErrInvalidParams
// with a2ui.Validate's newline-joined, JSON-path-prefixed error text — that
// text is what the model sees as the tool error, so it can correct its next
// attempt. present_data never runs SQL itself: sql-bound components
// (stat/table/barChart/lineChart with "sql") are executed later, per
// component, by the Vue renderer via RunA2UIQuery — which re-enters run_query's
// classifier — so a data-bound present_data result cannot be used to bypass
// the sandbox at validation time.
func (h *Handler) handlePresentData(_ context.Context, req *Request) *Response {
	page, err := a2ui.Validate(req.Params["page"])
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	return NewResponse(req.ID, page)
}
