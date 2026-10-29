//go:build unix

package repl

import "syscall"

// oNoFollow makes open fail on a symbolic link.
const oNoFollow = syscall.O_NOFOLLOW
