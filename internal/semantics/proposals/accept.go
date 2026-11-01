package proposals

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/SandwichLabs/slaw/internal/filestore"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
)

// StaleError says the definition a proposal would replace is not the one it
// was made against: edited, created or deleted since. It satisfies
// errors.Is(err, ErrConflict). A person reviews the diff and accepts anyway
// (AcceptOptions.Overwrite) or rejects.
type StaleError struct {
	Target string
	Reason string
}

func (e *StaleError) Error() string {
	return StaleSentence(e.Target, e.Reason) + "; review the difference and accept it anyway (overwrite) or reject it"
}

// StaleSentence says in words that a target changed since it was proposed.
func StaleSentence(target, reason string) string {
	if strings.HasPrefix(reason, "the ") {
		return fmt.Sprintf("%s: %s since this was proposed", target, reason)
	}
	return fmt.Sprintf("%s %s since this was proposed", target, reason)
}

// Is implements errors.Is.
func (e *StaleError) Is(target error) bool { return target == ErrConflict }

// ChangedAgainError says the person is accepting a different version than the
// one they were shown: the target or the proposal changed after they looked.
// It satisfies errors.Is(err, ErrConflict) and is raised even with Overwrite.
type ChangedAgainError struct{ Target string }

func (e *ChangedAgainError) Error() string {
	return fmt.Sprintf("%s or its proposal changed again since you looked; review it again before accepting", e.Target)
}

// Is implements errors.Is.
func (e *ChangedAgainError) Is(target error) bool { return target == ErrConflict }

// ErrUnclosedFence is why a glossary term cannot be added or replaced.
var ErrUnclosedFence = fmt.Errorf("%w: glossary.md has an unclosed ``` fence; fix it first", ErrInvalid)

// Preview is a proposal set beside the file it would replace: what a person
// reviews before deciding.
type Preview struct {
	Proposal *Proposal `json:"proposal"`
	// Target is the workspace-relative file accepting writes.
	Target string `json:"target"`
	// Exists says whether Target exists now; CurrentText and CurrentRev are
	// its text and revision ("" when it does not).
	Exists      bool   `json:"exists"`
	CurrentText string `json:"current_text"`
	CurrentRev  string `json:"current_rev"`
	// ProposedText is the whole file as it would be after accepting: the
	// proposed document, or the glossary with the term set.
	ProposedText string `json:"proposed_text"`
	// Stale is true when the file is not the one the proposal was made
	// against; StaleReason says how.
	Stale       bool   `json:"stale"`
	StaleReason string `json:"stale_reason,omitempty"`
	// ShownRev pins what the person is looking at: a token over the target's
	// current revision and the proposal file's. Accept with it refuses if
	// either changed since ("changed again since you looked"), and accepting
	// over a stale target (Overwrite) requires it.
	ShownRev string `json:"shown_rev"`
	// Warnings are things to weigh before accepting: a metric whose saved query
	// is missing, a measured join profile that would be dropped, comments that
	// would be lost.
	Warnings []string `json:"warnings"`
}

// AcceptOptions tune Accept.
type AcceptOptions struct {
	// Overwrite accepts a proposal whose target changed since it was made.
	Overwrite bool
	// ShownRev is Preview.ShownRev from the Show the person reviewed. It is
	// required for every accept: a proposal edited after it was shown, or a
	// target that moved, is refused (ChangedAgainError), so what is written is
	// what was displayed.
	ShownRev string
}

// QueryLookup reads saved queries, for the missing-saved-query warning.
type QueryLookup interface {
	Get(ctx context.Context, id string) (*queries.Query, error)
}

// ShowOption tunes Show.
type ShowOption func(*showCfg)

type showCfg struct{ queries QueryLookup }

// WithQueries lets Show warn about a metric whose saved query is gone.
func WithQueries(q QueryLookup) ShowOption { return func(c *showCfg) { c.queries = q } }

