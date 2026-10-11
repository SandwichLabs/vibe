//go:build unix

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// ErrUnsafe is returned when the lock path is not a plain file we own: a
// symlink, a FIFO or device, a hard-linked file, or one owned by another user.
// Acquire never writes through such a path (a committed symlink must not turn
// the lock into "overwrite any file you can write").
var ErrUnsafe = errors.New("the lock file is not a plain file owned by you")

// openFlags: never follow a symlink, never block on a FIFO, never leak into a
// child process.
const openFlags = os.O_RDWR | os.O_CREATE | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC

// checkOwnFile verifies fi describes a regular, single-link file owned by this
// uid. forProbe relaxes only the link count (a read-only probe writes nothing).
func checkOwnFile(fi os.FileInfo, forProbe bool) error {
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrUnsafe)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: cannot read its owner", ErrUnsafe)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%w: it belongs to another user", ErrUnsafe)
	}
	if !forProbe && st.Nlink != 1 {
		return fmt.Errorf("%w: it has other hard links", ErrUnsafe)
	}
	return nil
}

// Lock is a held exclusive lock.
type Lock struct {
	f    *os.File
	path string
	// dirfd/at: the lock was taken with AcquireAt; path is then a name inside
	// that directory and Release unlinks relative to it.
	dirfd int
	at    bool
}

// Path is the lock file's path.
func (l *Lock) Path() string { return l.path }

// Write replaces the lock file's content. The new bytes are written before
// the file is truncated, so a concurrent reader never sees an empty file; it
// may briefly see a torn mix and must retry on a parse error.
func (l *Lock) Write(payload []byte) error {
	if l == nil || l.f == nil {
		return fmt.Errorf("lock is released")
	}
	if _, err := l.f.WriteAt(payload, 0); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	if err := l.f.Truncate(int64(len(payload))); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	return nil
}

// Release drops the lock and removes the file. The unlink happens while still
// holding the flock (paired with Acquire's SameFile check). Safe to call more
// than once.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	var firstErr error
	var rmErr error
	if l.at {
		rmErr = unix.Unlinkat(l.dirfd, l.path, 0)
	} else {
		rmErr = os.Remove(l.path)
	}
	if rmErr != nil && !os.IsNotExist(rmErr) && !errors.Is(rmErr, syscall.ENOENT) {
		firstErr = rmErr
	}
	if err := l.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	l.f = nil
	return firstErr
}
