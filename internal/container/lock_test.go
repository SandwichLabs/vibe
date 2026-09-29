package container

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tableExists(t *testing.T, c *Container, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, c.DuckDBDB().QueryRow(
		"SELECT count(*) FROM information_schema.tables WHERE table_name=?", name,
	).Scan(&n))
	return n > 0
}

func TestOpen_SecondWriterRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	a, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithLockApp("Vibe Studio"))
	require.NoError(t, err)
	defer a.Close() //nolint:errcheck

	_, err = Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileInUse)

	var inUse *InUseError
	require.True(t, errors.As(err, &inUse))
	require.NotNil(t, inUse.Holder)
	assert.Equal(t, "Vibe Studio", inUse.Holder.App)
	assert.Equal(t, os.Getpid(), inUse.Holder.PID)
	assert.Contains(t, err.Error(), "Vibe Studio (pid ")

	require.NoError(t, a.Close())

	b, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, b.Close())
}

// Acceptance test for the issue's repro: the second writer is refused, so
// its work is never silently lost.
func TestOpen_Issue80Repro_NoDataLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	a, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithLockApp("Vibe Studio"))
	require.NoError(t, err)
	_, err = a.DuckDBDB().Exec("CREATE TABLE studio_t AS SELECT 1 x")
	require.NoError(t, err)

	_, err = Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithLockApp("vibe mcp serve"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileInUse)

	r, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithReadOnly(true))
	require.NoError(t, err)
	assert.False(t, tableExists(t, r, "studio_t"), "reader must not see A's unsaved work")
	require.NoError(t, r.Close())

	require.NoError(t, a.Save())
	require.NoError(t, a.Close())

	reopened, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer reopened.Close() //nolint:errcheck
	assert.True(t, tableExists(t, reopened, "studio_t"))
}

func TestOpen_ReadOnlyAlongsideWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	a, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer a.Close() //nolint:errcheck

	r, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()), WithReadOnly(true))
	require.NoError(t, err)
	require.NoError(t, r.Close())

	// A's writer lock must still be held — the reader closing must not
	// release it.
	_, err = Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	assert.ErrorIs(t, err, ErrFileInUse)
}

func TestCreate_TakesLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)

	_, err = Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	assert.ErrorIs(t, err, ErrFileInUse)

	require.NoError(t, c.Close())
	_, statErr := os.Stat(lockPath(path))
	assert.True(t, os.IsNotExist(statErr), "lock file should be gone after Close")
}

func TestOpen_FailedOpenReleasesLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	_, err = Open(path, WithPassphrase("wrong-passphrase"), WithLogger(testLogger()))
	require.Error(t, err)

	_, statErr := os.Stat(lockPath(path))
	assert.True(t, os.IsNotExist(statErr), "a failed open must not leak the lock file")

	ok, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, ok.Close())
}

func TestOpen_SymlinkSharesLock(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.vibe")
	b := filepath.Join(dir, "b.vibe")
	c, err := Create(a, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NoError(t, os.Symlink(a, b))

	holder, err := Open(a, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer holder.Close() //nolint:errcheck

	_, err = Open(b, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	assert.ErrorIs(t, err, ErrFileInUse)
}

func TestOpen_ZeroByteFileRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.vibe")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	_, err := Open(path, WithPassphrase("anything"), WithLogger(testLogger()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	fi, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.EqualValues(t, 0, fi.Size())
}

// TestOpen_StaleLockAfterCrash proves the lock is never stale: a SIGKILLed
// holder (fds are CLOEXEC on Go's os.OpenFile) releases the kernel flock
// immediately, even though the lockfile itself is left on disk.
func TestOpen_StaleLockAfterCrash(t *testing.T) {
	if os.Getenv("VIBE_TEST_HOLD") != "" {
		t.Skip("helper invocation, not a real test")
	}

	path := filepath.Join(t.TempDir(), "f.vibe")
	c, err := Create(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	readyPath := path + ".ready"
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperHoldLock$")
	cmd.Env = append(os.Environ(), "VIBE_TEST_HOLD="+path, "VIBE_TEST_PASSPHRASE="+testPassphrase)
	require.NoError(t, cmd.Start())
	// Belt-and-suspenders: if this test fails/panics before the explicit
	// Kill below, the helper must not outlive it holding the lock for the
	// rest of its 30s sleep.
	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
		cmd.Wait() //nolint:errcheck
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for lock-holder helper to be ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	_, err = Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.Error(t, err)
	var inUse *InUseError
	require.True(t, errors.As(err, &inUse))
	require.NotNil(t, inUse.Holder)
	assert.Equal(t, cmd.Process.Pid, inUse.Holder.PID)

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	_, statErr := os.Stat(lockPath(path))
	assert.NoError(t, statErr, "lockfile must survive a crash (informational only)")

	reopened, err := Open(path, WithPassphrase(testPassphrase), WithLogger(testLogger()))
	require.NoError(t, err)
	defer reopened.Close() //nolint:errcheck
	h := readHolder(lockPath(path))
	require.NotNil(t, h)
	assert.Equal(t, os.Getpid(), h.PID)
}

// TestHelperHoldLock is not a real test: it's re-exec'd by
// TestOpen_StaleLockAfterCrash as a subprocess that holds a writer lock and
// sleeps, then gets SIGKILLed by the parent.
func TestHelperHoldLock(t *testing.T) {
	path := os.Getenv("VIBE_TEST_HOLD")
	if path == "" {
		t.Skip("not invoked as the lock-holder helper")
	}
	c, err := Open(path, WithPassphrase(os.Getenv("VIBE_TEST_PASSPHRASE")), WithLogger(testLogger()))
	require.NoError(t, err)
	_ = c
	require.NoError(t, os.WriteFile(path+".ready", nil, 0o600))
	time.Sleep(30 * time.Second)
}
