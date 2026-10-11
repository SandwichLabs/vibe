package hub

import "github.com/SandwichLabs/vibe/internal/workspace"

// Test-only helpers: kept out of the production build.

// RequestRelease asks the current owner of ws's hub to let go. It returns once
// the request is delivered, not once the lock is free; use TakeOver to also
// wait for and acquire it.
//
// It signals only when everything checks out: the hub lock is held right now,
// the lock file is trusted (see the package doc), the holder is on this host,
// advertises ControlSigusr1, and is not this process.
func RequestRelease(ws workspace.Workspace) error {
	return requestRelease(ws.Root())
}
