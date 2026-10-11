//go:build !unix

package filestore

import "io/fs"

func processUmask() fs.FileMode { return 0 }
