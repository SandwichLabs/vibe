package semantics

import (
	"context"
	"errors"

	"github.com/SandwichLabs/slaw/internal/queries"
)

// Pins let a caller say which revisions of the metric files and saved queries an
// expansion may read. A script view or page that was allowed (#282) was allowed
// over exactly those revisions; its queries run with them pinned, so a run can
// never read a different revision, whatever a cache last said: the expander
// compares each metric and saved query it reads with the pin and refuses on any
// difference.

// Kinds of a pinned file.
const (
	PinMetric = "metric"
	PinQuery  = "query"
	// PinMissing is the revision of a file that was not there.
	PinMissing = "missing"
)

type pinsKey struct{}

// PinKey is the key of a pinned file in the map WithPins takes.
func PinKey(kind, name string) string { return kind + ":" + name }

// QueryRevision is a saved query's revision as pinned: its file revision and the
// error that came with it, so a query fixed or broken counts as a change.
func QueryRevision(q *queries.Query) string { return q.Rev + "|" + q.Error }

// WithPins returns ctx carrying the pinned revisions (PinKey to revision). Once
// pins are attached they are strict, even an empty set: a metric or saved query
// that is not in the map is refused. A view that read no metric when it was
// allowed therefore runs with an empty set, and a metric() added to its
// manifest afterwards expands nothing.
func WithPins(ctx context.Context, pins map[string]string) context.Context {
	if pins == nil {
		pins = map[string]string{}
	}
	return context.WithValue(ctx, pinsKey{}, pins)
}

// checkPin refuses a read of a file whose revision is not the pinned one.
func checkPin(ctx context.Context, kind, name, rev string) error {
	pins, attached := ctx.Value(pinsKey{}).(map[string]string)
	if !attached {
		return nil
	}
	want, pinned := pins[PinKey(kind, name)]
	if pinned && want == rev {
		return nil
	}
	if !pinned {
		return refuse("%s %q was not read by this page when you allowed it; allow the page again", kind, name)
	}
	if kind == PinQuery {
		return refuse("a saved query (%s) behind a metric this page reads changed; allow the page again", name)
	}
	return refuse("a metric this page reads changed (%s); allow the page again", name)
}

func (s *reader) pinnedMetric(ctx context.Context, name string) (*Metric, error) {
	m, err := s.sem.Metric(ctx, name)
	switch {
	case errors.Is(err, ErrNotFound):
		if perr := checkPin(ctx, PinMetric, name, PinMissing); perr != nil {
			return nil, perr
		}
	case err == nil:
		if perr := checkPin(ctx, PinMetric, name, m.Rev); perr != nil {
			return nil, perr
		}
	}
	return m, err
}

func (s *reader) pinnedQuery(ctx context.Context, id string) (*queries.Query, error) {
	q, err := s.q.Get(ctx, id)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		if perr := checkPin(ctx, PinQuery, id, PinMissing); perr != nil {
			return nil, perr
		}
	case err == nil && q != nil:
		if perr := checkPin(ctx, PinQuery, id, QueryRevision(q)); perr != nil {
			return nil, perr
		}
	}
	return q, err
}
