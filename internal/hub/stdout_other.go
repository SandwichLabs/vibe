//go:build !unix

package hub

import "os"

// Without file-descriptor redirection only Go-level writes are protected.
func redirectStdout() (*os.File, error) { return os.Stdout, nil }
