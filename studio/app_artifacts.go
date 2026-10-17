package main

import (
	"context"
	"time"
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
	handler, runSess, err := a.readOnlyRunner()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, a2uiQueryTimeout)
	defer cancel()
	before := statusSnapshot(runSess)
	rs, truncated, err := handler.RunArtifactQuery(ctx, name, query, values)
	a.afterReadOnlyRun(runSess, before)
	if err != nil {
		return nil, a.explainReadOnlyDenial(err)
	}
	return a2uiResult(rs, truncated)
}
