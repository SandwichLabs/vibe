package pagehost

import (
	"sync"
	"time"
)

// Limits of one page window: how many requests may be in flight, and how many
// may start in any minute.
const (
	MaxInFlight  = 4
	MaxPerMinute = 60
	// MaxLinesPerSecond bounds every message a window sends, requests or not:
	// a page that posts faster than this is flooding Studio.
	MaxLinesPerSecond = 400
	// MaxPutsPerSecond bounds the chunks of an upload, which are acknowledged one
	// at a time and so pace themselves: a 20 MB file is about 500 of them.
	MaxPutsPerSecond = 3000
)

// Limiter admits at most MaxInFlight requests at once and MaxPerMinute in any
// minute.
type Limiter struct {
	mu       sync.Mutex
	inFlight int
	starts   []time.Time
}

// Acquire admits a request, or reports that a limit is reached. Call Release
// when an admitted request ends.
func (l *Limiter) Acquire(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-time.Minute)
	keep := l.starts[:0]
	for _, t := range l.starts {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.starts = keep
	if l.inFlight >= MaxInFlight || len(l.starts) >= MaxPerMinute {
		return false
	}
	l.inFlight++
	l.starts = append(l.starts, now)
	return true
}

// Release ends an admitted request.
func (l *Limiter) Release() {
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
}

// Flood counts messages in one-second buckets.
type Flood struct {
	mu     sync.Mutex
	second int64
	n      int
}

// Hit records one message at now and reports whether the second's count is
// over MaxLinesPerSecond.
func (f *Flood) Hit(now time.Time) bool { return f.HitLimit(now, MaxLinesPerSecond) }

// HitLimit is Hit with a limit of max messages in the second.
func (f *Flood) HitLimit(now time.Time, max int) bool { return f.HitN(now, 1, max) }

// MaxPutBytesPerSecond bounds the bytes of upload chunks in a second: the
// order of what 400 full lines allow (P4), so chunks are no cheaper than lines.
const MaxPutBytesPerSecond = 32 << 20

// HitN counts n units at now (a message, or its bytes) and reports whether the
// second's count is over max.
func (f *Flood) HitN(now time.Time, n, max int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := now.Unix(); s != f.second {
		f.second, f.n = s, 0
	}
	f.n += n
	return f.n > max
}

// Rate admits at most n events in any window of length per (the `ask`
// capability's 10 calls a minute).
type Rate struct {
	mu     sync.Mutex
	n      int
	per    time.Duration
	starts []time.Time
}

// NewRate returns a Rate of n events per window.
func NewRate(n int, per time.Duration) *Rate { return &Rate{n: n, per: per} }

// Allow records an event at now and reports whether it is within the rate.
func (r *Rate) Allow(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cut := now.Add(-r.per)
	keep := r.starts[:0]
	for _, t := range r.starts {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	r.starts = keep
	if len(r.starts) >= r.n {
		return false
	}
	r.starts = append(r.starts, now)
	return true
}
