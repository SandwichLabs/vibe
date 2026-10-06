//go:build !unix

package hub

import "errors"

func sendRelease(int, func() error) error {
	return errors.New("release requests are not supported on this platform")
}
