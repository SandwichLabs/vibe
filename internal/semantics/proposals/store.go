package proposals

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/slaw/internal/semantics"
)

// Proposer is the whole of the inbox an agent surface holds: it can add a
// proposal and see how many wait. It cannot read one back, accept, reject or
// change one, so no tool built on it can decide anything.
type Proposer interface {
	// Create stores a new open proposal (its id and fields come from Build). It
	// refuses with ErrTooMany when MaxOpen proposals already wait.
	Create(ctx context.Context, p *Proposal) error
	// List returns every proposal, oldest first; never nil. A file that cannot
	// be read is listed with Error set.
	List(ctx context.Context) ([]Proposal, error)
	// OpenCount is how many proposals wait for a decision.
	OpenCount(ctx context.Context) (int, error)
}

// Store is the person's side of the inbox.
type Store interface {
	Proposer
	// Get returns one proposal. id must have the shape Slaw generates
	// (ValidID), so it cannot name a path; an unknown one is ErrNotFound and
	// an unreadable file is an error wrapping ErrInvalid.
	Get(ctx context.Context, id string) (*Proposal, error)
	// Decide moves an open proposal to accepted or rejected, comparing the
	// file it read with the file it writes. rev is the proposal file's
	// revision the caller looked at (Proposal.Rev): if the file differs now it
	// is ErrConflict and nothing changes; "" skips that check. A proposal that
	// is not open is ErrNotOpen.
	Decide(ctx context.Context, id, status, rev string) error
}

// backend is where a core keeps proposal files: the workspace folder or a map.
type backend interface {
	read(id string) (data []byte, rev string, err error)
	write(id string, data []byte, expect string) error
	remove(id string) error
	ids() ([]string, error)
}

type core struct {
	b      backend
	logger *slog.Logger
	now    func() time.Time
}

var _ Store = (*core)(nil)

func (c *core) load(id string) (*Proposal, error) {
	data, rev, err := c.b.read(id)
	if err != nil {
		return nil, err
	}
	p, err := Decode(id, data)
	if err != nil {
		return nil, err
	}
	p.Rev = rev
	return p, nil
}

// List implements Proposer.
func (c *core) List(ctx context.Context) ([]Proposal, error) {
	ids, err := c.b.ids()
	if err != nil {
		return nil, fmt.Errorf("failed to list proposals: %w", err)
	}
	sort.Strings(ids)
	out := make([]Proposal, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !ValidID(id) {
			out = append(out, Proposal{ID: id, Meta: semantics.Meta{Error: "the file name is not a proposal id"}})
			continue
		}
		p, err := c.load(id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			c.logger.Warn("unreadable proposal", "id", id, "error", err)
			out = append(out, Proposal{ID: id, Meta: semantics.Meta{Error: strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")}})
			continue
		}
		out = append(out, *p)
	}
	return out, nil
}

// OpenCount implements Proposer.
func (c *core) OpenCount(ctx context.Context) (int, error) {
	list, err := c.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range list {
		if list[i].Open() {
			n++
		}
	}
	return n, nil
}

// Create implements Proposer.
func (c *core) Create(ctx context.Context, p *Proposal) error {
	if p.Status != StatusOpen {
		return invalid("only an open proposal can be created")
	}
	data, err := Encode(p)
	if err != nil {
		return err
	}
	list, err := c.List(ctx)
	if err != nil {
		return err
	}
	open := 0
	for i := range list {
		if list[i].Open() {
			open++
		}
	}
	if open >= MaxOpen {
		return fmt.Errorf("%w (%d are waiting)", ErrTooMany, open)
	}
	if len(list) >= MaxFiles {
		c.prune(list)
	}
	if err := c.b.write(p.ID, data, ""); err != nil {
		return fmt.Errorf("failed to store proposal %s: %w", p.ID, err)
	}
	return nil
}

// prune removes the oldest decided proposals (history only) down to PruneTo
// files, so the folder cannot grow without bound. Open and unreadable files
// are never removed.
func (c *core) prune(list []Proposal) {
	excess := len(list) - PruneTo
	for i := range list {
		if excess <= 0 {
			return
		}
		p := &list[i]
		if p.Error != "" || p.Open() {
			continue
		}
		if err := c.b.remove(p.ID); err != nil {
			c.logger.Warn("failed to prune a decided proposal", "id", p.ID, "error", err)
			continue
		}
		excess--
	}
}

// Get implements Store.
func (c *core) Get(_ context.Context, id string) (*Proposal, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("failed to get proposal: %w", invalid("%q is not a proposal id", truncate(id, 80)))
	}
	p, err := c.load(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("proposal %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get proposal %s: %w", id, err)
	}
	return p, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// Decide implements Store.
func (c *core) Decide(ctx context.Context, id, status, rev string) error {
	if status != StatusAccepted && status != StatusRejected {
		return invalid("a proposal is decided as accepted or rejected, not %q", status)
	}
	p, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	if rev != "" && p.Rev != rev {
		return fmt.Errorf("proposal %s changed since you looked: %w", id, ErrConflict)
	}
	if p.Status != StatusOpen {
		return fmt.Errorf("proposal %s is already %s: %w", id, p.Status, ErrNotOpen)
	}
	p.Status, p.DecidedAt = status, c.now().UTC().Format(time.RFC3339)
	data, err := Encode(p)
	if err != nil {
		return err
	}
	if err := c.b.write(id, data, p.Rev); err != nil {
		return fmt.Errorf("failed to update proposal %s: %w", id, err)
	}
	return nil
}
