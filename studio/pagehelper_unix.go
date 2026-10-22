//go:build !windows

package main

import (
	"errors"
	"os"
	"strconv"
	"syscall"

	"github.com/SandwichLabs/vibe/internal/pagehost"
)

// claimHelperPipes takes the two descriptors Studio passed and marks both
// close-on-exec at once. Without that they leak into WebKitNetworkProcess,
// xdg-dbus-proxy and the web process (P1): after this, only the helper holds
// them.
func claimHelperPipes() (in, out *os.File, err error) {
	syscall.CloseOnExec(pagehost.FDIn)
	syscall.CloseOnExec(pagehost.FDOut)
	// Any descriptor above 4 is one Studio did not mean to pass: mark every one of
	// them, however high, so no process the helper starts holds it. /dev/fd lists
	// the open ones (/proc/self/fd on Linux, fdescfs on macOS).
	ents, err := os.ReadDir("/dev/fd")
	if err != nil {
		return nil, nil, errors.New("the helper cannot list its descriptors")
	}
	for _, e := range ents {
		if n, perr := strconv.Atoi(e.Name()); perr == nil && n > pagehost.FDOut {
			syscall.CloseOnExec(n)
		}
	}
	in = os.NewFile(pagehost.FDIn, "studio-in")
	out = os.NewFile(pagehost.FDOut, "studio-out")
	if in == nil || out == nil {
		return nil, nil, errors.New("the helper was not given its pipes")
	}
	return in, out, nil
}
