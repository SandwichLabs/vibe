package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/assay/internal/queries"
	"github.com/SandwichLabs/assay/internal/semantics"
)

// What the Semantics view lists and opens. Both are reads of semantics/ files
// (an unreadable file is listed with its error, never hidden) and neither is an
// agent tool: the view reaches them through Studio's ListDefinitions and
// GetDefinition bindings.

// DefinitionSummary is one line of the Semantics list: a definition's name and
// the one sentence that tells it apart, the revision of its file, and why the
// file could not be read when it could not.
type DefinitionSummary struct {
	Kind semantics.Kind `json:"kind"`
	Name string         `json:"name"`
	// Title is a metric's title; Summary is the first thing worth showing: a
	// description, or for an entity its grain, or for a join "left to right".
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
	Rev     string `json:"rev,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DefinitionList is everything under semantics/ in one read, plus how many
// proposals wait, so the view's badge and list come from one call.
type DefinitionList struct {
	Entities []DefinitionSummary `json:"entities"`
	Metrics  []DefinitionSummary `json:"metrics"`
	Joins    []DefinitionSummary `json:"joins"`
	// Terms are the glossary's "## " headings; GlossaryRev is "" when there is
	// no glossary.md, GlossaryError says why one could not be read.
	Terms         []string `json:"terms"`
	GlossaryRev   string   `json:"glossary_rev,omitempty"`
	GlossaryError string   `json:"glossary_error,omitempty"`
	OpenProposals int      `json:"open_proposals"`
}

// ListDefinitions lists the entities, metrics, joins and glossary terms of the
// workspace and counts the open proposals.
func ListDefinitions(ctx context.Context, ws Workspace) (*DefinitionList, error) {
	sm := ws.Semantics()
	out := &DefinitionList{Entities: []DefinitionSummary{}, Metrics: []DefinitionSummary{}, Joins: []DefinitionSummary{}, Terms: []string{}}
	ents, err := sm.Entities(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}
	for _, e := range ents {
		s := DefinitionSummary{Kind: semantics.KindEntity, Name: e.Name, Summary: firstNonEmpty(e.Grain, e.Description), Rev: e.Rev, Error: e.Error}
		out.Entities = append(out.Entities, s)
	}
	ms, err := sm.Metrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}
	for _, m := range ms {
		out.Metrics = append(out.Metrics, DefinitionSummary{Kind: semantics.KindMetric, Name: m.Name, Title: m.Title, Summary: m.Description, Rev: m.Rev, Error: m.Error})
	}
	js, err := sm.Joins(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list joins: %w", err)
	}
	for _, j := range js {
		s := DefinitionSummary{Kind: semantics.KindJoin, Name: j.Name, Rev: j.Rev, Error: j.Error}
		if j.Error == "" {
			s.Summary = fmt.Sprintf("%s to %s (%s)", j.Left, j.Right, j.Cardinality)
		}
		out.Joins = append(out.Joins, s)
	}
	g, err := sm.Glossary(ctx)
	if err != nil {
		return nil, err
	}
	out.Terms, out.GlossaryRev, out.GlossaryError = append(out.Terms, g.Terms...), g.Rev, g.Error
	if n, err := ws.Proposals().OpenCount(ctx); err == nil {
		out.OpenProposals = n
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// DefinitionDetail is one definition opened: its file text as stored (valid or
// not, so a broken file can be read), its revision, and the decoded document
// of its kind when the file is valid.
type DefinitionDetail struct {
	Kind semantics.Kind `json:"kind"`
	Name string         `json:"name"`
	Text string         `json:"text"`
	Rev  string         `json:"rev"`
	// Error says why the file could not be decoded; the document fields are
	// then nil.
	Error    string              `json:"error,omitempty"`
	Entity   *semantics.Entity   `json:"entity,omitempty"`
	Metric   *semantics.Metric   `json:"metric,omitempty"`
	Join     *semantics.Join     `json:"join,omitempty"`
	Glossary *semantics.Glossary `json:"glossary,omitempty"`
	// QueryMissing is true for a metric whose saved query is not in queries/.
	QueryMissing bool `json:"query_missing,omitempty"`
}

// GetDefinition opens one definition. A missing one satisfies
// errors.Is(err, semantics.ErrNotFound).
func GetDefinition(ctx context.Context, ws Workspace, kind semantics.Kind, name string) (*DefinitionDetail, error) {
	sm := ws.Semantics()
	src, err := sm.Source(ctx, kind, name)
	if err != nil {
		if kind == semantics.KindGlossary && errors.Is(err, semantics.ErrNotFound) {
			return &DefinitionDetail{Kind: kind, Name: name, Glossary: &semantics.Glossary{Terms: []string{}}}, nil
		}
		return nil, err
	}
	d := &DefinitionDetail{Kind: kind, Name: src.Name, Text: src.Text, Rev: src.Rev}
	var derr error
	switch kind {
	case semantics.KindEntity:
		d.Entity, derr = sm.Entity(ctx, name)
	case semantics.KindMetric:
		d.Metric, derr = sm.Metric(ctx, name)
	case semantics.KindJoin:
		d.Join, derr = sm.Join(ctx, name)
	case semantics.KindGlossary:
		d.Glossary, derr = sm.Glossary(ctx)
		if derr == nil && d.Glossary.Error != "" {
			d.Error, d.Glossary = d.Glossary.Error, nil
		}
	}
	if derr != nil {
		d.Error = semantics.ReadableError(derr)
		return d, nil
	}
	if d.Metric != nil {
		if _, qerr := ws.Queries().Get(ctx, d.Metric.Query); errors.Is(qerr, queries.ErrNotFound) {
			d.QueryMissing = true
		}
	}
	return d, nil
}
