//go:build !unix

package trustfile

// lockPath is a no-op where there is no flock: in-process calls are still
// serialised by the store's mutex; two processes may lose an update, which
// fails safe (the person is asked again).
func lockPath(string) (func(), error) { return func() {}, nil }
