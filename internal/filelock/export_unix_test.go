//go:build unix

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Test-only helpers: kept out of the production build.

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
