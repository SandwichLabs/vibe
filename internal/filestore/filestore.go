// Package filestore is the shared plumbing under the file-backed notebook,
// query and skill stores (#109, plan §4.1): safe paths that never leave the
// workspace, atomic writes (temp file + fsync + rename in the same directory),
// and compare-and-write.
//
// # Compare-and-write
//
// A file is identified by a Stamp, its modification time and a SHA-256 of its
// bytes. A store captures the stamp when it loads a file and passes it back to
// WriteAtomic; if the file on disk no longer hashes to the same value the write
// is refused with ErrConflict and nothing is touched. The hash is the arbiter:
// a changed mtime with identical bytes (a `touch`, a `git checkout` of the
// same content) is not a conflict. Between the check and the rename there is
// a small window another process can slip through; within one process a lock
// closes it, across processes it is the same best-effort an editor offers.
//
// # Paths
//
// Every operation takes the workspace root and a slash-separated relative
// path. The path must stay inside the root: ".." and absolute paths are
// refused, directory symlinks are followed only when they resolve inside the
// root, and a symlink as the file itself is refused (so a write can never be
// redirected out of the workspace and a rename can never replace a link
// target).
package filestore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrConflict is returned when a file changed on disk since the caller loaded
// it. Match it with errors.Is; the concrete error is a *ConflictError.
var ErrConflict = errors.New("changed on disk")

// ErrNotFound is returned when the file does not exist.
var ErrNotFound = errors.New("not found")

// ErrTooLarge is returned for a file over MaxFileSize.
var ErrTooLarge = errors.New("file is too large")

// MaxFileSize caps what Read will load (10 MB): far beyond any notebook, query
// or skill a person writes, small enough that a hostile or runaway file cannot
// exhaust memory on open.
const MaxFileSize = 10 << 20

// ErrOutsideRoot is returned for a path that would leave the workspace.
var ErrOutsideRoot = errors.New("path is outside the workspace")

// Stamp identifies one version of a file.
type Stamp struct {
	ModTime time.Time `json:"modTime"`
	Hash    string    `json:"hash"`
}

// IsZero reports whether the stamp is unset.
func (s Stamp) IsZero() bool { return s.Hash == "" }

// Rev is the opaque revision token a store hands to callers (the content hash).
func (s Stamp) Rev() string { return s.Hash }

// HashBytes is the content hash used in stamps and revisions.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// ConflictError carries the paths and revisions of a refused write so the UI
// can offer reload / overwrite / save as copy.
type ConflictError struct {
	Path string // workspace-relative, slash-separated
	// Missing is true when the file was deleted since the load (or, for a
	// create, already exists).
	Missing  bool
	Existing bool
	Want     string // revision the caller loaded ("" when it never loaded)
	Got      string // revision on disk now ("" when the file is gone)
}

func (e *ConflictError) Error() string {
	switch {
	case e.Missing:
		return fmt.Sprintf("%s was deleted on disk since it was loaded", e.Path)
	case e.Existing:
		return fmt.Sprintf("%s already exists on disk", e.Path)
	}
	return fmt.Sprintf("%s changed on disk since it was loaded", e.Path)
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Observer is told about every successful write and delete a store makes, so
// a file watcher can ignore its own changes. hash is "" for a delete. path is
// absolute.
type Observer func(path, hash string)

// Write describes one write.
type Write struct {
	// Rel is the workspace-relative, slash-separated target.
	Rel  string
	Data []byte
	// Expect is the revision (content hash) the caller loaded. Empty means the
	// caller believes the file does not exist yet.
	Expect string
	// Overwrite skips the conflict check ("overwrite" in the conflict banner).
	Overwrite bool
}

// locks serialises writers inside one process, per root.
var locks sync.Map // root -> *sync.Mutex

func lockFor(root string) *sync.Mutex {
	m, _ := locks.LoadOrStore(root, &sync.Mutex{})
	mu, _ := m.(*sync.Mutex)
	return mu
}

// Resolve returns the absolute path of rel inside root, verifying that it
// cannot escape. The file itself need not exist; its deepest existing
// ancestor is symlink-resolved and must stay inside the resolved root. A
// symlink as the final component is refused.
func Resolve(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, rel)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root: %w", err)
	}
	abs := filepath.Join(realRoot, clean)

	// Walk up to the deepest ancestor that exists and resolve it.
	existing := abs
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %q: %w", rel, err)
	}
	if !Within(realRoot, resolved) {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, rel)
	}
	if fi, err := os.Lstat(abs); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %q is a symbolic link", ErrOutsideRoot, rel)
	}
	return abs, nil
}

