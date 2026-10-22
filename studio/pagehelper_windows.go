//go:build windows

package main

import (
	"errors"
	"os"
)

func claimHelperPipes() (in, out *os.File, err error) {
	return nil, nil, errors.New("page windows are not available on Windows")
}

// sweepStaleRules has nothing to do here: the helper does not run on Windows.
func sweepStaleRules() {}
