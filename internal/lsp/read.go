package lsp

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// maxSnapshot bounds the snapshot file. It is machine-local, but a cloned or
// shared folder can plant anything at the path, and the server reads it for
// hours.
const maxSnapshot = 64 << 20

// readSnapshotFile reads the snapshot at path: a regular file owned by the
// current user, not a symbolic link, at most maxSnapshot bytes. On unix the
// opened descriptor is the one inspected, so nothing can be swapped in between,
// and a FIFO is refused without blocking. Errors never carry file contents. A
// missing file satisfies errors.Is(err, os.ErrNotExist).
func readSnapshotFile(path string) ([]byte, error) {
	f, err := openNoFollow(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("refusing to read the catalog snapshot (not a plain file): %w", err)
	}
	defer f.Close() //nolint:errcheck
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat the catalog snapshot: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("refusing to read the catalog snapshot: not a regular file")
	}
	if !ownedByMe(fi) {
		return nil, errors.New("refusing to read the catalog snapshot: not owned by the current user")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSnapshot+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read the catalog snapshot: %w", err)
	}
	if len(b) > maxSnapshot {
		return nil, fmt.Errorf("refusing to read the catalog snapshot: over the %d byte limit", maxSnapshot)
	}
	return b, nil
}
