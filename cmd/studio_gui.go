package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

const (
	guiBinName = "vibe-studio"
	guiAppName = "Vibe Studio.app"
)

// guiLauncher finds the desktop app. Every lookup is a field so tests can fake the file system.
type guiLauncher struct {
	goos         string
	home         string
	executable   func() (string, error)
	evalSymlinks func(string) (string, error)
	isFile       func(string) bool // a regular file (the vibe-studio binary)
	isDir        func(string) bool // a directory (the .app bundle)
	lookPath     func(string) (string, error)
}

// guiCommand is the argv of the process that replaces `vibe`.
type guiCommand struct {
	path string
	argv []string
}

func newGUILauncher() guiLauncher {
	home, _ := os.UserHomeDir()
	return guiLauncher{
		goos:         runtime.GOOS,
		home:         home,
		executable:   os.Executable,
		evalSymlinks: filepath.EvalSymlinks,
		isFile: func(p string) bool {
			fi, err := os.Stat(p)
			return err == nil && fi.Mode().IsRegular()
		},
		isDir: func(p string) bool {
			fi, err := os.Stat(p)
			return err == nil && fi.IsDir()
		},
		lookPath: exec.LookPath,
	}
}

// find looks for, in order: a vibe-studio binary beside the real vibe executable (symlinks are
// resolved, so a Homebrew-style link into another directory still finds its neighbour); vibe-studio
// on PATH; on macOS only, the Vibe Studio.app bundle beside the CLI, in ~/Applications or in
// /Applications, started with `open -a`. target is the one argument Studio opens (empty for none).
func (l guiLauncher) find(target string) (guiCommand, error) {
	argv := []string{guiBinName}
	var openArgs []string
	if target != "" {
		argv = append(argv, target)
		openArgs = []string{target}
	}
	var searched []string

	var exeDir string
	if exe, err := l.executable(); err == nil {
		if resolved, rerr := l.evalSymlinks(exe); rerr == nil {
			exe = resolved
		}
		exeDir = filepath.Dir(exe)
		beside := filepath.Join(exeDir, guiBinName)
		searched = append(searched, beside)
		if l.isFile(beside) {
			return guiCommand{path: beside, argv: argv}, nil
		}
	}

	if found, err := l.lookPath(guiBinName); err == nil {
		return guiCommand{path: found, argv: argv}, nil
	}
	searched = append(searched, "$PATH")

	if l.goos == "darwin" {
		var apps []string
		if exeDir != "" {
			apps = append(apps, filepath.Join(exeDir, guiAppName))
		}
		if l.home != "" {
			apps = append(apps, filepath.Join(l.home, "Applications", guiAppName))
		}
		apps = append(apps, filepath.Join("/Applications", guiAppName))
		for _, app := range apps {
			searched = append(searched, app)
			if l.isDir(app) {
				return guiCommand{path: "/usr/bin/open", argv: append([]string{"open", "-a", app}, openArgs...)}, nil
			}
		}
	}

	return guiCommand{}, fmt.Errorf("%s binary not found; searched: %s (`vibe studio` without --gui opens the terminal UI)", guiBinName, strings.Join(searched, ", "))
}

// launchGUI finds the desktop app and executes it, replacing the current process. dir is passed
// through as its one argument, the workspace folder Studio opens at launch; empty means no argument.
func launchGUI(dir string) error {
	target := ""
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve %q: %w", dir, err)
		}
		target = abs
	}
	c, err := newGUILauncher().find(target)
	if err != nil {
		return err
	}
	return syscall.Exec(c.path, c.argv, os.Environ())
}
