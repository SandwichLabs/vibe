package container

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver

	"github.com/sandwich-labs/vibe-app/internal/llm/skills"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// Container represents an open .vibe file with both SQLCipher and DuckDB connections.
type Container struct {
	path     string
	sqliteDB *sql.DB
	duckDB   *duckDBState
	logger   *slog.Logger
	readOnly bool
	lock     *fileLock

	// dirty tracks whether DuckDB user data (table rows/schema) has changed
	// since the last successful Save. It is UI signal only, never a gate on
	// any exit path: SQLCipher-side content (notebooks, queries, skills,
	// meta) is write-through and never marks this, so callers that write
	// DuckDB user data are the ones responsible for calling MarkDirty.
	dirty atomic.Bool
}

// Create creates a new .vibe file at the given path with the specified options.
// The passphrase option is required.
func Create(path string, opts ...Option) (*Container, error) {
	o := applyOptions(opts)

	if o.Passphrase == "" {
		return nil, fmt.Errorf("failed to create container: passphrase is required")
	}

	// Take the writer lock before anything else (#80): a refused create
	// costs nothing. This also covers the vanishingly unlikely case of two
	// Creates racing on the same path.
	lk, err := acquireLock(path, o.LockApp)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			lk.release() //nolint:errcheck
		}
	}()

	// Ensure the file does not already exist.
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("failed to create container: file already exists at %s", path)
	}

	o.Logger.Info("creating new .vibe container", "path", path)

	// Open SQLCipher database (creates the file).
	db, err := openSQLCipher(path, o.Passphrase)
	if err != nil {
		os.Remove(path)
		return nil, err
	}

	// Run schema migrations.
	if err := runMigrations(db); err != nil {
		db.Close() //nolint:errcheck
		os.Remove(path)
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Seed default metadata.
	if err := seedMeta(db, o.Title, o.Description); err != nil {
		db.Close() //nolint:errcheck
		os.Remove(path)
		return nil, fmt.Errorf("failed to seed metadata: %w", err)
	}

	// Materialize DuckDB (creates empty database for new files).
	duckState, err := materializeDuckDB(db, o.Logger)
	if err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to materialize DuckDB: %w", err)
	}

	c := &Container{
		path:     path,
		sqliteDB: db,
		duckDB:   duckState,
		logger:   o.Logger,
		readOnly: false,
		lock:     lk,
	}

	// Seed a bundled starter template if requested (P6.6). This writes user
	// tables + sample data into DuckDB and Access-style metadata into
	// SQLCipher, then saves once: applyTemplate's writes never reach the
	// encrypted file on their own (DuckDB user data only reaches it on
	// Save), so without this a crash right after Create keeps only the
	// SQLCipher-side notebook metadata a template writes and loses the
	// tables themselves.
	if o.Template != "" {
		err := applyTemplate(c, o.Template)
		if err == nil {
			err = c.Save()
		}
		if err != nil {
			c.Close() //nolint:errcheck // releases lk; mark ok so our own defer doesn't double-release it.
			ok = true
			os.Remove(path) //nolint:errcheck
			return nil, fmt.Errorf("failed to apply template: %w", err)
		}
	}

	o.Logger.Info("container created successfully", "path", path)

	ok = true
	return c, nil
}

