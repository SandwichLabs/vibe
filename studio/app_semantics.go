package main

import (
	"errors"

	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// --- Semantic definitions: the proposals inbox (E4) ---
//
// An agent files a proposal with propose_definition; it changes nothing in
// semantics/ until the person accepts it here. These four bindings are the
// person's side of that inbox (the UI is the Semantics view). The rules live in
// internal/semantics/proposals and internal/workspace; no agent tool reaches
// them.

// Proposal and ProposalPreview are the inbox's wire types.
type (
	Proposal        = proposals.Proposal
	ProposalPreview = proposals.Preview
)

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
