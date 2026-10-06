package session

import (
	"bufio"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper process: holds a .duckdb file attached (rw or ro) until stdin closes.
// Run via TestSpikeFileLockCrossProcess; a no-op in normal test runs.
func TestSpikeHelperHoldFile(t *testing.T) {
	path, mode := os.Getenv("SPIKE_HOLD_PATH"), os.Getenv("SPIKE_HOLD_MODE")
	if path == "" {
		t.Skip("helper process only")
	}
	db, err := tryOpen("")
	if err != nil {
		os.Stdout.WriteString("ERR " + err.Error() + "\n")
		return
	}
	defer db.Close()
	q := "ATTACH '" + path + "' AS held"
	if mode == "ro" {
		q += " (READ_ONLY)"
	}
	if _, err := db.Exec(q); err != nil {
		os.Stdout.WriteString("ERR " + err.Error() + "\n")
		return
	}
	os.Stdout.WriteString("READY\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // parent closes stdin to release
}

// startHolder launches the helper and waits for READY. Returns a stop func.
func startHolder(t *testing.T, path, mode string) func() {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpikeHelperHoldFile$", "-test.v")
	cmd.Env = append(os.Environ(), "SPIKE_HOLD_PATH="+path, "SPIKE_HOLD_MODE="+mode)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	sc := bufio.NewScanner(stdout)
	ready := false
	for sc.Scan() {
		if line := sc.Text(); line == "READY" {
			ready = true
			break
		} else if len(line) > 3 && line[:3] == "ERR" {
			t.Fatalf("helper failed: %s", line)
		}
	}
	require.True(t, ready, "helper never became ready")
	return func() { _ = stdin.Close(); _ = cmd.Wait() }
}

func attach(db *sql.DB, path, opts string) error {
	q := "ATTACH '" + path + "' AS f"
	if opts != "" {
		q += " (" + opts + ")"
	}
	_, err := db.Exec(q)
	return err
}

// Spike #107: mixed RW + RO processes on the same .duckdb file fail; RO+RO is fine.
func TestSpikeFileLockCrossProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.duckdb")
	seed := openDSN(t, "")
	require.NoError(t, attach(seed, path, ""))
	_, err := seed.Exec("CREATE TABLE f.t AS SELECT 1 AS a")
	require.NoError(t, err)
	_, err = seed.Exec("DETACH f")
	require.NoError(t, err)

	t.Run("RW holder blocks RO attach", func(t *testing.T) {
		stop := startHolder(t, path, "rw")
		defer stop()
		err := attach(openDSN(t, ""), path, "READ_ONLY")
		require.Error(t, err)
		t.Logf("RO attach while other process holds RW: %v", err)
		err = attach(openDSN(t, ""), path, "")
		require.Error(t, err)
		t.Logf("RW attach while other process holds RW: %v", err)
	})
	t.Run("RO holder blocks RW attach", func(t *testing.T) {
		stop := startHolder(t, path, "ro")
		defer stop()
		err := attach(openDSN(t, ""), path, "")
		require.Error(t, err)
		t.Logf("RW attach while other process holds RO: %v", err)
	})
	t.Run("RO holders coexist", func(t *testing.T) {
		stop := startHolder(t, path, "ro")
		defer stop()
		db := openDSN(t, "")
		require.NoError(t, attach(db, path, "READ_ONLY"))
		v, err := scalar(t.Context(), db, "SELECT a FROM f.t")
		require.NoError(t, err)
		assert.Equal(t, "1", v)
	})
	t.Run("released after holder exits", func(t *testing.T) {
		stop := startHolder(t, path, "rw")
		stop()
		require.NoError(t, attach(openDSN(t, ""), path, ""))
	})
}
