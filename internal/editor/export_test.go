package editor

import "path/filepath"

// ScratchDir is where scratch buffers for a workspace root live.
func ScratchDir(root string) string {
	return filepath.Join(root, ".assay-local", "edit")
}