// Open opens an existing .vibe file at the given path.
// The passphrase option is required.
func Open(path string, opts ...Option) (*Container, error) {
	o := applyOptions(opts)

	if o.Passphrase == "" {
		return nil, fmt.Errorf("failed to open container: passphrase is required")
	}

	// Ensure the file exists.
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to open container: file not found at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open container: %w", err)
	}
	// #94: a zero-byte file is never a valid .vibe container. Catch it
	// before any SQLCipher open, so it is never accidentally initialized.
	if fi.Size() == 0 {
		return nil, fmt.Errorf("failed to open container: %s is empty, not a .vibe file", filepath.Base(path))
	}

	// Take the writer lock (#80) before any SQLCipher/DuckDB work, so a
	// refused open costs nothing. Read-only opens (info/export/dump/verify,
	// `mcp serve --read-only-file`) take no lock and may proceed alongside
	// a writer, seeing the last saved state.
	var lk *fileLock
	if !o.ReadOnly {
		lk, err = acquireLock(path, o.LockApp)
		if err != nil {
			return nil, fmt.Errorf("failed to open container: %w", err)
		}
	}
	ok := false
	defer func() {
		if !ok {
			lk.release() //nolint:errcheck
		}
	}()

	o.Logger.Info("opening .vibe container", "path", path)

	// Open SQLCipher database with passphrase in DSN.
	db, err := openSQLCipher(path, o.Passphrase)
	if err != nil {
		return nil, err
	}

	// Verify the passphrase is correct.
	if err := verifyPassphrase(db); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to open container: %w", err)
	}

	// Ensure schema is up-to-date.
	if err := runMigrations(db); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Materialize DuckDB from stored blob.
	duckState, err := openDuckDBSession(db, o.ReadOnly, o.Logger)
	if err != nil {
		db.Close() //nolint:errcheck
		return nil, err
	}

	o.Logger.Info("container opened successfully", "path", path)

	ok = true
	return &Container{
		path:     path,
		sqliteDB: db,
		duckDB:   duckState,
		logger:   o.Logger,
		readOnly: o.ReadOnly,
		lock:     lk,
	}, nil
}

// SQLiteDB returns the underlying SQLCipher database connection.
func (c *Container) SQLiteDB() *sql.DB {
	return c.sqliteDB
}

// Notebooks returns the notebook store over _vibe_notebooks (the legacy
// container's implementation of notebooks.Store).
func (c *Container) Notebooks() notebooks.Store {
	if c.sqliteDB == nil {
		return notebooks.NewSQLiteStore(nil)
	}
	return notebooks.NewSQLiteStore(c.sqliteDB)
}

// Queries returns the saved-query store over _vibe_queries.
func (c *Container) Queries() queries.Store {
	return queries.NewRegistry(c.sqliteDB, c.logger)
}

// Skills returns the skill store over _vibe_skills.
func (c *Container) Skills() skills.Store {
	if c.sqliteDB == nil {
		return skills.NewSQLiteStore(nil)
	}
	return skills.NewSQLiteStore(c.sqliteDB)
}

// DuckDBDB returns the underlying DuckDB database connection.
func (c *Container) DuckDBDB() *sql.DB {
	if c.duckDB == nil {
		return nil
	}
	return c.duckDB.db
}

// Session returns the DuckDB session over the container's data: the hub every
// caller should query and introspect through. Its default catalog is the
// materialised file (Session.DefaultCatalog()). Like DuckDBDB(), it changes
// after Revert (the file is re-materialised), so callers must not cache it
// across a Revert. Returns nil when no DuckDB session is live.
func (c *Container) Session() session.Session {
	if c.duckDB == nil {
		return nil
	}
	return c.duckDB.sess
}

// DuckDBTempDir returns the filesystem path of the temp directory backing the
// materialized DuckDB database for this session (P6.8). Returns "" if there is
// no live DuckDB session.
func (c *Container) DuckDBTempDir() string {
	if c.duckDB == nil {
		return ""
	}
	return c.duckDB.tempDir
}

// MarkDirty records that DuckDB user data (table rows/schema) may have
// changed since the last successful Save. Callers that write DuckDB — not
// SQLCipher rows, which are write-through and never call this — call it
// after the write is attempted. It returns true only the call that flips
// clean to dirty, so a caller can use it to fire a "just became dirty"
// notification exactly once per clean/dirty edge. A read-only container is
// never dirty (Save is refused on it anyway), so this is a no-op for one.
func (c *Container) MarkDirty() bool {
	if c.readOnly {
		return false
	}
	return !c.dirty.Swap(true)
}

// Dirty reports whether DuckDB user data has changed since the last
// successful Save. UI signal only — see the dirty field's doc comment.
func (c *Container) Dirty() bool {
	return c.dirty.Load()
}

