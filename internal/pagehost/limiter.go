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
func (f *Flood) Hit(now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := now.Unix(); s != f.second {
		f.second, f.n = s, 0
	}
	f.n++
	return f.n > MaxLinesPerSecond
}
