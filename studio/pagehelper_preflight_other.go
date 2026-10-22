//go:build !darwin && !linux

package main

// platformHelperPreflight has nothing to add outside macOS: the Linux gate is the
// launcher's, and the helper only runs inside it.
func platformHelperPreflight() error { return nil }
