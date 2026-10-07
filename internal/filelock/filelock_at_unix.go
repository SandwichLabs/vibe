//go:build unix

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// AcquireAt is Acquire for the file name inside the already-open directory
// dir. Every step (open, stat, unlink) is relative to dir's descriptor, so
// swapping the directory's path for a symlink after dir was checked cannot
// redirect the lock. dir must stay open until Release returns.
func AcquireAt(dir *os.File, name string, payload []byte) (*Lock, error) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		fd, err := unix.Openat(int(dir.Fd()), name, openFlags|unix.O_CLOEXEC, 0o600)
		if err != nil {
			if errors.Is(err, syscall.ELOOP) {
				return nil, fmt.Errorf("%w: it is a symbolic link", ErrUnsafe)
			}
			return nil, fmt.Errorf("failed to create lock file: %w", err)
		}
		f := os.NewFile(uintptr(fd), name)
		if ofi, serr := f.Stat(); serr != nil {
			f.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to inspect lock file: %w", serr)
		} else if cerr := checkOwnFile(ofi, false); cerr != nil {
			f.Close() //nolint:errcheck
			return nil, cerr
		}
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()                       //nolint:errcheck
			if err == syscall.EWOULDBLOCK { //nolint:errorlint // syscall errno, not wrapped
				return nil, &HeldError{Path: name}
			}
			return nil, fmt.Errorf("failed to lock file: %w", err)
		}
		var st1, st2 unix.Stat_t
		if unix.Fstat(fd, &st1) != nil || unix.Fstatat(int(dir.Fd()), name, &st2, unix.AT_SYMLINK_NOFOLLOW) != nil ||
			st1.Dev != st2.Dev || st1.Ino != st2.Ino {
			f.Close() //nolint:errcheck
			continue
		}
		if err := f.Chmod(0o600); err != nil {
			f.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to make the lock file private: %w", err)
		}
		l := &Lock{f: f, path: name, dirfd: int(dir.Fd()), at: true}
		if err := l.Write(payload); err != nil {
			_ = l.Release()
			return nil, err
		}
		return l, nil
	}
	return nil, fmt.Errorf("failed to lock file: too many concurrent unlink races")
}

// ProbeAt is Probe for name inside dir.
func ProbeAt(dir *os.File, name string) (bool, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return false, nil
		}
		if errors.Is(err, syscall.ELOOP) {
			return false, fmt.Errorf("%w: it is a symbolic link", ErrUnsafe)
		}
		return false, fmt.Errorf("failed to probe lock file: %w", err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close() //nolint:errcheck
	if fi, serr := f.Stat(); serr != nil {
		return false, fmt.Errorf("failed to probe lock file: %w", serr)
	} else if cerr := checkOwnFile(fi, true); cerr != nil {
		return false, cerr
	}
	if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK { //nolint:errorlint // syscall errno, not wrapped
			return true, nil
		}
		return false, fmt.Errorf("failed to probe lock file: %w", err)
	}
	_ = syscall.Flock(fd, syscall.LOCK_UN)
	return false, nil
}

// OpenAt opens name inside dir read-only, never following a symlink or
// blocking on a FIFO.
func OpenAt(dir *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// OpenDirNoFollow opens the directory name inside parent, refusing a symlink
// (the caller fstat-checks the owner).
func OpenDirNoFollow(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// MkdirAt creates name (0700) inside parent.
func MkdirAt(parent *os.File, name string) error {
	return unix.Mkdirat(int(parent.Fd()), name, 0o700)
}
