// Package queries is the file-backed store of saved SQL queries
// (queries/*.sql) and the filter model the table browser shares with them.
package queries

import (
	"context"
	"errors"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Store is where a workspace keeps its named queries. Two implementations
// exist: the file store (queries/<id>.sql with a `-- vibe:` header) and the
// legacy container's Registry over _vibe_queries.
type Store interface {
	// List returns every query ordered by title; never nil.
	List(ctx context.Context) ([]Query, error)
	// Get returns one query; a missing id satisfies errors.Is(err, ErrNotFound).
	Get(ctx context.Context, id string) (*Query, error)
	// Create stores a new query. An empty (or uuid-shaped) q.ID is replaced by
	// the store's own id, which is written back into q.
	Create(ctx context.Context, q *Query) error
	// Update rewrites an existing query. With a file store a stale write
	// (q.Rev, WithRev, or the revision last seen for the id no longer matches
	// the file) returns ErrConflict and changes nothing.
	Update(ctx context.Context, q *Query, opts ...WriteOption) error
	// Delete removes a query; a missing id is an ErrNotFound error.
	Delete(ctx context.Context, id string) error
}

// ErrConflict means the query changed on disk since it was loaded.
var ErrConflict = filestore.ErrConflict

// ErrNotFound means there is no such query.
var ErrNotFound = filestore.ErrNotFound

// WriteOption tunes one Update.
type WriteOption func(*writeOpts)

type writeOpts struct {
	overwrite bool
	rev       string
	hasRev    bool
}

// Overwrite skips the conflict check ("overwrite" in the conflict banner).
func Overwrite() WriteOption { return func(o *writeOpts) { o.overwrite = true } }

// WithRev sets the revision the caller loaded, overriding q.Rev.
func WithRev(rev string) WriteOption {
	return func(o *writeOpts) { o.rev, o.hasRev = rev, true }
}

func applyWriteOpts(opts []WriteOption) writeOpts {
	var o writeOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

func errorsIsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
