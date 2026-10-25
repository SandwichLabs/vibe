package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// maxArtifactProblems caps the findings one write_artifact reply lists; the
// rest are counted ("and N more").
const maxArtifactProblems = 20

// artifactReport collects everything wrong with one write_artifact request in
// one pass (manifest, view, script, page, SQL policy and the view's column
// references), so the model fixes them together and not one call at a time.
type artifactReport struct {
	items []string
	// policyOnly stays true while every item came from the SQL policy, which
	// is reported as a permission error as it always was.
	policyOnly bool
}

func newArtifactReport() *artifactReport { return &artifactReport{policyOnly: true} }

func (r *artifactReport) add(policy bool, text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	r.items = append(r.items, text)
	if !policy {
		r.policyOnly = false
	}
}

func (r *artifactReport) empty() bool { return len(r.items) == 0 }

// response is the error reply: one finding as it is, several as a numbered list.
func (r *artifactReport) response(id any) *Response {
	code := ErrInvalidParams
	if r.policyOnly {
		code = ErrPermission
	}
	if len(r.items) == 1 {
		return NewErrorResponse(id, code, r.items[0])
	}
	shown := r.items
	more := 0
	if len(shown) > maxArtifactProblems {
		more = len(shown) - maxArtifactProblems
		shown = shown[:maxArtifactProblems]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d problems found; nothing was written. Fix them all and send the artifact again:", len(r.items))
	for i, it := range shown {
		fmt.Fprintf(&b, "\n%d. %s", i+1, it)
	}
	if more > 0 {
		fmt.Fprintf(&b, "\n... and %d more", more)
	}
	return NewErrorResponse(id, code, b.String())
}

// artifactVetResult is what a clean vet learned: each query's columns and,
// for a page, what the page vet derived.
type artifactVetResult struct {
	cols []ArtifactQueryColumns
	page *artifacts.PageInfo
}

// artifactChecks is the write_artifact vet after the request was decoded:
// every check that can run, whatever another one found. It returns the report
// and, when the SQL policy passed, each query's result columns.
//
// Order matters only for what a later check needs: the SQL policy and the
// column checks need a manifest that validated (Bind, the Uses lists), and the
// column checks need a view that validated and queries that passed the policy.
func (h *Handler) artifactChecks(ctx context.Context, m *artifacts.Manifest, view []byte, files []artifacts.PageFile) (*artifactReport, artifactVetResult) {
	rep := newArtifactReport()
	manifestErrs := artifacts.ValidateAll(m)
	for _, err := range manifestErrs {
		rep.add(false, scrubFeedbackText(err.Error()))
	}
	viewErr := artifacts.ValidateView(m, view)
	if viewErr != nil {
		rep.add(false, scrubFeedbackText(viewErr.Error()))
	} else if m.View == artifacts.ViewScript {
		for _, p := range artifacts.CheckScriptSyntax(string(view)) {
			rep.add(false, scrubFeedbackText(p))
		}
	}
	if len(manifestErrs) > 0 {
		return rep, artifactVetResult{}
	}
	var page *artifacts.PageInfo
	if m.View == artifacts.ViewHTML {
		// Files, templates and URL attributes against H1 and H2 now, so the
		// model gets a usable error; the store repeats the vet on every write.
		var err error
		if page, err = artifacts.VetPageForWrite(m, files); err != nil {
			rep.add(false, "html_files: "+scrubFeedbackText(err.Error()))
		}
	}
	cols, err := h.VetArtifactColumns(ctx, m)
	if err != nil {
		rep.add(true, err.Error())
		return rep, artifactVetResult{page: page}
	}
	if viewErr == nil {
		for _, p := range artifacts.CheckViewShape(m, view, columnNames(cols)) {
			rep.add(false, "view: "+scrubFeedbackText(p))
		}
	}
	return rep, artifactVetResult{cols: cols, page: page}
}

// columnNames is each query's result column names, in order.
func columnNames(cols []ArtifactQueryColumns) map[string][]string {
	out := make(map[string][]string, len(cols))
	for _, q := range cols {
		names := make([]string, len(q.Columns))
		for i, c := range q.Columns {
			names[i] = c.Name
		}
		out[q.Name] = names
	}
	return out
}

// queriesForAgent is the "queries" field of a write_artifact reply: each
// query's result columns, so the model knows what a pivot, drill or chart can
// name without opening the artifact. Empty when no column could be read.
func queriesForAgent(cols []ArtifactQueryColumns) []ArtifactQueryColumns {
	if cols == nil {
		return []ArtifactQueryColumns{}
	}
	return cols
}
