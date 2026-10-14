//go:build !unix

package selfupdate

import "os/exec"

func detach(*exec.Cmd) {}

func ownProcessGroup(*exec.Cmd) {}
