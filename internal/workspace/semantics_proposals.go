package workspace

import (
	"context"
	"fmt"
	"sort"

	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
)

// The proposals inbox as a person's surfaces use it: Studio's bindings and the
// `vibe semantics` commands call these, and nothing an agent holds can. The
// rules (validation, compare-and-write against the revision proposed against)
// live in internal/semantics/proposals.

// ListProposals returns the proposals waiting for a decision, newest first; with
// all, accepted and rejected ones too. A file that cannot be read is always
// listed, with its error.
func ListProposals(ctx context.Context, ws Workspace, all bool) ([]proposals.Proposal, error) {
	list, err := ws.Proposals().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list proposals: %w", err)
	}
	out := make([]proposals.Proposal, 0, len(list))
	for _, p := range list {
		if all || p.Open() || p.Error != "" {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// GetProposal returns a proposal beside the file it would replace: the current
// text and revision, the whole proposed file, and whether the file changed
// since it was proposed.
func GetProposal(ctx context.Context, ws Workspace, id string) (*proposals.Preview, error) {
	return proposals.Show(ctx, ws.Proposals(), ws.Semantics(), id, proposals.WithQueries(ws.Queries()))
}

// AcceptProposal writes a proposal's definition into semantics/ and marks it
// accepted. shownRev is the Preview.ShownRev the person reviewed: if the target
// or the proposal changed after that, nothing is written
// (*proposals.ChangedAgainError), even with overwrite. A target that changed
// since the proposal was made is a *proposals.StaleError (errors.Is
// ErrConflict) unless overwrite, which needs shownRev.
func AcceptProposal(ctx context.Context, ws Workspace, id, shownRev string, overwrite bool) (*proposals.Preview, error) {
	return proposals.Accept(ctx, ws.Proposals(), ws.Semantics(), id, proposals.AcceptOptions{Overwrite: overwrite, ShownRev: shownRev})
}

// RejectProposal marks a proposal rejected. Nothing under semantics/ changes.
func RejectProposal(ctx context.Context, ws Workspace, id string) error {
	return proposals.Reject(ctx, ws.Proposals(), id)
}