// Show loads a proposal and the file it would replace.
func Show(ctx context.Context, props Store, sem semantics.ReadStore, id string, opts ...ShowOption) (*Preview, error) {
	var cfg showCfg
	for _, o := range opts {
		o(&cfg)
	}
	p, err := props.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	pv := &Preview{Proposal: p, Target: p.Target(), Warnings: []string{}}
	src, err := sem.Source(ctx, p.Kind, p.Name)
	switch {
	case err == nil:
		pv.Exists, pv.CurrentText, pv.CurrentRev = true, src.Text, src.Rev
	case !errors.Is(err, semantics.ErrNotFound):
		return nil, fmt.Errorf("failed to read %s: %w", pv.Target, err)
	}
	if p.Kind == semantics.KindGlossary && semantics.GlossaryOpenFence(pv.CurrentText) {
		return nil, ErrUnclosedFence
	}
	if err := pv.propose(); err != nil {
		return nil, err
	}
	pv.markStale()
	pv.ShownRev = shownToken(pv.CurrentRev, p.Rev)
	pv.warn(ctx, cfg)
	return pv, nil
}

func shownToken(targetRev, proposalRev string) string {
	h := filestore.HashBytes([]byte(targetRev + "\x00" + proposalRev))
	if len(h) > 16 {
		h = h[:16]
	}
	return h
}

// markStale compares the target with the revision the proposal was made
// against. A glossary term is judged by the term, not the file: a new term
// (no replaces_rev) is stale only if the term now exists, a replacement only if
// the term changed or is gone; another term changing does not matter, since
// accepting edits the current text.
func (pv *Preview) markStale() {
	p := pv.Proposal
	if p.Kind == semantics.KindGlossary {
		term, _, err := p.GlossaryTerm()
		if err != nil {
			return
		}
		has := pv.Exists && semantics.HasGlossaryTerm(pv.CurrentText, term)
		switch {
		case has && p.ReplacesRev == "":
			pv.Stale, pv.StaleReason = true, "was created"
		case has && pv.CurrentRev != p.ReplacesRev:
			pv.Stale, pv.StaleReason = true, "changed"
		case !has && p.ReplacesRev != "":
			pv.Stale, pv.StaleReason = true, "the term was deleted"
		}
		return
	}
	switch {
	case pv.Exists && p.ReplacesRev == "":
		pv.Stale, pv.StaleReason = true, "was created"
	case pv.Exists && pv.CurrentRev != p.ReplacesRev:
		pv.Stale, pv.StaleReason = true, "changed"
	case !pv.Exists && p.ReplacesRev != "":
		pv.Stale, pv.StaleReason = true, "was deleted"
	}
}

func (pv *Preview) warn(ctx context.Context, cfg showCfg) {
	p := pv.Proposal
	if p.Kind == semantics.KindMetric && cfg.queries != nil {
		if m, err := semantics.DecodeMetric(p.Name, []byte(pv.ProposedText)); err == nil {
			if _, err := cfg.queries.Get(ctx, m.Query); errors.Is(err, queries.ErrNotFound) {
				pv.Warnings = append(pv.Warnings, fmt.Sprintf("its saved query %q no longer exists (queries/%s.sql): the metric will not run until it is saved again", m.Query, m.Query))
			}
		}
	}
	if !pv.Exists || p.Kind == semantics.KindGlossary {
		return
	}
	if p.Kind == semantics.KindJoin {
		if j, err := semantics.DecodeJoin(p.Name, []byte(pv.CurrentText)); err == nil && j.Measured != nil {
			pv.Warnings = append(pv.Warnings, "accepting drops the join's measured block (its profiling result); profile it again afterwards")
		}
	}
	if hasYAMLComments(pv.CurrentText) {
		pv.Warnings = append(pv.Warnings, "accepting replaces a hand-written file with comments; the comments will be lost")
	}
}

// hasYAMLComments reports a comment anywhere in a YAML document.
func hasYAMLComments(text string) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return strings.Contains(text, "#")
	}
	var walk func(n *yaml.Node) bool
	walk = func(n *yaml.Node) bool {
		if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
			return true
		}
		for _, c := range n.Content {
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(&doc)
}

// propose fills ProposedText.
func (pv *Preview) propose() error {
	text, err := pv.Proposal.Render()
	if err != nil {
		return err
	}
	if pv.Proposal.Kind != semantics.KindGlossary {
		pv.ProposedText = text
		return nil
	}
	term, body, err := pv.Proposal.GlossaryTerm()
	if err != nil {
		return err
	}
	pv.ProposedText, _ = semantics.SetGlossaryTerm(pv.CurrentText, term, body)
	if _, err := semantics.DecodeGlossary([]byte(pv.ProposedText)); err != nil {
		return err
	}
	return nil
}

