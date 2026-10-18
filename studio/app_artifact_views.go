package main

// Trust for custom artifact views (view: script, #159). The person's allow or
// deny is remembered outside the folder (views.json in the user config
// directory); these bindings are the only way to change it. No agent tool or
// MCP method reaches them. They carry the artifact's own files (shown as
// source) and a state, never a query result or a credential.

import (
	"errors"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// ArtifactViewState says where an artifact's custom view stands (none,
// undecided, allowed, changed, denied) and returns the exact files its hash
// covers, so what the person reads is what Allow approves.
func (a *App) ArtifactViewState(name string) (*workspace.ArtifactViewStatus, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, viewError(err)
	}
	return &st, nil
}

// viewError is what crosses the bridge: one of the workspace's fixed errors,
// never a raw error (it can hold a path).
func viewError(err error) error {
	for _, fixed := range []error{workspace.ErrViewChanged, workspace.ErrNoScriptView, workspace.ErrDecisionNotSaved} {
		if errors.Is(err, fixed) {
			return fixed
		}
	}
	if errors.Is(err, artifacts.ErrNotFound) {
		return errors.New("there is no such artifact")
	}
	return errors.New("the view could not be read")
}

// AllowArtifactView approves the view whose hash the person was shown. It is
// refused when the files changed since.
func (a *App) AllowArtifactView(name, hash string) (*workspace.ArtifactViewStatus, error) {
	return a.decideArtifactView(name, func(ws workspace.Workspace) error { return ws.AllowArtifactView(name, hash) })
}

// DenyArtifactView records that the view must not run, until revoked.
func (a *App) DenyArtifactView(name string) (*workspace.ArtifactViewStatus, error) {
	return a.decideArtifactView(name, func(ws workspace.Workspace) error { return ws.DenyArtifactView(name) })
}

// RevokeArtifactView forgets the decision; the next open asks again.
func (a *App) RevokeArtifactView(name string) (*workspace.ArtifactViewStatus, error) {
	return a.decideArtifactView(name, func(ws workspace.Workspace) error { return ws.RevokeArtifactView(name) })
}

// ListArtifactViewDecisions lists this workspace's stored view decisions, for
// Settings > Workspace.
func (a *App) ListArtifactViewDecisions() ([]workspace.ArtifactViewDecision, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	return ws.ArtifactViewDecisions(), nil
}

func (a *App) decideArtifactView(name string, f func(workspace.Workspace) error) (*workspace.ArtifactViewStatus, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	if err := f(ws); err != nil {
		return nil, viewError(err)
	}
	a.emitEvent("workspace:changed", map[string]any{"kind": "artifactview", "id": name})
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, viewError(err)
	}
	return &st, nil
}
