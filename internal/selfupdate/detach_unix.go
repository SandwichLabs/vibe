//go:build unix

package selfupdate

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session so it outlives this process.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// ownProcessGroup runs the install script in its own process group and, on
// cancellation, sends SIGTERM to the whole group: the script's curl/tar children
// stop too, and install.sh's EXIT/TERM trap gets to remove its temp dir.
// WaitDelay still bounds how long Wait lingers afterwards.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
}
