package main

// What Studio opens at startup (a folder or .vibe named on the command line, or
// handed over by the OS), and the native folder pickers.

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// InitialWorkspace is the folder named on the command line, if any.
type InitialWorkspace struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	// BundlePath is a .vibe bundle passed on the command line (the OS opened
	// one with Vibe): the launcher offers to import it.
	BundlePath string `json:"bundlePath"`
}

// launchArgPath turns what the OS hands a launcher into a plain path. A desktop
// entry with Exec=vibe-studio %U passes file:// URIs (percent-encoded), and
// macOS passes plain paths.
func launchArgPath(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(strings.ToLower(arg), "file:") {
		if u, err := url.Parse(arg); err == nil && u.Path != "" && (u.Host == "" || u.Host == "localhost") {
			return u.Path
		}
	}
	return arg
}

// classifyLaunchArg decides what a path handed to Studio at launch means: a
// workspace folder (Path), a .vibe bundle to import (BundlePath), or a plain
// note when it is neither. An empty result means "ignore" (a flag, or nothing).
func classifyLaunchArg(arg string) InitialWorkspace {
	arg = launchArgPath(arg)
	if arg == "" || strings.HasPrefix(arg, "-") {
		return InitialWorkspace{}
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return InitialWorkspace{Message: "Couldn't read " + arg + "."}
	}
	fi, err := os.Stat(abs)
	switch {
	case err == nil && fi.IsDir():
		// A folder is a workspace whatever its name (even x.vibe).
		return InitialWorkspace{Path: abs}
	case strings.EqualFold(filepath.Ext(abs), ".vibe"):
		// A bundle (#118). A file that is not a v2 bundle is told so by the
		// import preview, not here.
		return InitialWorkspace{BundlePath: abs}
	case err != nil:
		return InitialWorkspace{Message: "The folder " + abs + " isn't there."}
	default:
		return InitialWorkspace{Message: abs + " is a file, not a folder. Open a workspace folder, or a .vibe bundle to import it."}
	}
}

// resolveLaunch decides what Studio opens at startup. args are os.Args[1:].
//
//   - The first argument, if a path, wins. A folder holding a vibe.yaml is that workspace; a
//     folder inside a workspace opens the enclosing workspace; any other
//     folder is passed through unchanged (the open flow offers to make it one).
//     A .vibe file, a missing path or a file gets classifyLaunchArg's answer.
//   - With no usable argument, a terminal start opens the workspace found by
//     walking up from cwd (workspace.DiscoverForLaunch, which never selects
//     home, an ancestor of home or /). A desktop-launcher start (cwd is home
//     or /) finds none and yields the zero value: the Launcher, as before.
func resolveLaunch(args []string, cwd, home string) InitialWorkspace {
	// Only the first argument counts (as before); a flag there means "none".
	if len(args) > 0 {
		if r := classifyLaunchArg(args[0]); r != (InitialWorkspace{}) {
			if r.Path != "" && !workspace.HasConfigFile(r.Path) {
				if root, err := workspace.DiscoverForLaunch(r.Path, home); err == nil {
					r.Path = root
				}
			}
			return r
		}
	}
	if cwd == "" {
		return InitialWorkspace{}
	}
	if root, err := workspace.DiscoverForLaunch(cwd, home); err == nil {
		return InitialWorkspace{Path: root}
	}
	return InitialWorkspace{}
}

// setLaunchFromEnvironment records what to open from the command line and the
// working directory.
func (a *App) setLaunchFromEnvironment(args []string) {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	if r := resolveLaunch(args, cwd, home); r != (InitialWorkspace{}) {
		a.launchMu.Lock()
		a.initial = r
		a.launchMu.Unlock()
	}
}

// handleOpenPath is the macOS OnFileOpen entry (an Apple Event, not argv): the
// Finder opened a folder or a .vibe file with Vibe. Before the frontend has
// asked for its launch state the path is queued (the last one wins, as with
// argv); afterwards it is sent as an "app:open-path" event carrying the same
// InitialWorkspace shape GetInitialWorkspace returns.
func (a *App) handleOpenPath(path string) {
	r := classifyLaunchArg(path)
	if r == (InitialWorkspace{}) {
		return
	}
	a.launchMu.Lock()
	ready := a.frontendReady
	if !ready {
		a.initial = r
	}
	a.launchMu.Unlock()
	if ready {
		a.emitEvent("app:open-path", r)
	}
}

// GetInitialWorkspace returns what was named on the command line (or opened
// from the OS before the window was ready): a folder, a bundle, or a note when
// the argument could not be used. Reading does not clear it; MarkLaunchHandled
// does. Calling it marks the frontend ready, so later
// OS open requests arrive as "app:open-path" events instead.
func (a *App) GetInitialWorkspace() InitialWorkspace {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()
	a.frontendReady = true
	return a.initial
}

// MarkLaunchHandled consumes the launch target (#33). The frontend calls it once
// it has acted on what GetInitialWorkspace returned (opened, failed, or the
// person declined a prompt). Until then a reload during the first open still
// finds the target; afterwards a reload shows the Launcher, or whatever
// workspace is open, instead of reopening the launch folder.
func (a *App) MarkLaunchHandled() {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()
	a.initial = InitialWorkspace{}
}

// ChooseFolder opens the native folder picker.
func (a *App) ChooseFolder(title string) (string, error) {
	if title == "" {
		title = "Choose a folder"
	}
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
	if err != nil {
		return "", fmt.Errorf("failed to open the folder dialog: %w", err)
	}
	return path, nil
}

// SelectWorkspaceFolder opens the native folder picker for "Open folder…".
func (a *App) SelectWorkspaceFolder() (string, error) {
	return a.ChooseFolder("Open a workspace folder")
}
