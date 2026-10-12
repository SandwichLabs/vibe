package main

// Compare-and-write conflicts: the wire format the UI recognises, and the
// "Keep mine / Reload / Save as copy" resolver.

import (
	"errors"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// conflictPrefix starts the message of a save refused because the file changed
// on disk. The Wails bridge carries errors as plain strings, so this prefix is
// the wire format; the frontend's isConflict (lib/errors.ts) is its one reader.
const conflictPrefix = "conflict: "

// bridgeConflict is the one error shape a compare-and-write conflict takes
// when it crosses the Wails bridge. It satisfies errors.Is(err,
// filestore.ErrConflict) for Go callers and renders with conflictPrefix for
// the UI.
type bridgeConflict struct{ msg string }

func (c *bridgeConflict) Error() string { return conflictPrefix + c.msg }

func (c *bridgeConflict) Is(target error) bool { return target == filestore.ErrConflict }

// newConflict builds the bridge conflict for a message.
func newConflict(msg string) error { return &bridgeConflict{msg: msg} }

// conflictErr marks a filestore conflict so the UI can tell it from any other
// save failure. Other errors pass through untouched.
func conflictErr(err error) error {

	if err != nil && errors.Is(err, filestore.ErrConflict) {
		var ce *filestore.ConflictError
		if errors.As(err, &ce) {
			return newConflict(ce.Error())
		}
		return newConflict("the file changed on disk since it was loaded")
	}
	return err
}

// ConflictRequest and ConflictResult are the conflict resolver's wire types; the
// rules live in workspace.ResolveConflict.
type (
	ConflictRequest = workspace.ConflictRequest
	ConflictResult  = workspace.ConflictResult
)

// ResolveConflict settles a save that was refused because the file changed on
// disk: "reload" keeps the disk version (the frontend then re-reads it),
// "overwrite" writes the local edit over it, and "saveCopy" writes the local edit
// as a new item named "<title> (copy)".
func (a *App) ResolveConflict(req ConflictRequest) (*ConflictResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	res, err := workspace.ResolveConflict(a.ctxOrBackground(), a.ws, a.sess, req)
	if err != nil {
		return nil, conflictErr(err)
	}
	if req.Kind == workspace.ConflictNotebook && req.Action != workspace.ConflictReload {
		a.emitNotebooksChanged(res.ID, nil)
	}
	a.emitEvent("data:changed")
	return res, nil
}
