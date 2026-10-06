package session

import (
	"context"
	"database/sql"
)

// Rows is a cancellable result set with an optional row cap. It embeds
// *sql.Rows; Next stops once the cap is reached and Truncated reports it.
// Close releases the statement timeout and any pinned connection.
type Rows struct {
	*sql.Rows
	cap       int
	n         int
	truncated bool
	cancel    context.CancelFunc
	release   func() error
	closed    bool
}

// Next advances, honouring the row cap.
func (r *Rows) Next() bool {
	if r.cap > 0 && r.n >= r.cap {
		// One more Next tells us whether rows were actually dropped.
		if r.Rows.Next() {
			r.truncated = true
		}
		return false
	}
	if !r.Rows.Next() {
		return false
	}
	r.n++
	return true
}

// Truncated reports whether the row cap dropped rows.
func (r *Rows) Truncated() bool { return r.truncated }

// Close closes the rows, cancels the timeout context and releases the pinned
// connection, if any. Safe to call twice.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.Rows.Close()
	if r.cancel != nil {
		r.cancel()
	}
	if r.release != nil {
		if rerr := r.release(); err == nil {
			err = rerr
		}
	}
	return err
}

func withTimeout(ctx context.Context, cc callConfig) (context.Context, context.CancelFunc) {
	if cc.timeout > 0 {
		return context.WithTimeout(ctx, cc.timeout)
	}
	return context.WithCancel(ctx)
}
