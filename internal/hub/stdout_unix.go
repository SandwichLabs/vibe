//go:build unix

package hub

import (
	"fmt"
	"os"
	"syscall"
)

func redirectStdout() (*os.File, error) {
	fd, err := syscall.Dup(1)
	if err != nil {
		return nil, fmt.Errorf("failed to keep a private handle on stdout: %w", err)
	}
	syscall.CloseOnExec(fd)
	if err := dup2(2, 1); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("failed to point stdout at stderr: %w", err)
	}
	return os.NewFile(uintptr(fd), "mcp-stdout"), nil
}
