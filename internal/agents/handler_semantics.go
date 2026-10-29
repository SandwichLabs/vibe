package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
)

// --- the semantic layer: list_definitions, get_definition, propose_definition ---
//
// Definitions are files in semantics/ that only a person writes. The handler
// holds a semantics.ReadStore (no Put or Delete in its type) and a
// proposals.Proposer (add, list, count: no accept, reject or change), so no
// tool in this file can change a definition; TestNoToolWritesSemantics runs
// every tool against a store that fails the test on any write to prove it.

// definitionKinds are the kinds the tools take.
const definitionKinds = "entity, metric, join or glossary"

func (h *Handler) handleListDefinitions(ctx context.Context, req *Request) *Response {
	sem := h.access.Semantics()
	ents, err := sem.Entities(ctx)
	if err != nil {
		return semanticsFailure(req, "list the entities", err)
	}
	mets, err := sem.Metrics(ctx)
	if err != nil {
		return semanticsFailure(req, "list the metrics", err)
	}
	joins, err := sem.Joins(ctx)
	if err != nil {
		return semanticsFailure(req, "list the joins", err)
	}
	gl, err := sem.Glossary(ctx)
	if err != nil {
		return semanticsFailure(req, "read the glossary", err)
	}
	open, err := h.access.Proposals().OpenCount(ctx)
	if err != nil {
		return semanticsFailure(req, "count the proposals", err)
	}
	glossary := map[string]any{"terms": gl.Terms}
	if gl.Rev != "" {
		glossary["rev"] = gl.Rev
	}
	if gl.Error != "" {
		glossary["error"] = scrubStoreText(gl.Error)
	}
	return NewResponse(req.ID, map[string]any{
		"entities":  entityRows(ents),
		"metrics":   metricRows(mets),
		"joins":     joinRows(joins),
		"glossary":  glossary,
		"proposals": map[string]any{"open": open},
	})
}

func semanticsFailure(req *Request, what string, err error) *Response {
	return NewErrorResponse(req.ID, ErrInternal, fmt.Sprintf("failed to %s: %s", what, scrubStoreText(semantics.ReadableError(err))))
}

// defRow starts a list row: name, rev and the error of an unreadable file.
func defRow(name string, meta semantics.Meta) map[string]any {
	row := map[string]any{"name": name}
	if meta.Rev != "" {
		row["rev"] = meta.Rev
	}
	if meta.Error != "" {
		row["error"] = scrubStoreText(meta.Error)
	}
	return row
}

