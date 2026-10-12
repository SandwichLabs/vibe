package main

import "github.com/SandwichLabs/vibe/internal/workspace"

// Test-only helpers: kept out of the production build.

// has reports whether h still names a held bundle.
func (s *bundleStore) has(h string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	_, ok := s.held[h]
	return ok
}

// setInitialArg records the first command-line argument: a folder to open, a
// bundle to import, or a plain-language note when it cannot be used.
func (a *App) setInitialArg(arg string) {
	a.launchMu.Lock()
	defer a.launchMu.Unlock()
	if r := classifyLaunchArg(arg); r != (InitialWorkspace{}) {
		a.initial = r
	}
}

func (a *App) ExecuteQuery(sqlStr string) (*QueryResult, error) {
	return a.RunQuery(RunQueryRequest{SQL: sqlStr})
}

// SetAgentConfig saves THIS PERSON's agent settings for the workspace (the
// "On this computer" layer, kept outside the folder). It can lower the tier
// freely and raise it only up to the team limit in vibe.yaml, and it cannot turn
// the gate off when vibe.yaml requires it. Provider/model/endpoint are
// app-global config and are not settable here. Studio-only.
func (a *App) SetAgentConfig(cfg AgentConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := workspace.SetLocalAgent(a.ws, cfg.Sandbox, cfg.AskBeforeWrites); err != nil {
		return err
	}
	policy := a.agentPolicy()
	a.emitEvent("agent:config", map[string]any{"sandbox": string(policy.Mode), "askBeforeWrites": policy.AskBeforeWrites})
	return nil
}

// OpenWorkspaceReadOnly opens the workspace without taking its database, even
// when nobody else holds it.
func (a *App) OpenWorkspaceReadOnly(path string) (*OpenWorkspaceResult, error) {
	return a.open(path, openReadOnly)
}
