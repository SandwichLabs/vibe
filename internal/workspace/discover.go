package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/prefs"
)

// ExactRoot returns the workspace root for path when path itself is that root (a
// folder holding a vibe.yaml, not one inside a workspace). ok is false for
// anything else, including a folder that merely sits inside a workspace.
func ExactRoot(path string) (root string, ok bool) {
	root, err := Discover(path)
	if err != nil || !prefs.SamePath(root, path) {
		return "", false
	}
	return root, true
}

// Discover walks up from start to the nearest directory holding a vibe.yaml,
// like git finds .git, and returns that directory (absolute, symlinks
// resolved). start may be a file or a directory, and need not exist as long as
// some ancestor does. ErrNotWorkspace when nothing is found.
func Discover(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %q: %w", start, err)
	}
	// Resolve symlinks of the deepest existing ancestor so two routes to one
	// folder agree on its identity.
	existing := abs
	var tail []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
	if real, err := filepath.EvalSymlinks(existing); err == nil {
		existing = real
	}
	dir := filepath.Join(append([]string{existing}, tail...)...)
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if fi, err := os.Lstat(filepath.Join(dir, ConfigFile)); err == nil && fi.Mode().IsRegular() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w from %s", ErrNotWorkspace, start)
		}
		dir = parent
	}
}

// DiscoverForLaunch is Discover for something that opens a workspace without
// being asked to (Studio started from a terminal): it walks up from start the
// same way, but ignores a vibe.yaml that is home itself, any ancestor of home,
// or the filesystem root. Those are where a stray file turns every desktop
// launch into "open that workspace". A workspace strictly below home, or in a
// tree that does not contain home at all (/srv/x), is found normally. An empty
// home applies only the filesystem-root rule. ErrNotWorkspace when none counts.
func DiscoverForLaunch(start, home string) (string, error) {
	root, err := Discover(start)
	if err != nil {
		return "", err
	}
	if filepath.Dir(root) == root {
		return "", fmt.Errorf("%w from %s", ErrNotWorkspace, start)
	}
	if home != "" {
		// Both spellings of home count: the one given (cleaned) and its
		// symlink-resolved form, so a vibe.yaml above either is ignored.
		homes := []string{home}
		if abs, err := filepath.Abs(home); err == nil {
			homes = []string{abs}
		}
		if real, err := filepath.EvalSymlinks(homes[0]); err == nil && real != homes[0] {
			homes = append(homes, real)
		}
		for _, h := range homes {
			if filestore.Within(root, h) {
				return "", fmt.Errorf("%w from %s", ErrNotWorkspace, start)
			}
		}
	}
	return root, nil
}
