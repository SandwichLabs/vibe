//go:build !unix

package editor

import "io/fs"

// ownedByMe: ownership is not checked where there is no uid.
func ownedByMe(fs.FileInfo) bool { return true }
