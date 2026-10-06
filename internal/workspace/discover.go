package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

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
