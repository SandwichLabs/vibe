//go:build unix

package lsp

import (
	"io/fs"
	"os"
	"syscall"
)

// openNoFollow opens path read-only without following a symbolic link and
// without blocking on a FIFO.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// ownedByMe reports whether fi belongs to the current user.
func ownedByMe(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Getuid()
}
