package container

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver
)

// Container represents an open .vibe file with both SQLCipher and DuckDB connections.
type Container struct {
	path     string
	sqliteDB *sql.DB
	duckDB   *duckDBState
	logger   *slog.Logger
	readOnly bool
}

// Create creates a new .vibe file at the given path with the specified options.
// The passphrase option is required.
func Create(path string, opts ...Option) (*Container, error) {
	o := applyOptions(opts)

	if o.Passphrase == "" {
		return nil, fmt.Errorf("failed to create container: passphrase is required")
	}

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

	// Seed example data if requested.
	if o.ExampleData {
		if err := seedExampleData(db); err != nil {
			db.Close() //nolint:errcheck
			os.Remove(path)
			return nil, fmt.Errorf("failed to seed example data: %w", err)
		}
	}

	// Materialize DuckDB (creates empty database for new files).
	duckState, err := materializeDuckDB(db)
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
	}

	// Seed a bundled starter template if requested (P6.6). This writes user
	// tables + sample data into DuckDB and Access-style metadata into SQLCipher.
	if o.Template != "" {
		if err := applyTemplate(c, o.Template); err != nil {
			c.Close()       //nolint:errcheck
			os.Remove(path) //nolint:errcheck
			return nil, fmt.Errorf("failed to apply template: %w", err)
		}
	}

	o.Logger.Info("container created successfully", "path", path)

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
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to open container: file not found at %s", path)
	}

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
	duckState, err := materializeDuckDB(db)
	if err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to materialize DuckDB: %w", err)
	}

	// Retrofit auto-number sequences onto bare integer PKs created before the
	// sequence-backed-PK fix, so old files accept id-less form inserts too.
	if !o.ReadOnly {
		backfillAutoNumberPKs(duckState.db, o.Logger)
	}

	o.Logger.Info("container opened successfully", "path", path)

	return &Container{
		path:     path,
		sqliteDB: db,
		duckDB:   duckState,
		logger:   o.Logger,
		readOnly: o.ReadOnly,
	}, nil
}

// SQLiteDB returns the underlying SQLCipher database connection.
func (c *Container) SQLiteDB() *sql.DB {
	return c.sqliteDB
}

// DuckDBDB returns the underlying DuckDB database connection.
func (c *Container) DuckDBDB() *sql.DB {
	if c.duckDB == nil {
		return nil
	}
	return c.duckDB.db
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

// Save checkpoints DuckDB and writes the blob back into the SQLCipher container.
func (c *Container) Save() error {
	if c.readOnly {
		return fmt.Errorf("failed to save: container is read-only")
	}

	c.SetMeta("updated_at", time.Now().UTC().Format(time.RFC3339)) //nolint:errcheck

	c.logger.Info("saving container", "path", c.path)

	if err := checkpointDuckDB(c.duckDB, c.sqliteDB); err != nil {
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

	if firstErr != nil {
		return fmt.Errorf("failed to close container: %w", firstErr)
	}

	c.logger.Info("container closed successfully", "path", c.path)
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
