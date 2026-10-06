//go:build unix && !linux

package hub

func sendRelease(pid int, verify func() error) error { return killRelease(pid, verify) }
