// Package filelock is the crash-safe, non-blocking, exclusive advisory file
// lock behind a workspace's hub lock (#112, internal/hub). (It also served the
// .vibe container's writer lock, #80, until the container was removed.)
//
// The kernel flock, not the file's content, is the lock: it dies with the
// process (even on SIGKILL), so it is never stale. The file's content is
// informational — who holds the lock and, for the hub, how to reach them — and
// a leftover file after a crash is harmless. Content is written under the
// lock and read by others without it.
//
// On non-unix platforms (Windows is not a release target) Acquire succeeds
// without any cross-process exclusion and Probe reports "not held".
package filelock

import (
	"errors"
	"fmt"
)

// ErrHeld is wrapped by the error Acquire returns when another process
// already holds the lock.
var ErrHeld = errors.New("lock is held by another process")

// HeldError is returned by Acquire when the lock is already held.
type HeldError struct{ Path string }

func (e *HeldError) Error() string { return fmt.Sprintf("%s: %s", e.Path, ErrHeld) }
func (e *HeldError) Unwrap() error { return ErrHeld }