func entityRows(list []semantics.Entity) []map[string]any {
	rows := make([]map[string]any, 0, len(list))
	for _, e := range list {
		row := defRow(e.Name, e.Meta)
		if e.Error == "" {
			row["grain"], row["table"], row["id_column"] = e.Grain, e.Table, e.IDColumn
			if e.Connection != "" {
				row["connection"] = e.Connection
			}
			if e.Description != "" {
				row["description"] = e.Description
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func metricRows(list []semantics.Metric) []map[string]any {
	rows := make([]map[string]any, 0, len(list))
	for _, m := range list {
		row := defRow(m.Name, m.Meta)
		if m.Error == "" {
			row["query"], row["measure"] = m.Query, m.Measure
			row["dimensions"] = nonNilStrings(m.Dimensions)
			for k, v := range map[string]string{"title": m.Title, "description": m.Description, "format": m.Format} {
				if v != "" {
					row[k] = v
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func joinRows(list []semantics.Join) []map[string]any {
	rows := make([]map[string]any, 0, len(list))
	for _, j := range list {
		row := defRow(j.Name, j.Meta)
		if j.Error == "" {
			row["left"], row["right"], row["cardinality"] = j.Left, j.Right, j.Cardinality
			if j.Measured != nil {
				row["verdict"] = j.Measured.Verdict
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// kindParam reads the kind argument of get_definition and propose_definition.
func kindParam(req *Request) (semantics.Kind, *Response) {
	k := semantics.Kind(strings.ToLower(strings.TrimSpace(stringParam(req.Params["kind"]))))
	if !proposals.ValidKind(k) {
		return "", NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("kind must be %s (got %q)", definitionKinds, truncateRunesText(stringParam(req.Params["kind"]), 40)))
	}
	return k, nil
}

func truncateRunesText(s string, n int) string {
	out, cut := truncateRunes(s, n)
	if cut {
		out += "..."
	}
	return out
}

func (h *Handler) handleGetDefinition(ctx context.Context, req *Request) *Response {
	kind, errResp := kindParam(req)
	if errResp != nil {
		return errResp
	}
	name := strings.TrimSpace(stringParam(req.Params["name"]))
	sem := h.access.Semantics()
	if kind == semantics.KindGlossary {
		return h.getGlossary(ctx, req, name)
	}
	if name == "" {
		return NewErrorResponse(req.ID, ErrInvalidParams, "name is required (see list_definitions)")
	}
	var (
		def  any
		meta semantics.Meta
		err  error
	)
	switch kind {
	case semantics.KindEntity:
		var e *semantics.Entity
		if e, err = sem.Entity(ctx, name); err == nil {
			def, meta = e, e.Meta
		}
	case semantics.KindMetric:
		var m *semantics.Metric
		if m, err = sem.Metric(ctx, name); err == nil {
			def, meta = m, m.Meta
		}
	default:
		var j *semantics.Join
		if j, err = sem.Join(ctx, name); err == nil {
			def, meta = j, j.Meta
		}
	}
	if err != nil {
		return definitionReadError(req, kind, name, err)
	}
	doc, err := definitionDoc(def)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, "failed to encode the definition")
	}
	res := map[string]any{"kind": string(kind), "name": name, "rev": meta.Rev, "definition": doc}
	if m, ok := def.(*semantics.Metric); ok {
		for k, v := range h.metricQueryInfo(ctx, m) {
			res[k] = v
		}
	}
	return NewResponse(req.ID, res)
}

// definitionDoc is a decoded definition as a plain map without the store's own
// rev and error fields (rev is reported beside it).
func definitionDoc(def any) (map[string]any, error) {
	raw, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	delete(doc, "rev")
	delete(doc, "error")
	return doc, nil
}

// metricQueryInfo reports the saved query a metric is defined over: its
// declared parameters (what a caller may set when it runs the metric) and its
// connection, or why it cannot be read.
func (h *Handler) metricQueryInfo(ctx context.Context, m *semantics.Metric) map[string]any {
	q, err := h.access.Queries().Get(ctx, m.Query)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return map[string]any{"parameters": []queries.Param{}, "query_error": fmt.Sprintf("its saved query %q does not exist", m.Query)}
	case err != nil:
		return map[string]any{"parameters": []queries.Param{}, "query_error": scrubStoreText("its saved query cannot be read: " + err.Error())}
	case q.Error != "":
		return map[string]any{"parameters": []queries.Param{}, "query_error": scrubStoreText("its saved query cannot be read: " + q.Error)}
	}
	info := map[string]any{"parameters": nonNilParams(q.Parameters)}
	if q.Connection != "" {
		info["query_connection"] = q.Connection
	}
	return info
}

func definitionReadError(req *Request, kind semantics.Kind, name string, err error) *Response {
	switch {
	case errors.Is(err, semantics.ErrNotFound):
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("there is no %s named %q (see list_definitions)", kind, truncateRunesText(name, 80)))
	case errors.Is(err, semantics.ErrInvalid):
		return NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText(fmt.Sprintf("the %s cannot be read: %s", kind, semantics.ReadableError(err))))
	}
	return NewErrorResponse(req.ID, ErrInternal, scrubStoreText(fmt.Sprintf("failed to read the %s: %s", kind, semantics.ReadableError(err))))
}

func (h *Handler) getGlossary(ctx context.Context, req *Request, term string) *Response {
	g, err := h.access.Semantics().Glossary(ctx)
	if err != nil {
		return semanticsFailure(req, "read the glossary", err)
	}
	if g.Error != "" {
		return NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText("the glossary cannot be read: "+g.Error))
	}
	res := map[string]any{"kind": string(semantics.KindGlossary), "rev": g.Rev, "terms": g.Terms}
	if term != "" {
		text, ok := semantics.GlossarySection(g.Text, term)
		if !ok {
			return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("the glossary has no term %q (terms: %s)", truncateRunesText(term, 80), strings.Join(g.Terms, ", ")))
		}
		res["name"], res["text"] = term, text
	}
	return NewResponse(req.ID, res)
}

// --- propose_definition ---------------------------------------------------------

func (h *Handler) handleProposeDefinition(ctx context.Context, req *Request) *Response {
	kind, errResp := kindParam(req)
	if errResp != nil {
		return errResp
	}
	draft, errResp := draftOf(req, kind)
	if errResp != nil {
		return errResp
	}
	p, err := proposals.Build(draft, time.Now(), proposals.ByAgent)
	if err != nil {
		return proposalRefusal(req, err)
	}
	if errResp := h.checkProposalTarget(ctx, req, p); errResp != nil {
		return errResp
	}
	if errResp := h.checkProposalReferences(ctx, req, p); errResp != nil {
		return errResp
	}
	if errResp := h.checkProposalRoom(ctx, req, p); errResp != nil {
		return errResp
	}
	if isDryRun(ctx) {
		return dryRunOK(req.ID)
	}
	if err := h.access.Proposals().Create(ctx, p); err != nil {
		if errors.Is(err, proposals.ErrTooMany) {
			return NewErrorResponse(req.ID, ErrInvalidParams, tooManyProposals(err))
		}
		h.logger.Warn("failed to store a proposal", "error", err)
		return NewErrorResponse(req.ID, ErrInternal, "failed to file the proposal: "+scrubStoreText(err.Error()))
	}
	h.logger.Info("agent proposed a definition", "id", p.ID, "kind", p.Kind, "name", p.Name)
	return NewResponse(req.ID, map[string]any{
		"proposed": true, "id": p.ID, "kind": string(p.Kind), "name": p.Name, "status": p.Status,
		"replaces": p.ReplacesRev != "",
		"note":     "Filed in the person's proposals inbox. Nothing in semantics/ has changed and it takes effect only when the person accepts it (vibe semantics proposals, or Vibe Studio). Do not use the definition until then.",
	})
}

func tooManyProposals(err error) string {
	return fmt.Sprintf("%s. Nothing was filed. Tell the person: `vibe semantics proposals` lists them.", scrubStoreText(err.Error()))
}

func draftOf(req *Request, kind semantics.Kind) (proposals.Draft, *Response) {
	d := proposals.Draft{Kind: kind}
	for _, f := range []string{"name", "rationale", "replaces_rev"} {
		if v, present := req.Params[f]; present && v != nil {
			if _, ok := v.(string); !ok {
				return d, NewErrorResponse(req.ID, ErrInvalidParams, f+" must be a string")
			}
		}
	}
	d.Name = strings.TrimSpace(stringParam(req.Params["name"]))
	d.Rationale = stringParam(req.Params["rationale"])
	d.ReplacesRev = strings.TrimSpace(stringParam(req.Params["replaces_rev"]))
	def, ok := req.Params["definition"].(map[string]any)
	if !ok {
		return d, NewErrorResponse(req.ID, ErrInvalidParams, "definition must be an object")
	}
	d.Definition = def
	if raw, present := req.Params["evidence"]; present && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return d, NewErrorResponse(req.ID, ErrInvalidParams, "evidence must be an array of strings")
		}
		for _, e := range list {
			s, ok := e.(string)
			if !ok {
				return d, NewErrorResponse(req.ID, ErrInvalidParams, "evidence must be an array of strings")
			}
			d.Evidence = append(d.Evidence, s)
		}
	}
	return d, nil
}

// proposalRefusal is a proposal's own validation failure as the model reads it:
// the reason, with the file prefix of the decoder's label left in so it can
// find the field, and nothing filed.
func proposalRefusal(req *Request, err error) *Response {
	msg := strings.TrimPrefix(err.Error(), semantics.ErrInvalid.Error()+": ")
	return NewErrorResponse(req.ID, ErrInvalidParams, scrubFeedbackText("not filed, because the definition is not valid: "+msg))
}

// checkProposalTarget holds a proposal to the file it would replace: a new
// definition must not already exist, a replacement must carry the rev the agent
// read, and a stale rev is refused now rather than at accept.
func (h *Handler) checkProposalTarget(ctx context.Context, req *Request, p *proposals.Proposal) *Response {
	fail := func(format string, args ...any) *Response {
		return NewErrorResponse(req.ID, ErrInvalidParams, "not filed: "+fmt.Sprintf(format, args...))
	}
	sem := h.access.Semantics()
	if p.Kind == semantics.KindGlossary {
		return h.checkGlossaryTarget(ctx, req, p, fail)
	}
	src, err := sem.Source(ctx, p.Kind, p.Name)
	switch {
	case errors.Is(err, semantics.ErrNotFound):
		if p.ReplacesRev != "" {
			return fail("there is no %s named %q to replace (see list_definitions). Omit replaces_rev to propose a new one.", p.Kind, p.Name)
		}
		return nil
	case err != nil:
		return NewErrorResponse(req.ID, ErrInternal, "failed to read the current definition: "+scrubStoreText(err.Error()))
	case p.ReplacesRev == "":
		return fail("a %s named %q already exists, so this would replace it. Read it with get_definition and send its rev as replaces_rev, or pick another name.", p.Kind, p.Name)
	case p.ReplacesRev != src.Rev:
		return fail("the %s %q changed since the rev you sent (a person or another tool edited it). Call get_definition again, merge your change into what it returns, and send the new rev.", p.Kind, p.Name)
	}
	return nil
}

func (h *Handler) checkGlossaryTarget(ctx context.Context, req *Request, p *proposals.Proposal, fail func(string, ...any) *Response) *Response {
	g, err := h.access.Semantics().Glossary(ctx)
	if err != nil {
		return semanticsFailure(req, "read the glossary", err)
	}
	if g.Error != "" {
		return fail("the glossary cannot be read (%s), so a term cannot be added until the person fixes it", scrubStoreText(g.Error))
	}
	if semantics.GlossaryOpenFence(g.Text) {
		return fail("glossary.md has an unclosed ``` fence; fix it first (the person has to repair the file before a term can be added)")
	}
	term, _, err := p.GlossaryTerm()
	if err != nil {
		return proposalRefusal(req, err)
	}
	if _, exists := semantics.GlossarySection(g.Text, term); exists {
		switch {
		case p.ReplacesRev == "":
			return fail("the glossary already defines %q, so this would replace it. Read it with get_definition (kind glossary, name %q) and send the glossary's rev as replaces_rev.", term, term)
		case p.ReplacesRev != g.Rev:
			return fail("the glossary changed since the rev you sent. Call get_definition again and send the new rev.")
		}
		return nil
	}
	// A new term replaces nothing, whatever rev was sent: it goes stale only if
	// the term appears before the person accepts.
	p.ReplacesRev = ""
	return nil
}

// checkProposalReferences refuses a metric over a saved query that does not
// exist: the proposal would be valid as a file and useless as a metric.
func (h *Handler) checkProposalReferences(ctx context.Context, req *Request, p *proposals.Proposal) *Response {
	if p.Kind != semantics.KindMetric {
		return nil
	}
	data, err := p.Render()
	if err != nil {
		return proposalRefusal(req, err)
	}
	m, err := semantics.DecodeMetric(p.Name, []byte(data))
	if err != nil {
		return proposalRefusal(req, err)
	}
	q, err := h.access.Queries().Get(ctx, m.Query)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("not filed: the saved query %q does not exist (see list_queries). Save it first with save_query, then propose the metric.", m.Query))
	case err != nil:
		return NewErrorResponse(req.ID, ErrInternal, "failed to read the saved query: "+scrubStoreText(err.Error()))
	case q.Error != "":
		return NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText("not filed: the saved query cannot be read: "+q.Error))
	}
	return nil
}

