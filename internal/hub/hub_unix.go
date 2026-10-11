//go:build unix

package hub

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/SandwichLabs/vibe/internal/filelock"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

const releaseSupported = true

// checkTrusted enforces: a regular file, owned by the current uid, with no
// group or world access.
func checkTrusted(fi os.FileInfo) error {
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrUntrusted)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: cannot read its owner", ErrUntrusted)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%w: it is not owned by you", ErrUntrusted)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: it is readable by other users", ErrUntrusted)
	}
	return nil
}

// releaseDispatch is the process's single SIGUSR1 listener. signal.Notify is
// called once and never undone, so a late or stray SIGUSR1 can never fall back
// to Go's default action (terminate), and no hold's registration can disarm
// another's. At most one hold per process may be releasable.
var releaseDispatch struct {
	once sync.Once
	mu   sync.Mutex
	next uint64
	id   uint64 // registration id; 0 = none
	fn   func()
}

// listenForRelease registers fn to run once when this process gets SIGUSR1.
// The returned stop unregisters only this registration.
func listenForRelease(fn func()) (stop func(), err error) {
	d := &releaseDispatch
	d.once.Do(func() {
		ch := make(chan os.Signal, 4)
		signal.Notify(ch, syscall.SIGUSR1)
		go func() {
			for range ch {
				d.mu.Lock()
				f := d.fn
				d.fn, d.id = nil, 0 // fires once per registration
				d.mu.Unlock()
				if f != nil {
					go f()
				}
			}
		}()
	})
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.id != 0 {
		return nil, ErrOneReleasableHold
	}
	d.next++
	id := d.next
	d.id, d.fn = id, fn
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.id == id {
			d.fn, d.id = nil, 0
		}
	}, nil
}

// isSymlinkErr reports whether err is the refusal O_NOFOLLOW gives a symlink.
func isSymlinkErr(err error) bool { return errors.Is(err, syscall.ELOOP) }

// openLocalDir opens <root>/.vibe-local as a directory file descriptor,
// refusing a symlink (a cloned repository can ship one), checking it is owned
// by this user, and creating it 0700 when create is set. Everything the hub
// does afterwards is relative to this descriptor, so the path cannot be
// swapped underneath the lock. root itself is already symlink-resolved by
// workspace.Open.
func openLocalDir(root string, create bool) (*os.File, error) {
	rd, err := os.Open(root)
	if err != nil {
		return nil, fmt.Errorf("failed to open the workspace folder: %w", err)
	}
	defer rd.Close() //nolint:errcheck
	d, err := filelock.OpenDirNoFollow(rd, workspace.LocalDir)
	if err != nil && errors.Is(err, os.ErrNotExist) && create {
		if merr := filelock.MkdirAt(rd, workspace.LocalDir); merr != nil && !errors.Is(merr, os.ErrExist) {
			return nil, fmt.Errorf("failed to create %s: %w", workspace.LocalDir, merr)
		}
		d, err = filelock.OpenDirNoFollow(rd, workspace.LocalDir)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if isSymlinkErr(err) || errors.Is(err, syscall.ENOTDIR) {
			return nil, fmt.Errorf("%w: %s is not a plain directory", ErrUntrusted, workspace.LocalDir)
		}
		return nil, fmt.Errorf("failed to open %s: %w", workspace.LocalDir, err)
	}
	fi, err := d.Stat()
	if err == nil {
		err = checkOwnedDir(fi)
	}
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// checkOwnedDir enforces a real directory (not a symlink) owned by this uid.
func checkOwnedDir(fi os.FileInfo) error {
	if !fi.IsDir() {
		return fmt.Errorf("%w: %s is not a plain directory", ErrUntrusted, filepath.Base(fi.Name()))
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%w: %s is not owned by you", ErrUntrusted, fi.Name())
	}
	return nil
}
