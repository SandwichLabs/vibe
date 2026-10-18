//go:build unix

package editor

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByMe reports whether fi belongs to the current user.
func ownedByMe(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Getuid()
}
