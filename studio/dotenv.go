package main

// The workspace's .env file (#168): a direnv-style allow or deny, remembered
// per person outside the folder. These bindings carry variable names and a
// state, never a value; the values live in the workspace's own overlay
// (internal/workspace/dotenv.go) and reach only env: sign-ins.

import (
	"context"
	"errors"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// GetDotEnvStatus describes the open workspace's .env: state (none, undecided,
// allowed, changed, denied), the variable names it sets, the env: sign-ins they
// would satisfy and whether git tracks it. Never values.
func (a *App) GetDotEnvStatus() (*workspace.DotEnvStatus, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	st := ws.DotEnv()
	return &st, nil
}

// AllowDotEnv approves the .env as it is now and loads it for this workspace.
func (a *App) AllowDotEnv() (*workspace.DotEnvStatus, error) {
	return a.decideDotEnv(workspace.Workspace.AllowDotEnv)
}

// DenyDotEnv records that the .env must not be loaded, until revoked.
func (a *App) DenyDotEnv() (*workspace.DotEnvStatus, error) {
	return a.decideDotEnv(workspace.Workspace.DenyDotEnv)
}

// RevokeDotEnv forgets the decision; the next open asks again.
func (a *App) RevokeDotEnv() (*workspace.DotEnvStatus, error) {
	return a.decideDotEnv(workspace.Workspace.RevokeDotEnv)
}

func (a *App) decideDotEnv(f func(workspace.Workspace) error) (*workspace.DotEnvStatus, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	if err := f(ws); err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	a.refreshEnvConnections(ws)
	a.emitEvent("workspace:changed", map[string]any{"kind": "dotenv"})
	st := ws.DotEnv()
	return &st, nil
}

// refreshEnvConnections re-registers the connections that sign in with
// env:PREFIX and are not connected, so their status reflects the new .env
// decision at once. A connection that is already connected is left alone.
func (a *App) refreshEnvConnections(ws workspace.Workspace) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	cur, sess, ro, err := a.snapshot()
	if err != nil || cur != ws {
		return
	}
	state := map[string]session.StatusKind{}
	for _, st := range sess.Connections() {
		state[st.Name] = st.Kind
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	drop := map[string]bool{}
	var issues []workspace.SessionIssue
	for _, c := range ws.Connections() {
		if kind, _, perr := secrets.ParseMethod(c.SignIn); perr != nil || kind != "env" {
			continue
		}
		if state[c.Name] == session.StatusOK {
			continue
		}
		drop[c.Name] = true
		if msg := a.reregister(ctx, ws, sess, c, ro); msg != "" {
			issues = append(issues, workspace.SessionIssue{Connection: c.Name, Message: msg})
		}
		a.emitStatus(sess, c.Name)
	}
	if len(drop) == 0 {
		return
	}
	a.mu.Lock()
	if a.sess == sess {
		kept := a.issues[:0:0]
		for _, i := range a.issues {
			if !drop[i.Connection] {
				kept = append(kept, i)
			}
		}
		a.issues = append(kept, issues...)
	}
	a.mu.Unlock()
}
