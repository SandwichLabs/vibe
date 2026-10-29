package main

// The logic behind OpenArtifactPage (docs/plans/html-artifacts.md, P3): issuing
// a page token only for a static page that is allowed right now, over the files
// that were hashed. The binding in app_artifact_views.go stays thin.

import (
	"context"
	"errors"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// errPageScripted is the fixed refusal to open a scripted page in a frame: it
// has script, so it runs in its own page window (not built yet).
var errPageScripted = errors.New("this page has script, so it opens in a page window")

// ArtifactPageGrant is what OpenArtifactPage returns: the token the page frame's
// URL is made from (/vibe-view/<token>/page), bound to one hash.
type ArtifactPageGrant struct {
	Token string `json:"token"`
	// Hash is the PageHash the token is bound to.
	Hash string `json:"hash"`
}

// openPageGrant checks, in ws, that the artifact is an HTML page that is
// allowed and static, rebuilds it from the bytes that were hashed (checking
// the hash again over them), and holds them under a new token.
func openPageGrant(vs *viewServer, ws workspace.Workspace, name, origin string, runner func(m *artifacts.Manifest, pins map[string]string) artifacts.PageRunner) (*ArtifactPageGrant, error) {
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, viewError(err)
	}
	if st.Kind != artifacts.ViewHTML || st.State != string(stateAllowed) || st.Hash == "" {
		return nil, errViewNotAllowed
	}
	if st.PageKind != artifacts.PageStatic {
		return nil, errPageScripted
	}
	// The bytes held are the bytes the allowed hash covers, checked here rather
	// than assumed: what renders is what was hashed.
	page, err := artifacts.PageFromText(name, []byte(st.Manifest), st.PageFiles)
	if err != nil || page.Page.Hash != st.ContentHash {
		return nil, errViewNotAllowed
	}
	token, err := vs.issue(&viewGrant{name: name, hash: st.Hash, origin: origin, ws: ws, page: page, run: runner(&page.Manifest, st.Pins)})
	if err != nil {
		if errors.Is(err, errBadViewOrigin) {
			return nil, err
		}
		return nil, errors.New("the page could not be opened")
	}
	return &ArtifactPageGrant{Token: token, Hash: st.Hash}, nil
}

// pageRunner answers a page's queries from the manifest the page was hashed
// with (m, held by the grant), not from artifact.yaml as it is on disk when a
// lazy query runs: the same gates as every artifact query (vetted SQL at every
// run, Bind, read-only, the 500-row cap, the 30 s limit), never SQL from the
// page.
func (a *App) pageRunner(name string, m *artifacts.Manifest, pins map[string]string) artifacts.PageRunner {
	return func(ctx context.Context, query string, values map[string]any) (*artifacts.PageRows, error) {
		// The metric files and saved queries the page was allowed over are the
		// only revisions this run may read, whatever a cache last said (#282).
		res, err := a.runPageQuery(semantics.WithPins(ctx, pins), name, m, query, values)
		if err != nil {
			return nil, err
		}
		return &artifacts.PageRows{Columns: res.Columns, Rows: res.Rows, Truncated: res.Truncated}, nil
	}
}
