//go:build unix

package trustfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockPath takes an exclusive advisory lock on <path>.lock, blocking until it
// is free, and returns the release func. The lock is on the open file, so it is
// dropped if the process dies. It serialises load-modify-save across processes
// (Studio and the CLI) so one decision cannot overwrite another.
func lockPath(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to lock decisions: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to lock decisions: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
