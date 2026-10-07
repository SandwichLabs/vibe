package queries

import (
	"fmt"
)

// notFoundError is what the stores' Get/Update/Delete return for an unknown
// id; it satisfies errors.Is(err, ErrNotFound).
type notFoundError struct{ id string }

func (e *notFoundError) Error() string        { return fmt.Sprintf("query %q not found", e.id) }
func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// Query represents a named, reusable query.
type Query struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	SQL        string  `json:"sql"`
	Engine     string  `json:"engine"` // informational; the file store does not record it
	Parameters []Param `json:"parameters"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`

	// Description and Connection are stored in the query file's header.
	// Connection names the connection whose catalog is the default for the
	// query's unqualified table names; "" means the workspace default.
	Description string `json:"description,omitempty"`
	Connection  string `json:"connection,omitempty"`
	// Rev is the file store's revision of the query as loaded; hand it back on
	// Update and a write against a stale copy returns ErrConflict.
	Rev string `json:"rev,omitempty"`
	// Error is set by the file store for a query file that could not be read
	// (malformed header, too large, unusable name): it still lists, with the
	// file stem as title, so it can be found and fixed. Such a query cannot be
	// saved over until the file is fixed.
	Error string `json:"error,omitempty"`
}

// Param defines a parameter expected by a query.
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
}
