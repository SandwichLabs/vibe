//go:build linux

package hub

import "syscall"

// dup2 is Dup3 on Linux: linux/arm64 has no Dup2.
func dup2(oldfd, newfd int) error { return syscall.Dup3(oldfd, newfd, 0) }
