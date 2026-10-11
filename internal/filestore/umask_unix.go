//go:build unix

package filestore

import (
	"io/fs"
	"syscall"
)

// umaskVal is the process umask. There is no pure getter, only set-and-restore,
// and while the umask is briefly 0 any file another goroutine creates would be
// world-writable. Package init runs before main on a single goroutine, so the
// read happens there, before the session, watcher or DuckDB start creating files.
var umaskVal = readUmask()

func readUmask() fs.FileMode {
	old := syscall.Umask(0)
	syscall.Umask(old)
	return fs.FileMode(old) & fs.ModePerm
}

// processUmask is the process umask, read at package init.
func processUmask() fs.FileMode { return umaskVal }
