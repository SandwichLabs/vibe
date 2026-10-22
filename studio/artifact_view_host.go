package main

// The logic behind OpenArtifactView and ValidateArtifactViewPage (#160): issuing
// a view token only for a view that is allowed right now, with the bytes that
// were hashed, and validating a page a script view returned. The bindings in
// app_artifact_views.go stay thin.

import (
	"context"
	"errors"
	"io/fs"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// errViewNotAllowed is the fixed refusal to open a view that is not allowed (or
// that changed since it was): the person decides in the trust screen.
var errViewNotAllowed = errors.New("this view is not allowed to run")

// ArtifactViewGrant is what OpenArtifactView returns: the token that the view's
// Worker URLs are made from (/vibe-view/<token>/worker.js).
type ArtifactViewGrant struct {
	Token string `json:"token"`
	// Hash is the ViewHash the token is bound to, so the host can tell a later
	// state from the one it opened.
	Hash string `json:"hash"`
}

// viewLibs is the pinned libraries the frontend build copied into dist.
func viewLibs() fs.FS {
	sub, err := fs.Sub(assets, "frontend/dist/vibe-view/lib")
	if err != nil {
		return nil
	}
	return sub
}

// openViewGrant checks, in ws, that the artifact's view is allowed, hashes the
// bytes it read, and holds them under a new token.
func openViewGrant(vs *viewServer, ws workspace.Workspace, name, origin string) (*ArtifactViewGrant, error) {
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, viewError(err)
	}
	if st.Kind != artifacts.ViewScript || st.State != string(stateAllowed) || st.Hash == "" {
		return nil, errViewNotAllowed
	}
	script := []byte(st.Source)
	// The bytes held are the bytes the allowed hash covers, checked here rather
	// than assumed: what runs is what was hashed.
	h, err := artifacts.ViewHash([]byte(st.Manifest), []artifacts.ViewPart{{Path: artifacts.ScriptFile, Data: script}})
	if err != nil || h != st.Hash {
		return nil, errViewNotAllowed
	}
	token, err := vs.issue(&viewGrant{name: name, hash: st.Hash, origin: origin, ws: ws, script: script})
	if err != nil {
		if errors.Is(err, errBadViewOrigin) {
			return nil, err
		}
		return nil, errors.New("the view could not be opened")
	}
	return &ArtifactViewGrant{Token: token, Hash: st.Hash}, nil
}

// stateAllowed is the trustfile state name of an allowed decision.
const stateAllowed = "allowed"

// viewStillAllowed is the view server's re-check: the grant belongs to the open
// workspace and its artifact (a script view or an HTML page, by the grant) is
// allowed at exactly the hash that was served.
func (a *App) viewStillAllowed(g *viewGrant) bool {
	a.mu.RLock()
	ws := a.ws
	a.mu.RUnlock()
	if ws == nil || any(ws) != g.ws {
		return false
	}
	st, err := ws.ArtifactViewState(g.name)
	want := artifacts.ViewScript
	if g.page != nil {
		want = artifacts.ViewHTML
	}
	return err == nil && st.Kind == want && st.State == stateAllowed && st.Hash == g.hash
}

// validateViewPage is ValidateArtifactViewPage over a store: the page a script
// view returned, checked against the manifest and re-marshalled.
func validateViewPage(ctx context.Context, store artifacts.Store, name, page string) (string, error) {
	a, err := store.Get(ctx, name)
	if err != nil {
		return "", errors.New("there is no such artifact")
	}
	out, err := artifacts.RenderPage(&a.Manifest, []byte(page))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// runViewQuery is RunArtifactViewQuery's logic.
func (a *App) runViewQuery(token, query string, values map[string]any) (*A2UIQueryResult, error) {
	g := a.views.lookup(token)
	if g == nil {
		return nil, errViewNotAllowed
	}
	res, err := a.runArtifactQuery(g.ctx, g.name, query, values)
	if err != nil {
		return nil, err
	}
	// The files may have changed while the query ran: rows are delivered only if
	// the view is still allowed at the hash it was served at.
	if a.views.lookup(token) == nil {
		return nil, errViewNotAllowed
	}
	return res, nil
}
