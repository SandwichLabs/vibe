package notebooks

import (
	"encoding/json"
	"fmt"
)

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
