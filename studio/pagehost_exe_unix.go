//go:build !windows

package main

import (
	"os"
	"syscall"
)

// exeRecord is the Studio binary as it was when Studio started: its path and the
// identity (device and inode) of the file there. The helper must be the version
// Studio is, so a helper is started only while the file at that path is still the
// one that was started. Rename-over-self (an update) gives the same path a new
// inode, which os.Executable and EvalSymlinks would happily follow to the new file.
type exeRecord struct {
	path     string
	dev, ino uint64
	ok       bool
}

func recordExe() exeRecord {
	p, err := os.Executable()
	if err != nil {
		return exeRecord{}
	}
	dev, ino, ok := fileID(p)
	return exeRecord{path: p, dev: dev, ino: ino, ok: ok}
}

func fileID(p string) (dev, ino uint64, ok bool) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, 0, false
	}
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true //nolint:unconvert // the field types differ by platform
}

// unchanged reports whether the file at the recorded path is the file that was
// started. A record that could not be made never matches: no helper.
func (r exeRecord) unchanged() bool {
	if !r.ok {
		return false
	}
	dev, ino, ok := fileID(r.path)
	return ok && dev == r.dev && ino == r.ino
}
