package container

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2" // DuckDB driver
)

// duckDBState holds the DuckDB connection and its temporary file path.
type duckDBState struct {
	db       *sql.DB
	tempPath string
	tempDir  string
}

// materializeDuckDB reads the DuckDB blob from SQLCipher and opens it as a DuckDB database.
// If no blob exists (new file), it initializes an empty DuckDB database.
func materializeDuckDB(sqliteDB *sql.DB) (*duckDBState, error) {
	tempDir, err := os.MkdirTemp("", "vibe-duckdb-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory for DuckDB: %w", err)
	}

	tempPath := filepath.Join(tempDir, "analytics.duckdb")

	// Try to read existing blob from SQLCipher.
	var data []byte
	err = sqliteDB.QueryRow("SELECT data FROM _vibe_duckdb WHERE id = 1;").Scan(&data)
	if err != nil && err != sql.ErrNoRows {
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("failed to read DuckDB blob: %w", err)
	}

	if len(data) > 0 {
		// Write the blob to the temp file.
		if err := os.WriteFile(tempPath, data, 0600); err != nil {
			os.RemoveAll(tempDir) //nolint:errcheck
			return nil, fmt.Errorf("failed to write DuckDB temp file: %w", err)
		}
	}

	// Open the DuckDB database (creates new if file doesn't exist).
	ddb, err := sql.Open("duckdb", tempPath)
	if err != nil {
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("failed to open DuckDB: %w", err)
	}

	// Verify the connection works.
	if err := ddb.Ping(); err != nil {
		ddb.Close()           //nolint:errcheck
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("failed to ping DuckDB: %w", err)
	}

	return &duckDBState{
		db:       ddb,
		tempPath: tempPath,
		tempDir:  tempDir,
	}, nil
}

// checkpointDuckDB forces a DuckDB checkpoint and reads the database file back as a blob,
// then writes it into the SQLCipher _vibe_duckdb table.
func checkpointDuckDB(state *duckDBState, sqliteDB *sql.DB) error {
	// Force a checkpoint so all data is flushed to the file.
	if _, err := state.db.Exec("CHECKPOINT;"); err != nil {
		return fmt.Errorf("failed to checkpoint DuckDB: %w", err)
	}

	// Read the DuckDB file.
	data, err := os.ReadFile(state.tempPath)
	if err != nil {
		return fmt.Errorf("failed to read DuckDB file: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Upsert into _vibe_duckdb.
	_, err = sqliteDB.Exec(
		`INSERT INTO _vibe_duckdb (id, data, updated) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET data = excluded.data, updated = excluded.updated;`,
		data, now,
	)
	if err != nil {
		return fmt.Errorf("failed to write DuckDB blob to SQLCipher: %w", err)
	}

	return nil
}

// cleanupDuckDB closes the DuckDB connection and removes temporary files.
func cleanupDuckDB(state *duckDBState) error {
	if state == nil {
		return nil
	}

	var firstErr error

	if state.db != nil {
		if err := state.db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close DuckDB: %w", err)
		}
	}

	if state.tempDir != "" {
		if err := os.RemoveAll(state.tempDir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to remove DuckDB temp directory: %w", err)
		}
	}

	return firstErr
}
