package repl

import (
	"errors"
	"sync"
)

// ErrTooManySessions is returned by New when the workspace already has as many
// consoles open as its Limiter allows.
var ErrTooManySessions = errors.New("too many console sessions open for this workspace")

// Limiter caps concurrent sessions per workspace (each pins one connection).
// New takes a slot and Close gives it back.
type Limiter struct {
	mu  sync.Mutex
	n   int
	max int
}

// NewLimiter returns a Limiter allowing max sessions at once.
func NewLimiter(max int) *Limiter { return &Limiter{max: max} }

func (l *Limiter) acquire() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n >= l.max {
		return ErrTooManySessions
	}
	l.n++
	return nil
}

func (l *Limiter) release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.n--
	l.mu.Unlock()
}
