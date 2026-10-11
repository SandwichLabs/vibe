package dotenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Loaded is a read .env file. Vars holds values and must never cross the
// Wails bridge, MCP or the agent; use Names for anything shown.
type Loaded struct {
	// Path is the absolute path of the file (inside the symlink-resolved root).
	Path string
	// Present is true when a usable file was read.
	Present bool
	// Hash is the sha256 (hex) of the file content.
	Hash string
	// Vars, Names and Warnings come from Parse.
	Vars     map[string]string
	Names    []string
	Warnings []string
	// Ignored says why a file that exists is not used ("" when it is used or
	// absent): a symlink pointing outside the workspace, too large, not a
	// regular file, unreadable.
	Ignored string
}

// Read reads <root>/.env. root should be the symlink-resolved workspace root.
// A missing file is Present=false with no error. A file Vibe refuses to use
// (symlink out of the workspace, over MaxSize, not a regular file) is
// reported through Ignored, never as an error.
func Read(root string) Loaded {
	l := Loaded{Path: filepath.Join(root, File)}
	fi, err := os.Lstat(l.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.Ignored = "the file could not be read"
		}
		return l
	}
	real := l.Path
	if fi.Mode()&os.ModeSymlink != 0 {
		r, err := filepath.EvalSymlinks(l.Path)
		if err != nil {
			l.Ignored = "the file is a link that could not be followed"
			return l
		}
		if !filestore.Within(root, r) {
			l.Ignored = "the file is a link that points outside the workspace"
			return l
		}
		real = r
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		l.Ignored = "the file is not a regular file"
		return l
	}
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		l.Ignored = "the file could not be read"
		return l
	}
	defer f.Close() //nolint:errcheck
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		l.Ignored = "the file is not a regular file"
		return l
	}
	if st.Size() > MaxSize {
		l.Ignored = fmt.Sprintf("the file is larger than %d KiB", MaxSize>>10)
		return l
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		l.Ignored = "the file could not be read"
		return l
	}
	if len(data) > MaxSize {
		l.Ignored = fmt.Sprintf("the file is larger than %d KiB", MaxSize>>10)
		return l
	}
	sum := sha256.Sum256(data)
	p := Parse(data)
	l.Present, l.Hash = true, hex.EncodeToString(sum[:])
	l.Vars, l.Names, l.Warnings = p.Vars, p.Names, p.Warnings
	return l
}
