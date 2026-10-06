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

// Acquire takes the lock at path (created 0600 if missing), non-blocking, and
// writes payload as its content. It retries a bounded number of times only to
// close a narrow race against a concurrent holder unlinking the file between
// our open and our flock (see the SameFile check); a real conflict returns a
// *HeldError immediately.
func Acquire(path string, payload []byte) (*Lock, error) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		f, err := os.OpenFile(path, openFlags, 0o600)
		if err != nil {
			if errors.Is(err, syscall.ELOOP) {
				return nil, fmt.Errorf("%w: it is a symbolic link", ErrUnsafe)
			}
			return nil, fmt.Errorf("failed to create lock file: %w", err)
		}
		// Check the opened file itself, before locking or writing anything.
		if ofi, serr := f.Stat(); serr != nil {
			f.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to inspect lock file: %w", serr)
		} else if cerr := checkOwnFile(ofi, false); cerr != nil {
			f.Close() //nolint:errcheck
			return nil, cerr
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()                       //nolint:errcheck
			if err == syscall.EWOULDBLOCK { //nolint:errorlint // syscall errno, not wrapped
				return nil, &HeldError{Path: path}
			}
			return nil, fmt.Errorf("failed to lock file: %w", err)
		}
		// The previous holder may have unlinked path (under its own flock)
		// between our open and our flock; confirm the fd we locked still IS
		// the file at path.
		fi1, err1 := f.Stat()
		fi2, err2 := os.Stat(path)
		if err1 != nil || err2 != nil || !os.SameFile(fi1, fi2) {
			f.Close() //nolint:errcheck
			continue
		}
		// A file left by a crashed holder may carry loose permissions.
		if err := f.Chmod(0o600); err != nil {
			f.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to make the lock file private: %w", err)
		}
		l := &Lock{f: f, path: path}
		if err := l.Write(payload); err != nil {
			_ = l.Release()
			return nil, err
		}
		return l, nil
	}
	return nil, fmt.Errorf("failed to lock file: too many concurrent unlink races")
}

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

// Probe reports whether another process holds the lock at path, without
// leaving it held. A missing file is "not held". The probe briefly takes a
// shared flock, so a simultaneous Acquire elsewhere can see one spurious
// conflict; callers that must not fail on that retry once.
func Probe(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		if errors.Is(err, syscall.ELOOP) {
			return false, fmt.Errorf("%w: it is a symbolic link", ErrUnsafe)
		}
		return false, fmt.Errorf("failed to probe lock file: %w", err)
	}
	defer f.Close() //nolint:errcheck
	if fi, serr := f.Stat(); serr != nil {
		return false, fmt.Errorf("failed to probe lock file: %w", serr)
	} else if cerr := checkOwnFile(fi, true); cerr != nil {
		return false, cerr
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK { //nolint:errorlint // syscall errno, not wrapped
			return true, nil
		}
		return false, fmt.Errorf("failed to probe lock file: %w", err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}
