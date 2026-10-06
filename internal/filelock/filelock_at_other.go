//go:build !unix

package filelock

import (
	"errors"
	"os"
)

var errNoAt = errors.New("directory-relative locks are not supported on this platform")

// The directory-relative API exists so callers compile everywhere; off unix
// there is no locking at all.
func AcquireAt(dir *os.File, name string, _ []byte) (*Lock, error) { return &Lock{path: name}, nil }
func ProbeAt(*os.File, string) (bool, error)                       { return false, nil }
func OpenAt(*os.File, string) (*os.File, error)                    { return nil, errNoAt }
func OpenDirNoFollow(*os.File, string) (*os.File, error)           { return nil, errNoAt }
func MkdirAt(*os.File, string) error                               { return errNoAt }
