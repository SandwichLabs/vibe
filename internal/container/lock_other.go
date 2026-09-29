//go:build !unix

package container

// Windows is not a release target (docs/packaging.md), so acquireLock is a
// no-op there: every writable open "succeeds" without cross-process
// exclusion. #80's guarantees only apply on unix builds.
type fileLock struct{}

func acquireLock(vibePath, app string) (*fileLock, error) {
	return &fileLock{}, nil
}

func (l *fileLock) release() error {
	return nil
}
