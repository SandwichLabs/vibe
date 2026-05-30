package container

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2" // DuckDB driver
)

// duckDBTempPrefix is the prefix used for all DuckDB session temp directories.
// Orphan recovery (SweepOrphanedTemp) relies on this prefix plus the embedded
// owning PID to attribute and reclaim temp dirs left behind by dead processes.
const duckDBTempPrefix = "vibe-duckdb-"

// orphanMinAge is the minimum age a temp dir must have before a "live" PID
// liveness result will keep it. This mitigates PID reuse: a recently created
// dir whose PID happens to be alive is almost certainly a genuine live session,
// whereas a very old dir whose PID is alive is more likely a recycled PID. We
// only ever *delete* dirs whose PID is dead, so this guard never causes data
// loss — it only narrows the (rare) window in which a recycled PID protects a
// genuinely orphaned dir. The guard is intentionally conservative.
const orphanMinAge = 24 * time.Hour

// StartupSweep runs the orphan-temp recovery once at application startup. It is
// the wired entrypoint for both the CLI (cmd/root.go) and the Studio app
// (studio/app.go): orphaned temp dirs from crashed processes are reclaimed
// here. Failure is logged, never fatal — recovery is best-effort and must never
// block the application from starting. A nil logger is tolerated.
func StartupSweep(logger *slog.Logger) {
	if err := SweepOrphanedTemp(); err != nil && logger != nil {
		logger.Warn("orphaned DuckDB temp sweep encountered errors", "error", err)
	}
}

// duckDBState holds the DuckDB connection and its temporary file path.
type duckDBState struct {
	db       *sql.DB
	tempPath string
	tempDir  string
}

// materializeDuckDB reads the DuckDB blob from SQLCipher and opens it as a DuckDB database.
// If no blob exists (new file), it initializes an empty DuckDB database.
func materializeDuckDB(sqliteDB *sql.DB) (*duckDBState, error) {
	// Tag the temp dir name with the owning PID so an orphaned dir (left behind
	// by a crashed process) can be attributed and reclaimed by SweepOrphanedTemp.
	pattern := fmt.Sprintf("%s%d-*", duckDBTempPrefix, os.Getpid())
	tempDir, err := os.MkdirTemp("", pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory for DuckDB: %w", err)
	}

	// Lock down permissions: only the owner may read/traverse the materialized
	// (decrypted) DuckDB data. MkdirTemp creates 0700 on most platforms already,
	// but enforce it explicitly to harden against a permissive umask.
	if err := os.Chmod(tempDir, 0700); err != nil {
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("failed to secure DuckDB temp directory: %w", err)
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

// SweepOrphanedTemp removes DuckDB temp directories left behind by dead
// processes (P6.8 orphan recovery). It scans the system temp root for dirs
// matching the "vibe-duckdb-<pid>-*" naming scheme and removes any whose owning
// PID is no longer a running process. Temp dirs belonging to live processes
// (including this one's active sessions) are left untouched.
func SweepOrphanedTemp() error {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("failed to read temp root for orphan sweep: %w", err)
	}

	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, duckDBTempPrefix) {
			continue
		}

		pid, ok := pidFromTempName(name)
		if !ok {
			// Unattributable name; leave it alone rather than risk deleting
			// something we don't understand.
			continue
		}

		full := filepath.Join(root, name)

		if processAlive(pid) {
			// Mitigate PID reuse: a live PID normally means an active session, so
			// we keep the dir. But a recycled PID could make a genuinely orphaned
			// dir look live. If the dir is older than orphanMinAge, the "live" PID
			// is more likely a coincidental reuse than a real session, so we treat
			// it as orphaned. (We never delete on a live-and-recent dir.)
			if info, err := entry.Info(); err != nil || time.Since(info.ModTime()) < orphanMinAge {
				continue
			}
		}

		if err := os.RemoveAll(full); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to sweep orphaned DuckDB temp dir %q: %w", name, err)
		}
	}

	return firstErr
}

// pidFromTempName extracts the owning PID from a temp dir name of the form
// "vibe-duckdb-<pid>-<random>". Returns false if the name does not parse.
func pidFromTempName(name string) (int, bool) {
	rest := strings.TrimPrefix(name, duckDBTempPrefix)
	idx := strings.IndexByte(rest, '-')
	if idx <= 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(rest[:idx])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// processAlive reports whether a process with the given PID is currently
// running. It uses signal 0, which performs error checking without actually
// delivering a signal.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the process exists but we lack permission to signal it.
	return err == syscall.EPERM
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

	// Best-effort zero the decrypted temp DB file before removal so the
	// plaintext analytical data does not linger in freed disk blocks.
	if state.tempPath != "" {
		zeroFile(state.tempPath) //nolint:errcheck
	}

	if state.tempDir != "" {
		if err := os.RemoveAll(state.tempDir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to remove DuckDB temp directory: %w", err)
		}
	}

	return firstErr
}

// zeroFile overwrites the contents of the file at path with zero bytes. This is
// a best-effort defense to scrub decrypted analytical data from the temp file
// before it is unlinked; it does not guarantee secure erasure on copy-on-write
// or wear-leveled storage.
func zeroFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err //nolint:wrapcheck // best-effort, caller ignores
	}
	size := info.Size()
	if size <= 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return err //nolint:wrapcheck
	}
	defer f.Close() //nolint:errcheck

	zeros := make([]byte, 32*1024)
	var written int64
	for written < size {
		n := int64(len(zeros))
		if remaining := size - written; remaining < n {
			n = remaining
		}
		w, werr := f.Write(zeros[:n])
		if werr != nil {
			return werr //nolint:wrapcheck
		}
		written += int64(w)
	}

	return f.Sync() //nolint:wrapcheck
}
