package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/SandwichLabs/vibe/internal/bundle"
	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- Notebooks (#66 part A) ---
//
// SQL execution for a notebook cell reuses RunA2UIQuery — a notebook's sql
// cell is a read query, the same live binding an A2UI page's data-bound
// component uses (same sandbox-gated classifier, same read-only-only gate).
// Writes belong in the Query Explorer or the table browser, not in a
// notebook cell; RunA2UIQuery already rejects a write with
// errReadOnlyBindingDenied, and the frontend surfaces that as "Notebook
// cells are read-only queries — use the Query editor to change data."

// NotebooksChangedEvent is the payload of every "notebooks:changed" Wails
// event (review fix, finding 2). AppendedCellIDs is the load-bearing field:
// an open NotebookView.vue merges in ONLY cell ids listed here — never "any
// remote id we don't already have locally" (the pre-fix behavior), which
// could resurrect a cell the user had just deleted locally but not yet
// saved. Every emitter that is NOT "an agent tool appended a specific new
// cell" (CreateNotebook, SaveNotebook, RenameNotebook, DeleteNotebook,
// ImportNotebook) omits AppendedCellIDs entirely — the frontend treats an
// event with no AppendedCellIDs as "not an append, nothing to merge" (this
// is also how NotebookView tells its OWN SaveNotebook echo apart from a
// remote append: its own save never sets this field).
type NotebooksChangedEvent struct {
	NotebookID      string   `json:"notebookId"`
	AppendedCellIDs []string `json:"appendedCellIds,omitempty"`
}

// emitNotebooksChanged emits "notebooks:changed" with the shared payload
// shape — see NotebooksChangedEvent.
func (a *App) emitNotebooksChanged(notebookID string, appendedCellIDs []string) {
	a.emitEvent("notebooks:changed", NotebooksChangedEvent{NotebookID: notebookID, AppendedCellIDs: appendedCellIDs})
}

// ListNotebooks returns every notebook's summary, newest-updated first.
func (a *App) ListNotebooks() ([]notebooks.Summary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return a.ws.Notebooks().List(context.Background())
}

// GetNotebook loads one notebook, rebuilt through notebooks.Coerce.
func (a *App) GetNotebook(id string) (*notebooks.Notebook, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	nb, warnings, err := a.ws.Notebooks().Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		a.logger.Warn("notebook coercion warning", "notebook_id", id, "warning", w)
	}
	return &nb, nil
}

// SetActiveNotebook records which notebook is currently open in
// NotebookView.vue (or clears it with ""), for the chat/MCP agent's
// add_query_cell/create_dashboard tools (#67 deferred part) — "append to the
// notebook currently open in Studio" needs the frontend to say which one
// that is; there is no other way for a Go binding to know. The view calls
// this on mount, on switching notebooks, and with "" on unmount/close.
func (a *App) SetActiveNotebook(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeNotebookID = id
}

// CreateNotebook creates a new, empty notebook and returns it.
func (a *App) CreateNotebook(title string) (*notebooks.Notebook, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	nb, err := a.ws.Notebooks().Create(context.Background(), title)
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &nb, nil
}

// NotebookSaveResult wraps a saved/imported notebook with any warnings
// notebooks.Coerce produced — a save can silently drop or clamp fields (an
// unknown cell type, an out-of-range view, an invalid a2ui page), and the
// caller needs to know rather than have it happen invisibly.
type NotebookSaveResult struct {
	Notebook notebooks.Notebook `json:"notebook"`
	Warnings []string           `json:"warnings"`
}

