package container

import (
	"database/sql"

	"github.com/sandwich-labs/vibe-app/internal/llm/skills"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// Store defines the interface that Container implements, for use in testing
// and dependency injection by downstream packages.
type Store interface {
	// SQLiteDB returns the underlying SQLCipher database connection.
	SQLiteDB() *sql.DB

	// DuckDBDB returns the underlying DuckDB database connection.
	DuckDBDB() *sql.DB

	// Notebooks, Queries and Skills are the legacy container's content
	// stores (the workspace provides file-backed ones, #109).
	Notebooks() notebooks.Store
	Queries() queries.Store
	Skills() skills.Store

	// Session returns the DuckDB session (catalog-aware hub) over DuckDBDB.
	Session() session.Session

	// Save checkpoints DuckDB and writes the blob back to SQLCipher.
	Save() error

	// Close closes both databases and cleans up temporary files.
	Close() error

	// Meta returns a metadata value by key from _vibe_meta.
	Meta(key string) (string, error)

	// SetMeta sets a metadata value by key in _vibe_meta.
	SetMeta(key, value string) error

	// Path returns the filesystem path to the .vibe file.
	Path() string

	// IsReadOnly returns true if the container was opened in read-only mode.
	IsReadOnly() bool
}

// Ensure Container implements Store at compile time.
var _ Store = (*Container)(nil)
