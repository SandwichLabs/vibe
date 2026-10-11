//go:build !unix

package filelock

import "errors"

// ErrUnsafe is never returned here; it exists so callers compile everywhere.
var ErrUnsafe = errors.New("the lock file is not a plain file owned by you")

// Lock is a no-op on non-unix platforms: there is no cross-process exclusion.
type Lock struct{ path string }

// Path is the lock file's path.
func (l *Lock) Path() string { return l.path }

// Write is a no-op.
func (l *Lock) Write([]byte) error { return nil }

// Release is a no-op.
func (l *Lock) Release() error { return nil }
