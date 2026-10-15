package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// stage makes the type's private snapshot, if it takes one. done reports that a
// snapshot folder exists and must be removed again unless the attach succeeds.
func (h *hub) stage(ctx context.Context, conn *sql.Conn, t ConnectionType, spec Spec) (Spec, bool, error) {
	st, ok := t.(Stager)
	if !ok {
		return spec, false, nil
	}
	dir, err := h.stageDir(spec.Name)
	if err != nil {
		return spec, false, err
	}
	out, err := st.Stage(ctx, conn, spec, dir)
	if err != nil {
		h.removeStage(spec.Name)
		return spec, false, err
	}
	return out, true, nil
}

// stageDir returns an empty private folder for one connection's snapshot,
// under a private root that is created on first use.
func (h *hub) stageDir(name string) (string, error) {
	h.stageMu.Lock()
	defer h.stageMu.Unlock()
	if h.stageRoot == "" {
		root, err := os.MkdirTemp(h.tempDir, "vibe-catalogs-*")
		if err != nil {
			return "", fmt.Errorf("could not make a private folder for the catalog: %w", err)
		}
		if err := os.Chmod(root, 0o700); err != nil {
			_ = os.RemoveAll(root)
			return "", fmt.Errorf("could not secure the private folder for the catalog: %w", err)
		}
		h.stageRoot = root
	}
	dir := filepath.Join(h.stageRoot, name)
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("could not clear the private folder for the catalog: %w", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not make a private folder for the catalog: %w", err)
	}
	return dir, nil
}

// removeStage deletes one connection's snapshot folder.
func (h *hub) removeStage(name string) {
	h.stageMu.Lock()
	defer h.stageMu.Unlock()
	if h.stageRoot != "" {
		_ = os.RemoveAll(filepath.Join(h.stageRoot, name))
	}
}

// removeStages deletes every snapshot (the session is closing).
func (h *hub) removeStages() {
	h.stageMu.Lock()
	defer h.stageMu.Unlock()
	if h.stageRoot != "" {
		_ = os.RemoveAll(h.stageRoot)
		h.stageRoot = ""
	}
}