// Accept writes an open proposal's definition into semantics/ and marks the
// proposal accepted. The write is compare-and-write against the revision the
// proposal was made against: if the file changed since, nothing is written and
// the error is a *StaleError, unless opts.Overwrite. opts.ShownRev is
// mandatory: an empty one is ErrInvalid, and one that no longer matches (the
// target or the proposal changed after it was shown) is a *ChangedAgainError,
// even with Overwrite.
// The bytes written are exactly Preview.ProposedText of that same Show, which
// is the text the person reviewed. A target that already reads as proposed
// (a crash after the write) is not written again, only marked accepted. Only
// a person's surface calls this.
func Accept(ctx context.Context, props Store, sem semantics.Store, id string, opts AcceptOptions, show ...ShowOption) (*Preview, error) {
	pv, err := Show(ctx, props, sem, id, show...)
	if err != nil {
		return nil, err
	}
	p := pv.Proposal
	if p.Status != StatusOpen {
		return nil, fmt.Errorf("proposal %s is already %s: %w", id, p.Status, ErrNotOpen)
	}
	if opts.ShownRev == "" {
		return pv, &plainInvalid{"accepting needs the rev of the version you looked at (shown_rev from Show)"}
	}
	if opts.ShownRev != pv.ShownRev {
		return pv, &ChangedAgainError{Target: pv.Target}
	}
	if pv.Exists && pv.CurrentText == pv.ProposedText {
		return pv, markAccepted(ctx, props, pv, false)
	}
	if pv.Stale && !opts.Overwrite {
		return pv, &StaleError{Target: pv.Target, Reason: pv.StaleReason}
	}
	if err := writeAccepted(ctx, sem, pv, opts); err != nil {
		return pv, err
	}
	return pv, markAccepted(ctx, props, pv, true)
}

func markAccepted(ctx context.Context, props Store, pv *Preview, wrote bool) error {
	if err := props.Decide(ctx, pv.Proposal.ID, StatusAccepted, pv.Proposal.Rev); err != nil {
		if wrote {
			return fmt.Errorf("%s was written, but proposal %s could not be marked accepted: %w", pv.Target, pv.Proposal.ID, err)
		}
		return fmt.Errorf("proposal %s could not be marked accepted: %w", pv.Proposal.ID, err)
	}
	return nil
}

func writeAccepted(ctx context.Context, sem semantics.Store, pv *Preview, opts AcceptOptions) error {
	p := pv.Proposal
	if p.Kind == semantics.KindGlossary {
		// Read-modify-write on the revision just read: accepting never drops
		// another term, with or without Overwrite.
		g := &semantics.Glossary{Text: pv.ProposedText, Meta: semantics.Meta{Rev: pv.CurrentRev}}
		if err := sem.PutGlossary(ctx, g, semantics.WithRev(pv.CurrentRev)); err != nil {
			return fmt.Errorf("failed to accept proposal %s: %w", p.ID, err)
		}
		return nil
	}
	wopts := []semantics.WriteOption{semantics.WithRev(pv.CurrentRev)}
	if opts.Overwrite {
		wopts = []semantics.WriteOption{semantics.WithOverwrite()}
	}
	if _, err := sem.PutSource(ctx, p.Kind, p.Name, []byte(pv.ProposedText), wopts...); err != nil {
		return fmt.Errorf("failed to accept proposal %s: %w", p.ID, err)
	}
	return nil
}

// Reject marks an open proposal rejected. Nothing under semantics/ changes.
func Reject(ctx context.Context, props Store, id string) error {
	return props.Decide(ctx, id, StatusRejected, "")
}

// plainInvalid is a refusal that reads as a sentence, without the prefix of a
// stored definition's error; errors.Is(err, ErrInvalid) still holds.
type plainInvalid struct{ msg string }

func (e *plainInvalid) Error() string        { return e.msg }
func (e *plainInvalid) Is(target error) bool { return target == ErrInvalid }