// checkProposalRoom refuses a proposal that duplicates one already waiting, and
// one that would pass the cap on open proposals (a dry run reports it too, so
// the person is never shown a card for a call that will be refused).
func (h *Handler) checkProposalRoom(ctx context.Context, req *Request, p *proposals.Proposal) *Response {
	list, err := h.access.Proposals().List(ctx)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, "failed to read the proposals: "+scrubStoreText(err.Error()))
	}
	want, _ := p.Render()
	open := 0
	for i := range list {
		o := &list[i]
		if !o.Open() {
			continue
		}
		open++
		if o.Kind != p.Kind || o.Name != p.Name {
			continue
		}
		if got, err := o.Render(); err == nil && got == want && o.ReplacesRev == p.ReplacesRev {
			return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("not filed: this exact %s %q is already waiting for the person (proposal %s).", p.Kind, p.Name, o.ID))
		}
	}
	if open >= proposals.MaxOpen {
		return NewErrorResponse(req.ID, ErrInvalidParams, tooManyProposals(fmt.Errorf("%w (%d are waiting)", proposals.ErrTooMany, open)))
	}
	return nil
}

// proposeSummary is the approval card's line for propose_definition. It says
// what the call does and, as plainly, what it does not: the proposal waits in
// the person's inbox, and nothing in semantics/ changes until they accept it.
// It says "a change to" only when the definition already exists.
func (h *Handler) proposeSummary(ctx context.Context, req *Request) string {
	kind := strings.TrimSpace(stringParam(req.Params["kind"]))
	name := strings.TrimSpace(stringParam(req.Params["name"]))
	exists := false
	sem := h.access.Semantics()
	if def, ok := req.Params["definition"].(map[string]any); ok && kind == "glossary" {
		name = stringParam(def["term"])
		if g, err := sem.Glossary(ctx); err == nil && g.Error == "" {
			exists = semantics.HasGlossaryTerm(g.Text, name)
		}
	} else if proposals.ValidKind(semantics.Kind(kind)) && name != "" {
		_, err := sem.Source(ctx, semantics.Kind(kind), name)
		exists = err == nil
	}
	verb := "Propose"
	if exists {
		verb = "Propose a change to"
	}
	return fmt.Sprintf("%s %s %q (goes to your proposals inbox; nothing in semantics/ changes until you accept it)", verb, kind, name)
}
