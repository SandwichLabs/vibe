package main

import (
	"context"
	"errors"
	"sync"
)

// errRunCancelled is the fixed error a run the person stopped returns. The
// frontend matches this exact text (lib/errors.ts isCancelled) to show a
// neutral "Cancelled" line instead of a red error.
var errRunCancelled = errors.New("cancelled")

// Run keys name what a person can cancel. One run per key is in flight: a query
// editor tab ("query:<tabId>"), or one of the Semantics view's three runs
// (it runs one at a time, so the kind is enough).
const (
	runKeyMetric     = "metric"
	runKeyAssertions = "assertions"
	runKeyProfile    = "profile"
)

func queryRunKey(tabID string) string { return "query:" + tabID }

// runRegistry tracks the cancellable runs of the open workspace. It is the
// per-run counterpart of chatTurns: a run's context is derived from the app's,
// so a workspace close or app shutdown still stops it, and the registry only
// adds the person's own Stop.
type runRegistry struct {
	mu   sync.Mutex
	runs map[string]*runEntry
}

type runEntry struct{ cancel context.CancelFunc }

// begin registers a run under key with a context derived from parent. A run
// already registered under the same key (a double click, a second Run in the
// same tab) is cancelled first, so one key never has two runs fighting over
// its result. The returned func ends the run and must be deferred.
func (r *runRegistry) begin(parent context.Context, key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	e := &runEntry{cancel: cancel}
	r.mu.Lock()
	if r.runs == nil {
		r.runs = map[string]*runEntry{}
	}
	if old := r.runs[key]; old != nil {
		old.cancel()
	}
	r.runs[key] = e
	r.mu.Unlock()
	return ctx, func() {
		r.mu.Lock()
		if r.runs[key] == e {
			delete(r.runs, key)
		}
		r.mu.Unlock()
		cancel()
	}
}

// cancel stops the run registered under key. A no-op when none is (it usually
// just finished).
func (r *runRegistry) cancel(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.runs[key]; e != nil {
		e.cancel()
	}
}

// cancelAll stops every run (a workspace close or switch).
func (r *runRegistry) cancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.runs {
		e.cancel()
	}
}

// cancelledOr reports errRunCancelled for a run whose context was cancelled
// (by Stop, a replacing run or a close), and err otherwise. A deadline is not a
// cancel: it keeps its own message.
func cancelledOr(ctx context.Context, err error) error {
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return errRunCancelled
	}
	return err
}

// CancelRun stops the running query of an editor tab (key "query:<tabId>") or a
// Semantics run (key "metric", "assertions" or "profile"). A run that already
// finished, or a key that names none, is a no-op.
func (a *App) CancelRun(key string) {
	a.runs.cancel(key)
}
