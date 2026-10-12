package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/notebooks"
)

// ReadNotebookFile reads a notebook export file (the "kind" wrapper
// notebooks.FileKind) from path and returns the document ready to hand to a
// notebook store's Save as a brand-new notebook: the file's own id and
// updatedAt are stripped so a fresh id is always assigned (import never
// overwrites a notebook already in the workspace).
//
// The file is held to the same limits as a notebook in a .vibe bundle: its size
// is checked with Stat BEFORE it is read (MaxEntryBytes), so a huge file, or a
// symlink or fifo pointed at one, is never loaded into memory just to be
// rejected; it must identify itself as a Vibe notebook (an arbitrary JSON file
// such as package.json is refused); and it passes CheckNotebook.
func ReadNotebookFile(path string) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file: %w", err)
	}
	if info.Size() > MaxEntryBytes {
		return nil, fmt.Errorf("notebook file is %d bytes, exceeding the %d byte import limit", info.Size(), MaxEntryBytes)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read notebook file: %w", err)
	}
	var wrapper map[string]any
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid notebook file: %w", err)
	}
	// Unlike a notebook store's Get or Save, which stay permissive about a bare
	// notebook object with no "kind", an import requires the file to be
	// self-identified: without it a JSON file that happens to parse would either
	// fail deep inside Coerce with a confusing error or, pathologically, import
	// garbage.
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
	// The same checks a notebook in a bundle gets (CheckNotebook).
	inner, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("invalid notebook file: %w", err)
	}
	if _, _, err := CheckNotebook(inner); err != nil {
		return nil, err
	}
	return wrapper, nil
}

// notebookExportFile is the shape WriteNotebookFile writes and ReadNotebookFile
// reads: {kind, schemaVersion, notebook} with the product's file kind
// (notebooks.FileKind).
type notebookExportFile struct {
	Kind          string             `json:"kind"`
	SchemaVersion int                `json:"schemaVersion"`
	ExportedAt    string             `json:"exportedAt"`
	Notebook      notebooks.Notebook `json:"notebook"`
}

// WriteNotebookFile writes nb to path as a notebook export file (the envelope
// ReadNotebookFile reads), indented JSON, atomically. exportedAt is stamped in
// UTC (RFC 3339).
func WriteNotebookFile(path string, nb notebooks.Notebook, exportedAt time.Time) error {
	file := notebookExportFile{
		Kind: notebooks.FileKind, SchemaVersion: notebooks.SchemaVersion,
		ExportedAt: exportedAt.UTC().Format(time.RFC3339), Notebook: nb,
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal notebook: %w", err)
	}
	if err := filestore.WriteFileAtomic(path, b, 0o644); err != nil {
		return fmt.Errorf("failed to write notebook file: %w", err)
	}
	return nil
}
