//go:build linux

package hub

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Syscall numbers are the same on every Linux architecture (>= 5.3).
const (
	sysPidfdOpen       = 434
	sysPidfdSendSignal = 424
)

// sendRelease signals pid through a pidfd: the descriptor pins the process we
// opened, so if the pid is recycled between verify and the signal, the signal
// goes to the (dead) original, not a stranger. verify runs after the pidfd is
// open and must confirm the lock still names this pid. Old kernels without
// pidfd fall back to kill after the same verify.
func sendRelease(pid int, verify func() error) error {
	fd, _, errno := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if errno != 0 {
		if errno == syscall.ENOSYS || errno == syscall.EPERM || errno == syscall.EINVAL {
			return killRelease(pid, verify)
		}
		return fmt.Errorf("the holder (pid %d) is not running: %w", pid, errno)
	}
	defer syscall.Close(int(fd)) //nolint:errcheck
	if err := verify(); err != nil {
		return err
	}
	_, _, errno = syscall.Syscall6(sysPidfdSendSignal, fd, uintptr(syscall.SIGUSR1), uintptr(unsafe.Pointer(nil)), 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("failed to signal pid %d: %w", pid, errno)
	}
	return nil
}
