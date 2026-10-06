//go:build unix

package filelock

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireContendRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	l, err := Acquire(p, []byte("one"))
	require.NoError(t, err)

	fi, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	_, err = Acquire(p, []byte("two"))
	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.ErrorIs(t, err, ErrHeld)

	held2, err := Probe(p)
	require.NoError(t, err)
	assert.True(t, held2)

	require.NoError(t, l.Release())
	require.NoError(t, l.Release(), "release is idempotent")
	_, statErr := os.Stat(p)
	assert.True(t, os.IsNotExist(statErr))

	held2, err = Probe(p)
	require.NoError(t, err)
	assert.False(t, held2)
}

func TestProbeMissingAndLeftover(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	held, err := Probe(p)
	require.NoError(t, err)
	assert.False(t, held)

	// A file left by a crashed holder is not a held lock.
	require.NoError(t, os.WriteFile(p, []byte("stale"), 0o644))
	held, err = Probe(p)
	require.NoError(t, err)
	assert.False(t, held)

	l, err := Acquire(p, []byte("fresh"))
	require.NoError(t, err)
	defer l.Release() //nolint:errcheck
	fi, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "a leftover file's loose mode is tightened on acquire")
}

func TestWriteShrinksAndGrows(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	l, err := Acquire(p, []byte("a long first payload"))
	require.NoError(t, err)
	defer l.Release() //nolint:errcheck
	require.NoError(t, l.Write([]byte("short")))
	got, _ := os.ReadFile(p)
	assert.Equal(t, "short", string(got))
	require.NoError(t, l.Write([]byte("a much longer second payload")))
	got, _ = os.ReadFile(p)
	assert.Equal(t, "a much longer second payload", string(got))
}

func TestAcquireRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(victim, []byte("precious"), 0o644))
	p := filepath.Join(dir, "x.lock")
	require.NoError(t, os.Symlink(victim, p))

	_, err := Acquire(p, []byte("payload"))
	require.ErrorIs(t, err, ErrUnsafe)
	got, _ := os.ReadFile(victim)
	assert.Equal(t, "precious", string(got), "never written through a symlink")
	fi, _ := os.Stat(victim)
	assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm(), "never chmodded through a symlink")

	_, err = Probe(p)
	assert.ErrorIs(t, err, ErrUnsafe)

	// A dangling symlink must not be created through either.
	dangling := filepath.Join(dir, "new-target")
	p2 := filepath.Join(dir, "y.lock")
	require.NoError(t, os.Symlink(dangling, p2))
	_, err = Acquire(p2, []byte("payload"))
	require.ErrorIs(t, err, ErrUnsafe)
	_, statErr := os.Lstat(dangling)
	assert.True(t, os.IsNotExist(statErr))
}

func TestAcquireRefusesHardLink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(victim, []byte("precious"), 0o644))
	p := filepath.Join(dir, "x.lock")
	require.NoError(t, os.Link(victim, p))
	_, err := Acquire(p, []byte("payload"))
	require.ErrorIs(t, err, ErrUnsafe)
	got, _ := os.ReadFile(victim)
	assert.Equal(t, "precious", string(got))
}

func TestFifoNeitherHangsNorIsLocked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	require.NoError(t, syscall.Mkfifo(p, 0o600))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := Acquire(p, []byte("x"))
		assert.ErrorIs(t, err, ErrUnsafe)
		_, err = Probe(p)
		assert.ErrorIs(t, err, ErrUnsafe)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO at the lock path hung the lock")
	}
}

func TestProbeRefusesAnotherUsersFile(t *testing.T) {
	const rootOwned = "/etc/hostname"
	st, err := os.Stat(rootOwned)
	sys, ok := st.Sys().(*syscall.Stat_t)
	if err != nil || !ok || sys.Uid == uint32(os.Getuid()) {
		t.Skip("no file owned by another user to test with")
	}
	_, err = Probe(rootOwned)
	assert.ErrorIs(t, err, ErrUnsafe)
	_, err = Acquire(rootOwned, []byte("x"))
	assert.Error(t, err)
}

func TestAcquireAtIsRelativeToTheDirectoryDescriptor(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "local")
	require.NoError(t, os.Mkdir(real, 0o700))
	dir, err := os.Open(real)
	require.NoError(t, err)
	defer dir.Close() //nolint:errcheck

	// After the directory was checked and opened, its path is swapped for a
	// symlink to somewhere else (a racing clone). The lock must still be
	// created in, and removed from, the directory we hold.
	moved := filepath.Join(base, "moved")
	require.NoError(t, os.Rename(real, moved))
	victim := filepath.Join(base, "victim")
	require.NoError(t, os.Mkdir(victim, 0o755))
	require.NoError(t, os.Symlink(victim, real))

	l, err := AcquireAt(dir, "hub.lock", []byte("x"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(moved, "hub.lock"))
	assert.NoError(t, err, "the lock is in the directory we opened")
	entries, _ := os.ReadDir(victim)
	assert.Empty(t, entries, "nothing was created through the swapped path")

	held, err := ProbeAt(dir, "hub.lock")
	require.NoError(t, err)
	assert.True(t, held)
	require.NoError(t, l.Release())
	_, err = os.Stat(filepath.Join(moved, "hub.lock"))
	assert.True(t, os.IsNotExist(err))
}
