//go:build !unix

package cmd

import "github.com/SandwichLabs/assay/internal/repl"

// watchResize does nothing where there is no SIGWINCH.
func watchResize(int, *repl.Session) func() { return func() {} }
