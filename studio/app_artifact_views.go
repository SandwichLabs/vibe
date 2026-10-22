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
	// A decision ends the artifact's open tokens at once: a revoke or a deny
	// must not wait for the next re-check.
	a.views.dropArtifact(name)
	a.emitEvent("workspace:changed", map[string]any{"kind": "artifactview", "id": name})
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, viewError(err)
	}
	return &st, nil
}

// OpenArtifactView issues the token a custom view's Worker is loaded by
// (/vibe-view/<token>/worker.js), only for a view that is allowed right now; it
// holds the bytes that were hashed, and those are what the token serves. origin
// is the page's own origin, which the view's CSP names. A view that is not
// allowed, or changed since, is refused.
func (a *App) OpenArtifactView(name, origin string) (*ArtifactViewGrant, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	return openViewGrant(a.views, ws, name, origin)
}

// OpenArtifactPage issues the token a static HTML page's frame is loaded by
// (/vibe-view/<token>/page), only for a page that is allowed right now and has
// no script; it holds the files that were hashed, and those are what the token
// renders and serves. origin is the window's own origin, which the page's CSP
// names. A page that is not allowed, or changed since, is refused.
func (a *App) OpenArtifactPage(name, origin string) (*ArtifactPageGrant, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	return openPageGrant(a.views, ws, name, origin, func(m *artifacts.Manifest) artifacts.PageRunner { return a.pageRunner(name, m) })
}

// ValidateArtifactViewPage validates the A2UI page a custom view returned
// against the artifact's manifest (ValidateView with its parameters and
// queries) and returns it as Go re-marshalled it, the only form the renderer is
// given. An invalid page comes back as the validation error.
func (a *App) ValidateArtifactViewPage(name, page string) (string, error) {
	store, err := a.artifactStore()
	if err != nil {
		return "", err
	}
	return validateViewPage(a.ctxOrBackground(), store, name, page)
}

// RunArtifactViewQuery runs one named query for a custom view's Worker, the only
// way its bridge reaches data. The artifact is the grant's, never the caller's:
// the token must still be allowed (same workspace, same ViewHash, decision
// allowed) when the query starts and again when it returns, so an edit to the
// manifest or a revoked view gets no rows. The run stops when the token is
// dropped. Everything else is RunArtifactQuery's (read policy, 500 rows, 30 s).
func (a *App) RunArtifactViewQuery(token, query string, values map[string]any) (*A2UIQueryResult, error) {
	return a.runViewQuery(token, query, values)
}
