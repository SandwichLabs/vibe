//go:build darwin

package main

// platformHelperPreflight is the helper's own refusal: below macOS 14 it does not start.
func platformHelperPreflight() error { return pageHostPreflight() }
