//go:build !unix

package lsp

import (
	"errors"
	"io/fs"
	"os"
)

// openNoFollow refuses a symbolic link (Lstat, then open), the best this
// platform allows.
func openNoFollow(path string) (*os.File, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return os.Open(path)
}

// ownedByMe: ownership is not checked where there is no uid.
func ownedByMe(fs.FileInfo) bool { return true }
