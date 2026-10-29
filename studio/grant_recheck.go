package main

// Re-checking what is open when something an allowed page reads changes (#282).
// A script view or page whose queries call metric() was allowed over the metric
// files and saved queries it read. When one of those changes (a git pull, the
// Query Explorer, an accepted proposal, the agent's save_query) its decision no
// longer allows it: the grants and windows held for it are dropped at once and
// the trust screen is told, rather than waiting for the next verdict cache
// expiry. The queries themselves are separately pinned to the allowed revisions
// (semantics.WithPins), so even a run that races this check reads nothing else.

import (
	"context"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

// openGrant is a grant or window held for an artifact, with the hash it is for.
type openGrant struct {
	name, hash string
	ws         any // the workspace it was opened in
}

// snapshot lists the grants held.
func (v *viewServer) snapshot() []openGrant {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]openGrant, 0, len(v.grants))
	for _, g := range v.grants {
		out = append(out, openGrant{g.name, g.hash, g.ws})
	}
	return out
}

// snapshot lists the open page windows.
func (h *pageHost) snapshot() []openGrant {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]openGrant, 0, len(h.wins))
	for name, w := range h.wins {
		out = append(out, openGrant{name, w.hash, w.ws})
	}
	return out
}

// recheckGrants drops every open view grant and page window whose artifact is
// not allowed at the hash it was opened for any more, and announces each to the
// trust screen. With nothing open it does nothing.
func (a *App) recheckGrants() {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return
	}
	a.recheckGrantsIn(a.ctxOrBackground(), ws)
}

// recheckGrantsIn is recheckGrants for a workspace the caller holds.
func (a *App) recheckGrantsIn(ctx context.Context, ws workspace.Workspace) {
	open := append(a.views.snapshot(), a.pages.snapshot()...)
	if len(open) == 0 {
		return
	}
	ended := map[string]bool{}
	for _, g := range open {
		if ended[g.name] || (g.ws != nil && g.ws != any(ws)) {
			continue
		}
		st, err := ws.ArtifactViewStateCtx(ctx, g.name)
		if err == nil && st.State == stateAllowed && st.Hash == g.hash {
			continue
		}
		ended[g.name] = true
		a.views.dropArtifact(g.name)
		a.pages.closeArtifact(g.name, pageStateChanged)
		a.emitEvent("workspace:changed", map[string]any{"kind": "artifactview", "id": g.name})
	}
}
