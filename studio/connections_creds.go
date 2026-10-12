package main

// Credentials (write-only): set, connect with, clear and status.

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// CredentialInput is what the editor and the sign-in prompt send. Password,
// Token and Secret are write-only: they are saved and never returned. Empty
// means "keep what is saved".
type CredentialInput struct {
	Username string `json:"username"`
	KeyID    string `json:"keyId"`
	Password string `json:"password"`
	Token    string `json:"token"`
	Secret   string `json:"secret"`
}

// HasSecret reports whether any write-only field was typed.
func (c CredentialInput) HasSecret() bool { return c.Password != "" || c.Token != "" || c.Secret != "" }

func (c CredentialInput) toCredential() secrets.Credential {
	return secrets.Credential{User: c.Username, KeyID: c.KeyID, Password: c.Password, Token: c.Token, Secret: c.Secret}
}

// SetCredentialsRequest sets this person's sign-in method and credential for a
// connection.
type SetCredentialsRequest struct {
	Name string `json:"name"`
	// Method is keychain, prompt or env:PREFIX; empty keeps the current one.
	Method string `json:"method"`
	CredentialInput
	// Remember promotes an "ask each time" credential to the keychain (off by
	// default, D9).
	Remember bool `json:"remember"`
	// ConfirmRebind: see SaveConnectionRequest.
	ConfirmRebind bool `json:"confirmRebind"`
}

// SetConnectionCredentials saves a credential. It is write-only: nothing it
// returns holds a secret. After it, Preflight clears the connection's recent
// failure so the new credential is tried on the next use.
func (a *App) SetConnectionCredentials(req SetCredentialsRequest) (*ConnectionInfo, error) {
	ws, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	a.secrets.Redactor().AddCredential(req.toCredential())
	if err := a.applyCredentials(ws, req); err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	a.afterCredentialChange(sess, req.Name)
	return a.infoOf(req.Name), nil
}

func (a *App) applyCredentials(ws workspace.Workspace, req SetCredentialsRequest) error {
	cur, ok := ws.Connection(req.Name)
	if !ok {
		return fmt.Errorf("There is no connection called %q.", req.Name)
	}
	method := req.Method
	if method == "" {
		method = cur.SignIn
	}
	if method != cur.SignIn {
		if err := a.setSignIn(ws, req.Name, method, req.ConfirmRebind); err != nil {
			return err
		}
	}
	if kind, _, _ := secrets.ParseMethod(method); kind == "env" {
		return nil // environment variables are set outside Vibe
	}
	cred := req.toCredential()
	if cred == (secrets.Credential{}) {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	return ws.SetCredentials(ctx, req.Name, cred, req.Remember)
}

// afterCredentialChange applies what changed: the session re-registers the
// connection when its sign-in method changed, and Preflight clears the 30
// second failure cache and re-reads the credential state, so the rail shows
// the truth and the next use tries again.
func (a *App) afterCredentialChange(sess session.Session, name string) {
	a.syncConnections()
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	sess.Preflight(ctx, name)
	a.emitStatus(sess, name)
	a.emitEvent("workspace:changed", map[string]any{"kind": "connections"})
}

// ConnectWithCredentialsRequest is the sign-in prompt's answer.
type ConnectWithCredentialsRequest struct {
	Name string `json:"name"`
	CredentialInput
	// Remember saves it in the keychain; off keeps it for this session only.
	Remember bool `json:"remember"`
}

// ConnectWithCredentials signs in from the prompt and connects. With Remember
// off the credential is held in memory for this session only: the keychain is
// not written and the person's sign-in method is not changed.
func (a *App) ConnectWithCredentials(req ConnectWithCredentialsRequest) (*ConnectionInfo, error) {
	ws, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	cred := req.toCredential()
	a.secrets.Redactor().AddCredential(cred)
	cur, ok := ws.Connection(req.Name)
	if !ok {
		return nil, fmt.Errorf("There is no connection called %q.", req.Name)
	}
	if kind, _, _ := secrets.ParseMethod(cur.SignIn); kind == "keychain" && !req.Remember {
		rc, rerr := workspace.ResolveConnection(ws, cur, nil)
		if rerr == nil {
			ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
			rerr = ws.Resolver().SetSessionCredentials(ctx, workspace.CredentialRef(ws.ID(), cur.Scope, cur.Name), cred, rc.Binding())
			cancel()
		}
		if rerr != nil {
			return nil, errors.New(a.redact(rerr.Error()))
		}
	} else if err := a.applyCredentials(ws, SetCredentialsRequest{Name: req.Name, Method: cur.SignIn, CredentialInput: req.CredentialInput, Remember: req.Remember}); err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	a.afterCredentialChange(sess, req.Name)
	return a.ConnectConnection(ConnectionRef{Name: req.Name})
}

// ClearCredentialsRequest clears a saved credential, or one field of it.
type ClearCredentialsRequest struct {
	Name string `json:"name"`
	// Field is user | password | token | key_id | secret; empty clears all.
	Field string `json:"field"`
}

// ClearConnectionCredentials removes a saved credential (or one field).
func (a *App) ClearConnectionCredentials(req ClearCredentialsRequest) (*ConnectionInfo, error) {
	ws, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	err = workspace.ClearCredential(ctx, ws, req.Name, req.Field)
	if err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	// An explicit Clear takes the connection off the hub (dropping a storage
	// connection's temporary secret too): it must not keep working on a credential
	// the person just removed. The next use asks again.
	if c, ok := ws.Connection(req.Name); ok {
		ctx2, cancel2 := context.WithTimeout(a.ctxOrBackground(), connTimeout)
		a.mu.RLock()
		ro := a.readOnly
		a.mu.RUnlock()
		a.syncMu.Lock()
		_ = a.reregister(ctx2, ws, sess, c, ro)
		a.syncMu.Unlock()
		cancel2()
	}
	a.afterCredentialChange(sess, req.Name)
	return a.infoOf(req.Name), nil
}

// GetConnectionCredentialStatus says which sign-in method a connection uses, the
// readable fields (username, key id) and whether the secret is set, never the
// secret itself.
func (a *App) GetConnectionCredentialStatus(name string) (*secrets.CredentialStatus, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	st, err := ws.CredentialStatus(ctx, name)
	if err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	return &st, nil
}
