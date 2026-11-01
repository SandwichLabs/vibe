package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SandwichLabs/slaw/internal/agents"
	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/filestore"
)

// ArtifactSummary is one data artifact as the Library shows it. A folder
// that could not be read as an artifact still lists, with Error set.
type ArtifactSummary struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// View is the view kind: "a2ui", "script", "html (static)" or "html (scripted)"
	// (artifacts.ViewLabel); empty for a broken artifact.
	View      string `json:"view"`
	UpdatedAt string `json:"updated_at"`
	Rev       string `json:"rev"`
	Error     string `json:"error,omitempty"`
}

// ArtifactQueryInfo is one manifest query as the view needs it: its name and
// the parameters its SQL takes. The SQL stays in Go.
type ArtifactQueryInfo struct {
	Name string   `json:"name"`
	Uses []string `json:"uses"`
}

// ArtifactDetail is one artifact opened. View is the page Go validated and
// re-marshalled (never the stored bytes), so the renderer shows exactly what
// passed the check. Defaults are the parameters' default values with relative
// dates resolved on this machine's clock. A broken artifact comes back with
// Error set and no view, so the screen shows what to fix.
type ArtifactDetail struct {
	Name        string              `json:"name"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Params      []artifacts.Param   `json:"params"`
	Queries     []ArtifactQueryInfo `json:"queries"`
	// ViewKind is "a2ui", "script" or "html". A script view or an HTML page
	// has no View here: it is shown as source until the person allows it
	// (ArtifactViewState). PageKind is an HTML page's derived host, "static"
	// or "scripted".
	ViewKind  string          `json:"view_kind"`
	PageKind  string          `json:"page_kind,omitempty"`
	View      json.RawMessage `json:"view,omitempty"`
	Defaults  map[string]any  `json:"defaults"`
	Rev       string          `json:"rev"`
	UpdatedAt string          `json:"updated_at"`
	Error     string          `json:"error,omitempty"`
}

// listArtifacts is ListArtifacts over a store.
func listArtifacts(ctx context.Context, store artifacts.Store) ([]ArtifactSummary, error) {
	list, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}
	out := make([]ArtifactSummary, 0, len(list))
	for _, a := range list {
		out = append(out, ArtifactSummary{
			Name: a.Name, Title: a.Manifest.Title, Description: a.Manifest.Description,
			View: artifacts.ViewLabel(&a), UpdatedAt: a.UpdatedAt, Rev: a.Rev, Error: a.Error,
		})
	}
	return out, nil
}

// ArtifactCounts is how many artifacts of each Library kind a workspace has
// (Home's tiles). A broken artifact counts once, under Broken, whatever kind
// it was meant to be, as the list shows it.
type ArtifactCounts struct {
	Live   int `json:"live"`
	Page   int `json:"page"`
	Script int `json:"script"`
	Broken int `json:"broken"`
}

// countArtifacts counts a listing by kind. A store that cannot list counts as
// none: Home shows zeros rather than failing.
func countArtifacts(ctx context.Context, store artifacts.Store) ArtifactCounts {
	var c ArtifactCounts
	list, err := listArtifacts(ctx, store)
	if err != nil {
		return c
	}
	for _, a := range list {
		switch {
		case a.Error != "":
			c.Broken++
		case a.View == artifacts.ViewScript:
			c.Script++
		case strings.HasPrefix(a.View, artifacts.ViewHTML):
			c.Page++
		case a.View == artifacts.ViewA2UI:
			c.Live++
		}
	}
	return c
}

// getArtifact is GetArtifact over a store. A missing artifact is an error; one
// that exists but cannot be used (a bad file, a view the manifest refuses) is
// a detail with Error set.
func getArtifact(ctx context.Context, store artifacts.Store, name string, now time.Time) (*ArtifactDetail, error) {
	if !filestore.ValidID(name) {
		return nil, fmt.Errorf("invalid artifact name %q", name)
	}
	a, err := store.Get(ctx, name)
	if errors.Is(err, artifacts.ErrNotFound) {
		return nil, fmt.Errorf("there is no artifact named %q", name)
	}
	broken := &ArtifactDetail{Name: name, Params: []artifacts.Param{}, Queries: []ArtifactQueryInfo{}, Defaults: map[string]any{}}
	if err != nil {
		broken.Error = err.Error()
		return broken, nil
	}
	return openArtifact(a, now), nil
}

// openArtifact shapes a loaded artifact. Anything the render or the defaults
// refuse turns into the Error of an otherwise empty detail.
func openArtifact(a *artifacts.Artifact, now time.Time) *ArtifactDetail {
	d := &ArtifactDetail{
		Name: a.Name, Title: a.Manifest.Title, Description: a.Manifest.Description,
		Params: append([]artifacts.Param{}, a.Manifest.Parameters...), Queries: []ArtifactQueryInfo{},
		Rev: a.Rev, UpdatedAt: a.UpdatedAt,
	}
	for _, q := range a.Manifest.Queries {
		d.Queries = append(d.Queries, ArtifactQueryInfo{Name: q.Name, Uses: append([]string{}, q.Uses...)})
	}
	d.ViewKind = a.Manifest.View
	var view []byte
	var err error
	if a.Manifest.View == artifacts.ViewHTML && a.Page != nil {
		d.PageKind = a.Page.Kind
	}
	if a.Manifest.View == artifacts.ViewScript || a.Manifest.View == artifacts.ViewHTML {
		d.Defaults, err = artifacts.Defaults(&a.Manifest, now)
	} else if view, err = artifacts.RenderView(&a.Manifest, a.View); err == nil {
		d.Defaults, err = artifacts.Defaults(&a.Manifest, now)
	}
	if err != nil {
		d.Error = err.Error()
		d.Defaults = map[string]any{}
		return d
	}
	d.View = view
	return d
}

// RunArtifactQuery is RunA2UIQuery's dispatch path for a stored artifact's named
// query: the lifecycle lock and workspace identity check of RunReadOnlySQLIn,
// so a call that outlives a workspace switch is refused instead of running the
// new workspace's artifact of the same name.
func (h *agentToolHandler) RunArtifactQuery(ctx context.Context, name, query string, values map[string]any) (rs *engine.ResultSet, truncated bool, err error) {
	return h.runQuery(func() (*engine.ResultSet, bool, error) { return h.inner.RunArtifactQuery(ctx, name, query, values) })
}

// RunManifestQuery is RunArtifactQuery over a manifest the caller holds (a
// page that was hashed), behind the same workspace identity check.
func (h *agentToolHandler) RunManifestQuery(ctx context.Context, name string, m *artifacts.Manifest, query string, values map[string]any) (*engine.ResultSet, bool, error) {
	return h.runQuery(func() (*engine.ResultSet, bool, error) { return h.inner.RunManifestQuery(ctx, name, m, query, values) })
}

func (h *agentToolHandler) runQuery(run func() (*engine.ResultSet, bool, error)) (*engine.ResultSet, bool, error) {
	h.app.mu.RLock()
	defer h.app.mu.RUnlock()

	if h.app.ws != h.ws || h.app.sess != h.sess {
		return nil, false, fmt.Errorf("the workspace was closed or replaced; reopen the artifact and try again")
	}
	policy := h.app.agentPolicy()
	h.inner.SetFilePolicy(agents.Tighter(h.ceiling, policy.Mode), policy.AskBeforeWrites)
	return run()
}

// VetArtifact is agents.Handler.VetArtifact behind the same workspace identity
// check as RunArtifactQuery: the vet may connect the sources the queries name,
// so a call that outlives a workspace switch is refused instead of running on
// the new workspace.
func (h *agentToolHandler) VetArtifact(ctx context.Context, m *artifacts.Manifest) error {
	h.app.mu.RLock()
	defer h.app.mu.RUnlock()

	if h.app.ws != h.ws || h.app.sess != h.sess {
		return fmt.Errorf("the workspace was closed or replaced; reopen the artifact and try again")
	}
	policy := h.app.agentPolicy()
	h.inner.SetFilePolicy(agents.Tighter(h.ceiling, policy.Mode), policy.AskBeforeWrites)
	return h.inner.VetArtifact(ctx, m)
}

// artifactEditEnv takes the open workspace's artifact store and its vetter in
// one locked step, so a workspace switch cannot pair one workspace's store with
// another's connections.
func (a *App) artifactEditEnv() (artifactEnv, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return artifactEnv{}, err
	}
	return artifactEnv{store: a.ws.Artifacts(), vet: a.newAgentToolHandler(agents.ModeFull), root: a.ws.Root()}, nil
}
