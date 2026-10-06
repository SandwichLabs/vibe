//go:build linux

package main

import "os"

// WebKitGTK's JavaScriptCore claims SIGUSR1 for its garbage collector
// ("Overriding existing handler for signal 10") and replaces Go's handler for
// it. Vibe uses SIGUSR1 to ask a workspace's owner to let go (internal/hub, so
// `Take over` works), so without this a release request would crash Studio
// instead of closing the workspace. JSC_SIGNAL_FOR_GC moves the collector to
// SIGUSR2, which nothing else here uses. It has to be set before WebKit starts,
// hence init rather than main.
func init() {
	if os.Getenv("JSC_SIGNAL_FOR_GC") == "" {
		_ = os.Setenv("JSC_SIGNAL_FOR_GC", "12")
	}
}
