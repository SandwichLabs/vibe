//go:build linux

package main

import (
	"errors"
	"net"
	"os"
)

// platformHelperPreflight is the helper's own check that it is inside the gate
// (pagehost_gate_linux.go), so a helper started any other way refuses before it
// opens a window: the namespace has no interface but loopback, and the home is
// the gate's.
func platformHelperPreflight() error {
	if home, err := os.UserHomeDir(); err != nil || home != helperHome {
		return errors.New("not started by the page gate")
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return errors.New("cannot read the network interfaces")
	}
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback == 0 {
			return errors.New("the helper has a network interface")
		}
	}
	return nil
}