// Within reports whether p is root or lies under it. It is purely lexical
// (filepath.Rel): resolve symlinks on both sides first when that matters. The
// one containment check for the workspace's file access, the agent's SQL file
// policy and the session's path guards.
func Within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Read reads rel under root and returns its bytes and stamp.
func Read(root, rel string) ([]byte, Stamp, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return nil, Stamp{}, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Stamp{}, fmt.Errorf("%s: %w", rel, ErrNotFound)
		}
		return nil, Stamp{}, fmt.Errorf("failed to open %s: %w", rel, err)
	}
	defer f.Close() //nolint:errcheck
	fi, err := f.Stat()
	if err != nil {
		return nil, Stamp{}, fmt.Errorf("failed to stat %s: %w", rel, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, Stamp{}, fmt.Errorf("%s is not a regular file", rel)
	}
	if fi.Size() > MaxFileSize {
		return nil, Stamp{}, fmt.Errorf("%s: %w (%d bytes, limit %d)", rel, ErrTooLarge, fi.Size(), MaxFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, Stamp{}, fmt.Errorf("failed to read %s: %w", rel, err)
	}
	if len(data) > MaxFileSize { // grew while reading
		return nil, Stamp{}, fmt.Errorf("%s: %w", rel, ErrTooLarge)
	}
	return data, Stamp{ModTime: fi.ModTime(), Hash: HashBytes(data)}, nil
}

// StatStamp returns the stamp of rel without keeping its bytes; ErrNotFound
// when absent.
func StatStamp(root, rel string) (Stamp, error) {
	_, st, err := Read(root, rel)
	return st, err
}

// WriteAtomic performs a compare-and-write. It returns the new stamp.
func WriteAtomic(root string, w Write, obs Observer) (Stamp, error) {
	mu := lockFor(root)
	mu.Lock()
	defer mu.Unlock()

	abs, err := Resolve(root, w.Rel)
	if err != nil {
		return Stamp{}, err
	}
	prev, err := statHash(abs)
	if err != nil {
		return Stamp{}, err
	}
	if !w.Overwrite {
		cur := prev
		switch {
		case w.Expect == "" && cur != "":
			return Stamp{}, &ConflictError{Path: w.Rel, Existing: true, Got: cur}
		case w.Expect != "" && cur == "":
			return Stamp{}, &ConflictError{Path: w.Rel, Missing: true, Want: w.Expect}
		case w.Expect != "" && cur != w.Expect:
			return Stamp{}, &ConflictError{Path: w.Rel, Want: w.Expect, Got: cur}
		}
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Stamp{}, fmt.Errorf("failed to create %s: %w", filepath.Dir(w.Rel), err)
	}
	// MkdirAll may have followed a link planted since Resolve: check again.
	if resolved, err := filepath.EvalSymlinks(dir); err != nil {
		return Stamp{}, fmt.Errorf("failed to resolve %s: %w", filepath.Dir(w.Rel), err)
	} else if realRoot, _ := filepath.EvalSymlinks(root); !Within(realRoot, resolved) {
		return Stamp{}, fmt.Errorf("%w: %q", ErrOutsideRoot, w.Rel)
	}

	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(abs); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, TempPrefix+filepath.Base(abs)+".*")
	if err != nil {
		return Stamp{}, fmt.Errorf("failed to create temp file for %s: %w", w.Rel, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(w.Data); err != nil {
		_ = tmp.Close()
		cleanup()
		return Stamp{}, fmt.Errorf("failed to write %s: %w", w.Rel, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return Stamp{}, fmt.Errorf("failed to set mode on %s: %w", w.Rel, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return Stamp{}, fmt.Errorf("failed to sync %s: %w", w.Rel, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return Stamp{}, fmt.Errorf("failed to close %s: %w", w.Rel, err)
	}
	// Last look right before the swap (narrows the cross-process window).
	if !w.Overwrite {
		if cur, err := statHash(abs); err == nil && cur != w.Expect {
			cleanup()
			return Stamp{}, &ConflictError{Path: w.Rel, Want: w.Expect, Got: cur, Existing: w.Expect == "", Missing: cur == "" && w.Expect != ""}
		}
	}
	if obs != nil {
		// Announced before the rename so a fast watcher never sees an
		// unannounced version of our own write.
		obs(abs, HashBytes(w.Data))
	}
	if err := os.Rename(tmpName, abs); err != nil {
		cleanup()
		if obs != nil {
			obs(abs, prev) // roll the announcement back: nothing changed
		}
		return Stamp{}, fmt.Errorf("failed to replace %s: %w", w.Rel, err)
	}
	syncDir(dir)
	fi, err := os.Stat(abs)
	if err != nil {
		return Stamp{}, fmt.Errorf("failed to stat %s: %w", w.Rel, err)
	}
	return Stamp{ModTime: fi.ModTime(), Hash: HashBytes(w.Data)}, nil
}

// Remove deletes rel. expect is the revision the caller loaded; "" with
// overwrite=false and a file present is a conflict only when expect was
// supplied and differs. Deleting a missing file is a no-op.
func Remove(root, rel, expect string, overwrite bool, obs Observer) error {
	mu := lockFor(root)
	mu.Lock()
	defer mu.Unlock()
	abs, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	cur, err := statHash(abs)
	if err != nil {
		return err
	}
	if cur == "" {
		return nil
	}
	if !overwrite && expect != "" && cur != expect {
		return &ConflictError{Path: rel, Want: expect, Got: cur}
	}
	if obs != nil {
		obs(abs, "")
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
		if obs != nil {
			obs(abs, cur) // roll the announcement back: the file is still there
		}
		return fmt.Errorf("failed to delete %s: %w", rel, err)
	}
	syncDir(filepath.Dir(abs))
	return nil
}

// statHash returns the content hash of a regular file, "" when absent. It
// streams, so the size cap that guards Read is not needed here.
func statHash(abs string) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read %s: %w", filepath.Base(abs), err)
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to read %s: %w", filepath.Base(abs), err)
	}
	return hex.EncodeToString(h.Sum(nil)[:16]), nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// TempPrefix starts every temp file WriteAtomic creates; watchers ignore it.
const TempPrefix = ".vibe-tmp-"

// IsTemp reports whether a base name is one of WriteAtomic's temp files or an
// editor's hidden/backup file (dotfiles, "~" and ".swp" suffixes).
func IsTemp(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") ||
		strings.HasSuffix(name, ".swp") || strings.HasSuffix(name, ".tmp")
}

// ListDir returns the regular, non-hidden files in rel (not recursive) with
// the given extension, sorted by name. A missing directory is empty.
func ListDir(root, rel, ext string) ([]string, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list %s: %w", rel, err)
	}
	var out []string
	for _, e := range entries {
		if IsTemp(e.Name()) || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		if !e.Type().IsRegular() { // symlinks and directories are skipped
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

// ListSubdirs returns the non-hidden real subdirectories of rel, sorted.
func ListSubdirs(root, rel string) ([]string, error) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list %s: %w", rel, err)
	}
	var out []string
	for _, e := range entries {
		if IsTemp(e.Name()) || !e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

// --- ids and slugs ---

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into a filename stem: lowercase ASCII letters and
// digits joined by "-", at most 64 bytes, "untitled" when nothing is left.
func Slug(title string) string { return SlugOr(title, "untitled", 64) }

// SlugOr is the one slug routine: lowercase s, turn every run of characters
// outside a-z and 0-9 into a single "-", trim "-" from both ends, cut to
// maxBytes (0 means no cut; the result is pure ASCII, so a byte cut is safe)
// and return fallback when nothing is left. Callers differ only in the
// fallback word and the cap: files use Slug, workspace folders and skill
// names pass their own.
func SlugOr(s, fallback string, maxBytes int) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if maxBytes > 0 && len(s) > maxBytes {
		s = strings.Trim(s[:maxBytes], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}

// UniqueSlug returns base, or base-2, base-3, ... the first one exists
// reports false for.
func UniqueSlug(base string, exists func(string) bool) string {
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		cand := base
		if len(cand)+len(suffix) > 64 {
			cand = strings.TrimRight(cand[:64-len(suffix)], "-")
		}
		cand += suffix
		if !exists(cand) {
			return cand
		}
	}
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// windowsReserved are device names Windows refuses as file stems.
var windowsReserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

// ValidID reports whether id is safe to use as a file stem on every platform:
// 1 to 64 characters of lowercase letters, digits, "-" and "_", starting with
// a letter or digit, and not a Windows device name (con, nul, com1, lpt1, ...).
// Slug only ever produces such ids.
func ValidID(id string) bool {
	if !idRE.MatchString(id) {
		return false
	}
	if windowsReserved[id] {
		return false
	}
	for _, p := range []string{"com", "lpt"} {
		if len(id) == 4 && strings.HasPrefix(id, p) && id[3] >= '1' && id[3] <= '9' {
			return false
		}
	}
	return true
}

// SweepStaleTemps removes WriteAtomic temp files older than minAge from the
// given workspace-relative directories (a crash between create and rename
// leaves them behind). Errors are ignored: this is housekeeping.
func SweepStaleTemps(root string, dirs []string, minAge time.Duration) {
	cutoff := time.Now().Add(-minAge)
	for _, d := range dirs {
		abs, err := Resolve(root, d)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), TempPrefix) || !e.Type().IsRegular() {
				continue
			}
			if fi, err := e.Info(); err == nil && fi.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(abs, e.Name()))
			}
		}
	}
}

// RandomSuffix returns n random hex characters.
func RandomSuffix(n int) string {
	b := make([]byte, (n+1)/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

// RemoveEmptyDir removes rel when it is an empty real directory under root;
// anything else (not empty, a link, outside the root) is left alone.
func RemoveEmptyDir(root, rel string) {
	abs, err := Resolve(root, rel)
	if err != nil {
		return
	}
	_ = os.Remove(abs) // fails harmlessly when not empty
}

// WriteFileAtomic writes data to path through a temp file in the same
// directory (synced, then renamed over path), so a reader never sees a
// half-written file and a failure leaves the old one. Unlike WriteAtomic it is
// not confined to a workspace root and does no compare-and-write: it is for
// files the person chose (an export) and per-person config.
//
// The file's mode is the existing destination's when there is one; otherwise
// mode with the process umask applied, as os.WriteFile would. Because the
// file is replaced by rename, a symbolic link or hard link at path is
// replaced, not written through.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	} else {
		mode &^= processUmask()
	}
	return WriteFileAtomicExact(path, data, mode)
}

// WriteFileAtomicExact is WriteFileAtomic with the file's mode set to exactly
// mode: no umask, and an existing file's mode is not kept. For files whose
// permissions are part of their contract (a config that may hold a key).
func WriteFileAtomicExact(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), TempPrefix+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
