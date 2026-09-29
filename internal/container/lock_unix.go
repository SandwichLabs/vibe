//go:build unix

package container

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

// fileLock is an exclusive advisory lock on a .vibe file's sidecar
// <file>.lock, held via flock(2) for the lifetime of a writable Container.
type fileLock struct {
	f    *os.File
	path string
}

// acquireLock takes the writer lock for vibePath, non-blocking. It retries a
// bounded number of times only to close a narrow race against a concurrent
// holder unlinking its lock file between our open and our flock (see the
// SameFile check below) — a real conflict returns *InUseError immediately.
func acquireLock(vibePath, app string) (*fileLock, error) {
	lp := lockPath(vibePath)

	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		f, err := os.OpenFile(lp, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("failed to create lock file: %w", err)
		}

		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			if err == syscall.EWOULDBLOCK { //nolint:errorlint // syscall errno, not wrapped
				holder := readHolder(lp)
				f.Close() //nolint:errcheck
				return nil, &InUseError{Path: vibePath, Holder: holder}
			}
			f.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to lock file: %w", err)
		}

		// Guard against the previous holder having unlinked lp (in its own
		// release, under its own flock) between our Open and our Flock: we
		// may now hold a flock on an unlinked inode that a fresh
		// acquireLock elsewhere already succeeded against via a newly
		// created lp. Confirm the fd we locked still IS the file at lp.
		fi1, err1 := f.Stat()
		fi2, err2 := os.Stat(lp)
		if err1 != nil || err2 != nil || !os.SameFile(fi1, fi2) {
			f.Close() //nolint:errcheck
			continue
		}

		holder := LockHolder{App: app, PID: os.Getpid()}
		if hostname, err := os.Hostname(); err == nil {
			holder.Host = hostname
		}
		if data, err := json.Marshal(holder); err == nil {
			f.Truncate(0)      //nolint:errcheck
			f.WriteAt(data, 0) //nolint:errcheck
		}

		return &fileLock{f: f, path: lp}, nil
	}

	return nil, fmt.Errorf("failed to lock file: too many concurrent unlink races")
}

// release drops the lock and removes the sidecar file. The unlink happens
// while still holding the flock (paired with acquireLock's SameFile check)
// so a concurrent acquireLock elsewhere never locks an inode we are about to
// remove out from under it.
func (l *fileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	var firstErr error
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	if err := l.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