// Save checkpoints DuckDB and writes the blob back into the SQLCipher container.
func (c *Container) Save() error {
	if c.readOnly {
		return fmt.Errorf("failed to save: container is read-only")
	}

	c.SetMeta("updated_at", time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	c.logger.Info("saving container", "path", c.path)

	// Clear the dirty flag BEFORE the checkpoint, not after: a write that
	// lands (from a concurrent caller, or the agent) while the checkpoint is
	// running must leave the file dirty again rather than being silently
	// cleared once this Save happens to finish.
	was := c.dirty.Swap(false)

	if err := checkpointDuckDB(c.duckDB, c.sqliteDB); err != nil {
		if was {
			c.dirty.Store(true)
		}
		return fmt.Errorf("failed to save container: %w", err)
	}

	c.logger.Info("container saved successfully", "path", c.path)
	return nil
}

// Close closes both database connections and cleans up temporary files.
func (c *Container) Close() error {
	c.logger.Info("closing container", "path", c.path)

	var firstErr error

	if err := cleanupDuckDB(c.duckDB); err != nil {
		firstErr = fmt.Errorf("failed to cleanup DuckDB: %w", err)
	}
	c.duckDB = nil

	if c.sqliteDB != nil {
		if err := c.sqliteDB.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close SQLite: %w", err)
		}
		c.sqliteDB = nil
	}

	// Release the writer lock last (#80), so no other process can open the
	// file for writing until our own SQLite close above has finished.
	if err := c.lock.release(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("failed to release lock: %w", err)
	}
	c.lock = nil

	if firstErr != nil {
		return fmt.Errorf("failed to close container: %w", firstErr)
	}

	c.logger.Info("container closed successfully", "path", c.path)
	return nil
}

// Revert discards every DuckDB change since the last successful Save (#98):
// it materializes a fresh session copy from the saved blob, swaps it in, and
// then closes and zeroes the old copy. The SQLCipher connection and the #80
// writer lock stay held throughout, so no passphrase is needed and no other
// process can take the file in between. SQLCipher-side content is
// write-through and untouched. A file that has never been saved reverts to
// an empty DuckDB (the state of its last save).
//
// DuckDBDB() returns a NEW *sql.DB afterwards and the old one is closed;
// callers that cached it (an engine.Manager) must rebuild. Not safe for
// concurrent use with other calls on c: the caller serializes (Studio holds
// a.mu.Lock). On error c's current session is left exactly as it was.
//
// Materialize-before-cleanup means two plaintext copies exist briefly, both
// in 0700 dirs. That is acceptable and is what makes failure atomic.
func (c *Container) Revert() error {
	if c.readOnly {
		return fmt.Errorf("failed to revert: container is read-only")
	}
	if c.sqliteDB == nil || c.duckDB == nil {
		return fmt.Errorf("failed to revert: container is closed")
	}

	c.logger.Info("reverting container to last save", "path", c.path)

	fresh, err := openDuckDBSession(c.sqliteDB, false, c.logger)
	if err != nil {
		return fmt.Errorf("failed to revert: %w", err)
	}

	old := c.duckDB
	c.duckDB = fresh
	c.dirty.Store(false)

	if err := cleanupDuckDB(old); err != nil {
		c.logger.Warn("failed to clean up previous DuckDB session after revert", "error", err)
	}

	c.logger.Info("container reverted successfully", "path", c.path)
	return nil
}

// Meta returns a metadata value by key from _vibe_meta.
func (c *Container) Meta(key string) (string, error) {
	var value string
	err := c.sqliteDB.QueryRow("SELECT value FROM _vibe_meta WHERE key = ?;", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("failed to get meta: key %q not found", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get meta key %q: %w", key, err)
	}
	return value, nil
}

// SetMeta sets a metadata value by key in _vibe_meta.
func (c *Container) SetMeta(key, value string) error {
	if c.readOnly {
		return fmt.Errorf("failed to set meta: container is read-only")
	}

	_, err := c.sqliteDB.Exec(
		`INSERT INTO _vibe_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value;`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set meta key %q: %w", key, err)
	}
	return nil
}

// Path returns the filesystem path to the .vibe file.
func (c *Container) Path() string {
	return c.path
}

// IsReadOnly returns true if the container was opened in read-only mode.
func (c *Container) IsReadOnly() bool {
	return c.readOnly
}
