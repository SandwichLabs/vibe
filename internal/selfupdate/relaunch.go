package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Relaunch starts the freshly installed Studio as a separate, detached process: on Linux the
// recorded vibe-studio binary, on macOS the app via `open -n -a`. args are passed on (a
// workspace folder to reopen). target is the ApplyResult.Restart path; it must be absolute and exist.
// Call it only once the old instance has committed to quitting and let go of its workspace, so the
// new one never fights it for the hub lock.
func Relaunch(goos, target string, args ...string) error {
	if !filepath.IsAbs(target) {
		return fmt.Errorf("refusing to start %q: not an absolute path", target)
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("the new Studio is not there: %w", err)
	}
	var cmd *exec.Cmd
	if goos == "darwin" {
		cmd = exec.Command("open", append([]string{"-n", "-a", target, "--args"}, args...)...)
	} else {
		cmd = exec.Command(target, args...)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the new Studio: %w", err)
	}
	return cmd.Process.Release()
}
