package hub

import (
	"errors"
	"os"
)

// ErrOneReleasableHold is returned when a second hold in one process asks to
// be releasable: the release signal is process-wide, so it could not say which
// hold it meant.
var ErrOneReleasableHold = errors.New("only one releasable hub hold is allowed per process")

func closeDir(d *os.File) {
	if d != nil {
		_ = d.Close()
	}
}
