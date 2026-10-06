package notebooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// DB is the minimal *sql.DB / *sql.Tx surface this package needs. Every
// storage function below takes DB rather than a concrete *sql.DB so a
// caller that needs atomicity across more than one statement (the page
// migration: insert the notebook, then stamp the source page's
// migrated_to, as one unit) can pass a *sql.Tx instead of the whole
// database — both already satisfy this interface, so no adapter is needed.
type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// Summary is a notebook list entry (ListNotebooks).
type Summary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CellCount int    `json:"cellCount"`
	UpdatedAt string `json:"updatedAt"`
	// Error is set by the file store for a notebook file that failed to parse:
	// it still lists (so it can be found and fixed) with the file stem as title.
	Error string `json:"error,omitempty"`
}

// List returns every notebook in the container, newest-updated first.
func List(db DB) ([]Summary, error) {
	rows, err := db.Query(`SELECT id, title, cells, updated_at FROM _vibe_notebooks ORDER BY updated_at DESC, title`)
	if err != nil {
		return nil, fmt.Errorf("failed to list notebooks: %w", err)
	}
	defer rows.Close()

	out := make([]Summary, 0)
	for rows.Next() {
		var id, title, cellsJSON, updatedAt string
		if err := rows.Scan(&id, &title, &cellsJSON, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan notebook: %w", err)
		}
		var cells []json.RawMessage
		_ = json.Unmarshal([]byte(cellsJSON), &cells) // best-effort count; a malformed row still lists with count 0
		out = append(out, Summary{ID: id, Title: title, CellCount: len(cells), UpdatedAt: updatedAt})
	}
	return out, rows.Err()
}

// rowToRaw builds the generic map Coerce expects from one _vibe_notebooks row.
func rowToRaw(id, title, cellsJSON, updatedAt string) (map[string]any, error) {
	var cells any
	if err := json.Unmarshal([]byte(cellsJSON), &cells); err != nil {
		return nil, fmt.Errorf("stored cells JSON is corrupt: %w", err)
	}
	return map[string]any{
		"schemaVersion": float64(SchemaVersion),
		"id":            id,
		"title":         title,
		"cells":         cells,
		"updatedAt":     updatedAt,
	}, nil
}

// Get loads one notebook by id and rebuilds it through Coerce — every load
// goes through the whitelist rebuild, even a row this same package wrote,
// since a shared .vibe file may have been edited by a future or buggy Vibe
// version, or by hand.
func Get(db DB, id string) (Notebook, []string, error) {
	var title, cellsJSON, updatedAt string
	err := db.QueryRow(`SELECT title, cells, updated_at FROM _vibe_notebooks WHERE id = ?`, id).
		Scan(&title, &cellsJSON, &updatedAt)
	if err != nil {
		return Notebook{}, nil, fmt.Errorf("failed to get notebook %q: %w", id, err)
	}
	raw, err := rowToRaw(id, title, cellsJSON, updatedAt)
	if err != nil {
		return Notebook{}, nil, err
	}
	return Coerce(raw)
}

// Save coerces raw input (a JSON string/bytes from the frontend, or an
// already-decoded value) into a Notebook and upserts it. Returns the saved
// notebook (with a fresh updatedAt) and any coercion warnings. Create a new
// notebook with Empty() and pass it here for the first save.
func Save(db DB, raw any) (Notebook, []string, error) {
	nb, warnings, err := Coerce(raw)
	if err != nil {
		return Notebook{}, warnings, err
	}

	cellsJSON, err := json.Marshal(nb.Cells)
	if err != nil {
		return Notebook{}, warnings, fmt.Errorf("failed to marshal cells: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	nb.UpdatedAt = now

	_, err = db.Exec(
		`INSERT INTO _vibe_notebooks (id, title, cells, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   title = excluded.title,
		   cells = excluded.cells,
		   updated_at = excluded.updated_at`,
		nb.ID, nb.Title, string(cellsJSON), now, now,
	)
	if err != nil {
		return Notebook{}, warnings, fmt.Errorf("failed to save notebook %q: %w", nb.ID, err)
	}
	return nb, warnings, nil
}

// Create makes a brand-new, empty notebook with the given title and persists
// it, returning its id.
func Create(db DB, title string) (Notebook, error) {
	nb := Empty(title)
	saved, _, err := Save(db, notebookToRaw(nb))
	if err != nil {
		return Notebook{}, err
	}
	return saved, nil
}

// notebookToRaw round-trips a Notebook through its own JSON encoding, the
// simplest way to hand a Notebook back into Coerce's `raw any` input without
// a second, parallel "trusted" construction path.
func notebookToRaw(nb Notebook) any {
	b, err := json.Marshal(nb)
	if err != nil {
		// Notebook.MarshalJSON only fails for a cell of unknown Type, which
		// this package itself never constructs — Empty/NewCell/Coerce always
		// set a valid Type. Treat as unreachable rather than plumbing a
		// second error return through every caller.
		panic(fmt.Sprintf("notebooks: unreachable marshal failure: %v", err))
	}
	return b
}

// Rename updates a notebook's title without touching its cells.
func Rename(db DB, id, title string) error {
	if title == "" {
		title = "Untitled notebook"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE _vibe_notebooks SET title = ?, updated_at = ? WHERE id = ?`, title, now, id)
	if err != nil {
		return fmt.Errorf("failed to rename notebook %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("notebook %q not found", id)
	}
	return nil
}

// Delete removes a notebook by id. Deleting a non-existent id is a no-op,
// matching delete_page/delete_record semantics elsewhere in this codebase.
func Delete(db DB, id string) error {
	if _, err := db.Exec(`DELETE FROM _vibe_notebooks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete notebook %q: %w", id, err)
	}
	return nil
}

// Insert persists a Notebook that was built directly (e.g. by the page
// migration), bypassing Save's raw-decode step since the caller already has
// a well-typed Notebook it wants stored as-is. It still runs the value
// through Coerce (via notebookToRaw + Save) so a migration bug can never
// smuggle an invalid cell into storage.
func Insert(db DB, nb Notebook) (Notebook, []string, error) {
	return Save(db, notebookToRaw(nb))
}
