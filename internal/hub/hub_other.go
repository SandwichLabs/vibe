//go:build !unix

package hub

import (
	"os"
	"path/filepath"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

const releaseSupported = false

func checkTrusted(os.FileInfo) error { return nil }

func listenForRelease(func()) (stop func(), err error) { return func() {}, nil }

func isSymlinkErr(error) bool { return false }

func checkOwnedDir(os.FileInfo) error { return nil }

func openLocalDir(root string, create bool) (*os.File, error) {
	dir := filepath.Join(root, workspace.LocalDir)
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return os.Open(dir)
}
