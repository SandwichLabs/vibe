package main

import (
	"context"
	"errors"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/semantics/assert"
	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// --- Semantic definitions: the proposals inbox (E4) ---
//
// An agent files a proposal with propose_definition; it changes nothing in
// semantics/ until the person accepts it here. These four bindings are the
// person's side of that inbox (the UI is the Semantics view). The rules live in
// internal/semantics/proposals and internal/workspace; no agent tool reaches
// them.

// Proposal and ProposalPreview are the inbox's wire types; DefinitionList and
// DefinitionDetail are the Semantics view's list and opened definition.
type (
	Proposal         = proposals.Proposal
	ProposalPreview  = proposals.Preview
	DefinitionList   = workspace.DefinitionList
	DefinitionDetail = workspace.DefinitionDetail
)

// ListDefinitions returns the workspace's entities, metrics, joins and
// glossary terms (a file that cannot be read is listed with its error) and how
// many proposals wait.
func (a *App) ListDefinitions() (*DefinitionList, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return workspace.ListDefinitions(a.ctxOrBackground(), a.ws)
}

// GetDefinition opens one definition (kind entity, metric, join or glossary):
// its file text as stored, its revision and the decoded document.
func (a *App) GetDefinition(kind, name string) (*DefinitionDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return workspace.GetDefinition(a.ctxOrBackground(), a.ws, semantics.Kind(kind), name)
}

// proposalErr marks a proposal whose target changed since it was proposed as a
// conflict the UI can offer "accept anyway" for, keeping the reason.
func proposalErr(err error) error {
	if err != nil && errors.Is(err, proposals.ErrConflict) {
		var stale *proposals.StaleError
		var again *proposals.ChangedAgainError
		switch {
		case errors.As(err, &stale):
			return newConflict(stale.Error())
		case errors.As(err, &again):
			return newConflict(again.Error())
		}
	}
	return conflictErr(err)
}

// ListProposals returns the proposed definitions waiting for a decision,
// newest first; with all, the accepted and rejected ones too.
func (a *App) ListProposals(all bool) ([]Proposal, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return workspace.ListProposals(a.ctxOrBackground(), a.ws, all)
}

// GetProposal returns one proposal with the current text of the file it would
// replace and the whole proposed text (the diff is drawn client-side), and
// whether the file changed since it was proposed.
func (a *App) GetProposal(id string) (*ProposalPreview, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return workspace.GetProposal(a.ctxOrBackground(), a.ws, id)
}

// AcceptProposal writes the proposal's definition into semantics/ and marks it
// accepted. shownRev is the shown_rev GetProposal returned for the version the
// person reviewed: if the file or the proposal changed after that, nothing is
// written, even with overwrite. If the file changed since the proposal was made
// it is refused as a conflict unless overwrite (which needs shownRev).
func (a *App) AcceptProposal(id, shownRev string, overwrite bool) error {
	defer a.recheckGrants() // registered first, so it runs after the lock is released
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if _, err := workspace.AcceptProposal(a.ctxOrBackground(), a.ws, id, shownRev, overwrite); err != nil {
		return proposalErr(err)
	}
	a.emitEvent("data:changed")
	return nil
}

// RejectProposal marks the proposal rejected; semantics/ is not changed.
func (a *App) RejectProposal(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := workspace.RejectProposal(a.ctxOrBackground(), a.ws, id); err != nil {
		return err
	}
	a.emitEvent("data:changed")
	return nil
}

// --- Semantic definitions: running what they declare (E4 P5) ---
//
// The person's actions behind the Semantics view's "Run assertions", "Profile"
// and "Try it". Every statement goes through the agent policy's read-only path
// in the tightest tier (a definition may come from a cloned repository); none
// of it is an agent tool or an MCP method. The rules live in
// internal/workspace/semantics_run.go and internal/semantics.

const (
	// tryItTimeout bounds one "Try it" run, like a live A2UI binding.
	tryItTimeout = a2uiQueryTimeout
	// tryItRowCap is the most rows "Try it" returns, as many as the query grid.
	tryItRowCap = queryGridRowCap
)

// AssertionRow and JoinProfile are the wire types of those actions.
type (
	AssertionRow = assert.Row
	JoinProfile  = semantics.Profile
)

// definitionRunner is the read-only handler a definition run uses, and the
// workspace and session it was built for. It takes the lifecycle lock only to
// read them; the handler then takes a.mu.RLock for each statement it runs (so
// a workspace close waits for the statement in flight, not for the whole run).
// There is no cancel from the UI yet (the Semantics view adds one); the
// per-statement and Try it deadlines bound a run until then.
func (a *App) definitionRunner() (*agentToolHandler, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return a.newAgentToolHandler(agents.ModeReadOnly), nil
}

// RunAssertions runs the declared assertions of one entity or metric (kind
// "entity" or "metric" with a name), of every one of a kind (no name) or of
// everything (neither). A check that cannot run is an "error" row and the
// others still run.
func (a *App) RunAssertions(kind, name string) ([]AssertionRow, error) {
	h, err := a.definitionRunner()
	if err != nil {
		return nil, err
	}
	before := session.StatusSnapshot(h.sess)
	results, err := workspace.RunAssertions(a.ctxOrBackground(), h.ws, kind, name, workspace.ReadOnlyRunFunc(h), time.Now())
	a.afterReadOnlyRun(h.sess, before)
	if err != nil {
		return nil, err
	}
	return assert.Rows(results), nil
}

// ProfileJoin measures a join against its declared cardinality and records the
// result in the join's file (measured:). A file that changed while it ran is
// reported as a conflict and nothing is written.
func (a *App) ProfileJoin(name string) (*JoinProfile, error) {
	h, err := a.definitionRunner()
	if err != nil {
		return nil, err
	}
	before := session.StatusSnapshot(h.sess)
	p, err := workspace.ProfileJoin(a.ctxOrBackground(), h.ws, name, workspace.ReadOnlyRunFunc(h), time.Now())
	a.afterReadOnlyRun(h.sess, before)
	if err != nil {
		return nil, conflictErr(err)
	}
	a.emitEvent("data:changed")
	a.recheckGrants()
	return p, nil
}

// RunMetric is a metric's "Try it": the metric broken down by dims (a subset
// of its dimensions; none gives the single total), run through the read-only
// policy and capped like the query grid.
func (a *App) RunMetric(name string, dims []string) (*QueryResult, error) {
	h, err := a.definitionRunner()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), tryItTimeout)
	defer cancel()
	sqlStr, connection, err := workspace.MetricQuery(ctx, h.ws, name, dims)
	if err != nil {
		return nil, err
	}
	before := session.StatusSnapshot(h.sess)
	start := time.Now()
	rs, truncated, err := h.RunReadOnlySQLIn(ctx, connection, sqlStr, nil, tryItRowCap)
	a.afterReadOnlyRun(h.sess, before)
	if err != nil {
		return nil, a.explainReadOnlyDenial(err)
	}
	return tryItResult(rs, truncated, time.Since(start))
}

// tryItResult shapes a capped result for the grid. The read-only path stops at
// the cap without counting the rest, so a truncated result sets Truncated and
// leaves TotalRows equal to RowCount (the true total is unknown), instead of
// inventing a count.
func tryItResult(rs *engine.ResultSet, truncated bool, took time.Duration) (*QueryResult, error) {
	out := &QueryResult{Duration: humanDuration(took), RowCount: rs.RowCount(), TotalRows: rs.RowCount(), Rows: rs.Rows, Columns: columnInfos(rs), Truncated: truncated}
	if err := jsonRoundTripGuard(out); err != nil {
		return nil, err
	}
	return out, nil
}
