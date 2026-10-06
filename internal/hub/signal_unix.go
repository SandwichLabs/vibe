//go:build unix

package hub

import (
	"fmt"
	"syscall"
)

// killRelease verifies, then kill(2)s: the best a platform without pidfd can do.
func killRelease(pid int, verify func() error) error {
	if err := syscall.Kill(pid, 0); err != nil {
		return fmt.Errorf("the holder (pid %d) is not running: %w", pid, err)
	}
	if err := verify(); err != nil {
		return err
	}
	if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		return fmt.Errorf("failed to signal pid %d: %w", pid, err)
	}
	return nil
}
