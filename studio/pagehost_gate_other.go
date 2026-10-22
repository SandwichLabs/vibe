//go:build !linux && !darwin

package main

import (
	"os"
	"os/exec"
)

// There is no network gate on this platform, so there is no page window.
func gatedCommand(string, *os.File, *os.File, func(string) string) (*exec.Cmd, func(), error) {
	return nil, nil, errPageWindowUnavailable
}

func pageHostPreflight() error { return errPageWindowUnavailable }
