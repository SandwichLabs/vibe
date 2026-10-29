package main

import (
	"context"
	"time"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// --- Data artifacts (#162) ---

// ListArtifacts returns every data artifact of the open workspace, a broken one
// included with its error.
func (a *App) ListArtifacts() ([]ArtifactSummary, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return listArtifacts(a.ctxOrBackground(), a.ws.Artifacts())
}

// GetArtifact opens one artifact: its parameters, the view as Go validated it,
// the resolved defaults and the revision. A broken artifact comes back with
// Error set rather than as a failure.
func (a *App) GetArtifact(name string) (*ArtifactDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return getArtifact(a.ctxOrBackground(), a.ws.Artifacts(), name, time.Now())
}

// RunArtifactQuery runs one named query of a stored artifact with the given
// parameter values and returns what RunA2UIQuery does (500 rows at most, 30
// seconds at most). The query comes from the stored artifact, never from the
// caller, and the values only ever become bound parameters; every check is
// agents.Handler.RunArtifactQuery's.
func (a *App) RunArtifactQuery(name, query string, values map[string]any) (*A2UIQueryResult, error) {
	return a.runArtifactQuery(a.ctx, name, query, values)
}

// runArtifactQuery is RunArtifactQuery under a parent context (a custom view's
// grant cancels it when the Worker goes away).
func (a *App) runArtifactQuery(parent context.Context, name, query string, values map[string]any) (*A2UIQueryResult, error) {
	return a.runStoredQuery(parent, func(ctx context.Context, h *agentToolHandler) (*engine.ResultSet, bool, error) {
		return h.RunArtifactQuery(ctx, name, query, values)
	})
}

// runPageQuery runs a query of the manifest a page was hashed with (not the
// file on disk now) under the same gates.
func (a *App) runPageQuery(parent context.Context, name string, m *artifacts.Manifest, query string, values map[string]any) (*A2UIQueryResult, error) {
	return a.runStoredQuery(parent, func(ctx context.Context, h *agentToolHandler) (*engine.ResultSet, bool, error) {
		return h.RunManifestQuery(ctx, name, m, query, values)
	})
}

func (a *App) runStoredQuery(parent context.Context, run func(context.Context, *agentToolHandler) (*engine.ResultSet, bool, error)) (*A2UIQueryResult, error) {
	handler, runSess, err := a.readOnlyRunner()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, a2uiQueryTimeout)
	defer cancel()
	before := session.StatusSnapshot(runSess)
	rs, truncated, err := run(ctx, handler)
	a.afterReadOnlyRun(runSess, before)
	if err != nil {
		return nil, a.explainReadOnlyDenial(err)
	}
	return a2uiResult(rs, truncated)
}

// GetArtifactSource returns an artifact's two files as text, as they are on
// disk, valid or not, with the one revision over both (the Source tab).
func (a *App) GetArtifactSource(name string) (*ArtifactSource, error) {
	store, err := a.artifactStore()
	if err != nil {
		return nil, err
	}
	return getArtifactSource(a.ctxOrBackground(), store, name)
}

// SaveArtifact writes a person's edit of both files. A manifest or view that
// does not pass the structural checks is refused; a stale rev (either file
// changed on disk) is a conflict; the agent policy's objections to a query come
// back as warnings, after the save. The files are written as typed. The write
// runs outside a.mu and completes in the workspace it started in: the store is
// path-based, an open editor is flushed before a switch, and the old watcher is
// stopped, so a late write is the save the person asked for.
func (a *App) SaveArtifact(name, manifest, view, rev string) (*ArtifactSaveResult, error) {
	env, err := a.artifactEditEnv()
	if err != nil {
		return nil, err
	}
	res, err := saveArtifact(a.ctxOrBackground(), env, name, manifest, view, rev)
	return res, conflictErr(err)
}

// CheckArtifact is the agent-policy check of a stored artifact: the warnings a
// save would show, for an artifact written some other way.
func (a *App) CheckArtifact(name string) ([]string, error) {
	env, err := a.artifactEditEnv()
	if err != nil {
		return nil, err
	}
	return checkArtifact(a.ctxOrBackground(), env, name)
}

// NewArtifact creates artifacts/<name> from a small working starter of kind
// ("a2ui" or empty, "html", "html-script" or "script"); a name already in use
// or an unknown kind is refused.
func (a *App) NewArtifact(name, kind string) (*ArtifactSource, error) {
	store, err := a.artifactStore()
	if err != nil {
		return nil, err
	}
	return newArtifact(a.ctxOrBackground(), store, name, kind)
}

// DeleteArtifact removes both files of an artifact, only if they are still at
// the revision the person had open (an outside edit meanwhile is a conflict).
func (a *App) DeleteArtifact(name, rev string) error {
	store, err := a.artifactStore()
	if err != nil {
		return err
	}
	return conflictErr(deleteArtifact(a.ctxOrBackground(), store, name, rev))
}

// artifactStore is the open workspace's artifact store.
func (a *App) artifactStore() (artifacts.Store, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return a.ws.Artifacts(), nil
}
