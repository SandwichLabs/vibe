package container

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaterializeDuckDB_TempDirPermsAndPIDName verifies P6.8 acceptance:
// the materialized DuckDB temp directory is created with 0700 permissions and
// is named so that an orphan can be attributed to a PID.
func TestMaterializeDuckDB_TempDirPermsAndPIDName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Temp Perms Test"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	tempDir := c.DuckDBTempDir()
	require.NotEmpty(t, tempDir, "container should expose its DuckDB temp dir")

	info, err := os.Stat(tempDir)
	require.NoError(t, err, "temp dir should exist while session is live")
	require.True(t, info.IsDir())

	// (a) 0700 permissions.
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm(),
		"DuckDB temp dir must be created with 0700 perms")

	// (a) PID-tagged name so orphans are attributable.
	base := filepath.Base(tempDir)
	pidStr := strconv.Itoa(os.Getpid())
	assert.Truef(t, strings.Contains(base, pidStr),
		"temp dir name %q must contain the owning PID %s", base, pidStr)
	assert.Truef(t, strings.HasPrefix(base, "vibe-duckdb-"),
		"temp dir name %q must keep the vibe-duckdb- prefix", base)
}

// TestSweepOrphanedTemp_RemovesDeadKeepsLive verifies the startup sweep removes
// temp dirs whose owning PID is no longer running while leaving a live session's
// temp dir intact.
func TestSweepOrphanedTemp_RemovesDeadKeepsLive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Sweep Test"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	liveDir := c.DuckDBTempDir()
	require.NotEmpty(t, liveDir)
	require.DirExists(t, liveDir)

	// The temp root is the parent of the live session's temp dir.
	tempRoot := filepath.Dir(liveDir)

	// Fabricate an orphan dir tagged with a PID that is not running.
	// PIDs are picked high to be very unlikely to be live; 0 is never a real
	// running user process and is rejected by signal-0 liveness checks.
	deadPID := 999999999
	orphanDir := filepath.Join(tempRoot, "vibe-duckdb-"+strconv.Itoa(deadPID)+"-stale")
	require.NoError(t, os.MkdirAll(orphanDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(orphanDir, "analytics.duckdb"), []byte("stale"), 0600))
	require.DirExists(t, orphanDir)

	// Trigger the startup sweep.
	require.NoError(t, SweepOrphanedTemp())

	// (b) orphan removed, live session untouched.
	assert.NoDirExists(t, orphanDir, "orphaned (dead-PID) temp dir should be swept")
	assert.DirExists(t, liveDir, "live session's temp dir must NOT be swept")
}

// TestClose_RemovesTempDirAndZeroesFile verifies that a clean Close removes the
// temp dir and (best-effort) that the temp DB file no longer exists.
func TestClose_RemovesTempDirAndZeroesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Close Cleanup Test"),
	)
	require.NoError(t, err)

	tempDir := c.DuckDBTempDir()
	require.NotEmpty(t, tempDir)
	tempFile := filepath.Join(tempDir, "analytics.duckdb")

	// Write something so the temp file is materialized.
	_, err = c.DuckDBDB().Exec("CREATE TABLE t (id INTEGER);")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("INSERT INTO t VALUES (1);")
	require.NoError(t, err)
	require.NoError(t, c.Save())

	require.NoError(t, c.Close())

	assert.NoDirExists(t, tempDir, "clean Close must remove the DuckDB temp dir")
	_, statErr := os.Stat(tempFile)
	assert.True(t, os.IsNotExist(statErr),
		"temp DB file should no longer exist after clean Close")
}

// TestRoundTrip_StillWorksAfterHardening guards against regressions: a full
// open -> write -> Save -> Close -> reopen cycle must still round-trip data.
func TestRoundTrip_StillWorksAfterHardening(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vibe")

	c, err := Create(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
		WithTitle("Round Trip"),
	)
	require.NoError(t, err)

	_, err = c.DuckDBDB().Exec("CREATE TABLE widgets (id INTEGER, name TEXT);")
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec("INSERT INTO widgets VALUES (7, 'sprocket');")
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path,
		WithPassphrase(testPassphrase),
		WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	var id int
	var name string
	err = c2.DuckDBDB().QueryRow("SELECT id, name FROM widgets WHERE id = 7;").Scan(&id, &name)
	require.NoError(t, err)
	assert.Equal(t, 7, id)
	assert.Equal(t, "sprocket", name)
}

// TestTempFileMode_NewAndExisting: the materialized DuckDB temp file is
// 0600 both for a brand-new file (DuckDB creates the file itself, with the
// process umask) and for one materialized from an existing blob (written
// via os.WriteFile with an explicit mode) — see materializeDuckDB's chmod.
func TestTempFileMode_NewAndExisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "mode.vibe")

	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)

	tempDir := c.DuckDBTempDir()
	tempPath := filepath.Join(tempDir, "analytics.duckdb")
	info, err := os.Stat(tempPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "a brand-new DuckDB temp file must be 0600")

	_, err = c.DuckDBDB().Exec(`CREATE TABLE t (id INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	tempPath2 := filepath.Join(c2.DuckDBTempDir(), "analytics.duckdb")
	info2, err := os.Stat(tempPath2)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info2.Mode().Perm(), "a temp file materialized from an existing blob must also be 0600")
}