// SaveNotebook rebuilds raw (the frontend's in-memory notebook, JSON-decoded
// into a plain map by Wails) through notebooks.Coerce and upserts it. This is
// the ONE write path for a notebook's title/cells — the frontend's add/move/
// delete/collapse cell operations all mutate its local copy and call this
// with the whole document, matching the "outputs are never persisted, only
// source/view/page are" contract (Coerce only ever keeps those fields).
// sessionID must be the value GetSessionID() returned at the time
// the caller LOADED the notebook it is now saving (see App.sessionID's doc
// comment) — never re-fetched at save time, or this check couldn't catch
// the bug it exists for (a save that fires after the file underneath it was
// already switched). A mismatch means the file was closed/reopened/switched
// since this notebook was loaded, so the write is rejected rather than
// silently landing in the wrong container.
func (a *App) SaveNotebook(raw map[string]any, sessionID string) (*NotebookSaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if sessionID != a.sessionID {
		return nil, fmt.Errorf("stale workspace session: the open workspace changed since this notebook was loaded; reopen it and try again")
	}
	nb, warnings, err := a.ws.Notebooks().Save(context.Background(), raw)
	if err != nil {
		return nil, conflictErr(err)
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &NotebookSaveResult{Notebook: nb, Warnings: warnings}, nil
}

// DeleteNotebook removes a notebook by id.
func (a *App) DeleteNotebook(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := a.ws.Notebooks().Delete(context.Background(), id); err != nil {
		return err
	}
	a.emitNotebooksChanged(id, nil)
	return nil
}

// notebookExportFile is the shape ExportNotebook writes and ImportNotebook
// reads — matching model.js's toFile()/coerceNotebook's accepted wrapper
// shape ({kind, schemaVersion, notebook}), adapted to this product's file
// kind (notebooks.FileKind).
type notebookExportFile struct {
	Kind          string             `json:"kind"`
	SchemaVersion int                `json:"schemaVersion"`
	ExportedAt    string             `json:"exportedAt"`
	Notebook      notebooks.Notebook `json:"notebook"`
}

// ExportNotebook writes one notebook to a JSON file. If dst is empty, a save
// dialog picks the destination; dst == "" and a cancelled dialog both return
// ("", nil), matching ExportQueryResults's cancel semantics.
func (a *App) ExportNotebook(id, dst string) (string, error) {
	a.mu.RLock()
	if err := a.requireWorkspace(); err != nil {
		a.mu.RUnlock()
		return "", err
	}
	nb, warnings, err := a.ws.Notebooks().Get(context.Background(), id)
	a.mu.RUnlock()
	if err != nil {
		return "", err
	}
	for _, w := range warnings {
		a.logger.Warn("notebook coercion warning on export", "notebook_id", id, "warning", w)
	}

	path := dst
	if path == "" {
		path, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Export Notebook",
			DefaultFilename: nb.Title + ".json",
			Filters: []runtime.FileFilter{
				{DisplayName: "Vibe Notebook (*.json)", Pattern: "*.json"},
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to open save dialog: %w", err)
		}
		if path == "" {
			return "", nil
		}
	}

	file := notebookExportFile{
		Kind: notebooks.FileKind, SchemaVersion: notebooks.SchemaVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), Notebook: nb,
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal notebook: %w", err)
	}
	if err := filestore.WriteFileAtomic(path, b, 0o644); err != nil {
		return "", fmt.Errorf("failed to write notebook file: %w", err)
	}
	return path, nil
}

// SelectNotebookFile opens a file dialog for choosing an exported notebook
// JSON file — ImportNotebook's picker step.
func (a *App) SelectNotebookFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import Notebook",
		Filters: []runtime.FileFilter{
			{DisplayName: "Vibe Notebook (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// ImportNotebook reads path (an ExportNotebook file) and imports it as a
// brand-new notebook, through notebooks.Coerce like every other load. Import
// never overwrites a notebook already in this container — the file's own id/
// updatedAt are stripped before Coerce so a fresh id is always assigned,
// matching model.js's importFile ("Import always lands as a new notebook").
// maxImportNotebookFileBytes caps an imported notebook file at the same
// per-entry limit a .vibe bundle applies (bundle.MaxEntryBytes, #118), so a
// single notebook and a bundled one share one validation path (review
// fix, step 4a #7c) — checked via os.Stat BEFORE the file is read, so a
// multi-hundred-MB file (or a symlink/fifo pointed at one) never gets
// fully loaded into memory just to be rejected.
const maxImportNotebookFileBytes = bundle.MaxEntryBytes

func (a *App) ImportNotebook(path string) (*NotebookSaveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file: %w", err)
	}
	if info.Size() > maxImportNotebookFileBytes {
		return nil, fmt.Errorf("notebook file is %d bytes, exceeding the %d byte import limit", info.Size(), maxImportNotebookFileBytes)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file: %w", err)
	}
	var wrapper map[string]any
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid notebook file: %w", err)
	}
	// ImportNotebook specifically (unlike Storage.Get/SaveNotebook, which
	// stay permissive about a bare notebook object with no "kind") requires
	// the file to be self-identified as a Vibe notebook export — review fix,
	// step 4a #7b: without this, importing an arbitrary JSON file that
	// happens to parse (e.g. package.json) would silently either fail deep
	// inside Coerce with a confusing error, or in a pathological case
	// coincidentally look enough like a notebook to import garbage.
	if kind, _ := wrapper["kind"].(string); kind != notebooks.FileKind {
		return nil, fmt.Errorf("not a Vibe notebook file (expected \"kind\": %q, got %q)", notebooks.FileKind, kind)
	}
	if nested, ok := wrapper["notebook"].(map[string]any); ok {
		delete(nested, "id")
		delete(nested, "updatedAt")
	} else {
		delete(wrapper, "id")
		delete(wrapper, "updatedAt")
	}

	// The same checks a notebook in a bundle gets (bundle.CheckNotebook), then
	// the store's own Save.
	inner, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("invalid notebook file: %w", err)
	}
	if _, _, err := bundle.CheckNotebook(inner); err != nil {
		return nil, err
	}
	nb, warnings, err := a.ws.Notebooks().Save(context.Background(), wrapper)
	if err != nil {
		return nil, err
	}
	a.emitNotebooksChanged(nb.ID, nil)
	return &NotebookSaveResult{Notebook: nb, Warnings: warnings}, nil
}
