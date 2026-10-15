package main

// Keeping the session in step with the definitions, and connect, disconnect and
// refresh.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// connTimeout bounds one connect, install or refresh. Test has its own 10 s.
const connTimeout = 60 * time.Second

// connSignature identifies what the session was registered from.
func connSignature(c workspace.Connection) string {
	b, _ := json.Marshal(c)
	return string(b) + "|" + c.SignInFingerprint
}

// recordConnSigs remembers the signatures of the connections just registered
// (called right after the session is opened).
func (a *App) recordConnSigs(ws workspace.Workspace) {
	sigs := map[string]string{}
	for _, c := range ws.Connections() {
		sigs[c.Name] = connSignature(c)
	}
	deps := currentDeps(ws)
	a.connSigMu.Lock()
	a.connSigs = sigs
	a.connDeps = deps
	a.connSigMu.Unlock()
}

func (a *App) sessionOpts(readOnly bool) []workspace.SessionOption {
	if readOnly {
		return []workspace.SessionOption{workspace.WithForceReadOnly()}
	}
	return nil
}

// syncConnections re-registers the connections whose definition or sign-in
// changed since the session saw them, and forgets the ones that are gone. A
// connection that is attached and changed is detached (the next use attaches
// the new definition); the default catalog cannot be detached and keeps its old
// definition until the workspace is reopened. It dials nothing remote.
func (a *App) syncConnections() {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	ws, sess, ro, err := a.snapshot()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	cur := map[string]workspace.Connection{}
	for _, c := range ws.Connections() {
		cur[c.Name] = c
	}
	nextDeps := currentDeps(ws)
	a.connSigMu.Lock()
	old, oldDeps := a.connSigs, a.connDeps
	a.connSigs = map[string]string{}
	for n, c := range cur {
		a.connSigs[n] = connSignature(c)
	}
	next := a.connSigs
	a.connDeps = nextDeps
	a.connSigMu.Unlock()
	changed := changedNames(cur, old, next)
	stale := dependentsOf(cur, changed, oldDeps, nextDeps)

	var issues []workspace.SessionIssue
	drop := map[string]bool{}
	for n := range old {
		if _, ok := cur[n]; !ok {
			_ = sess.Detach(ctx, n)
			drop[n] = true
		}
	}
	// Register every changed connection first, then preflight and connect them:
	// a Delta table or lake connects at once and needs its storage connection to
	// be registered, whatever order the names come in.
	var changedNow []workspace.Connection
	for n, c := range cur {
		if old[n] == next[n] && !stale[n] {
			continue
		}
		drop[n] = true
		if msg := a.registerOnly(ctx, ws, sess, c, ro); msg != "" {
			issues = append(issues, workspace.SessionIssue{Connection: n, Message: msg})
			continue
		}
		changedNow = append(changedNow, c)
	}
	for _, c := range changedNow {
		if msg := a.preflightOne(ctx, sess, c); msg != "" {
			issues = append(issues, workspace.SessionIssue{Connection: c.Name, Message: msg})
		}
	}
	for n := range drop {
		if _, ok := cur[n]; ok {
			a.emitStatus(sess, n)
		}
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

// reregister replaces one connection's registration from the current
// definition. It returns a redacted problem message ("" when fine).
func (a *App) reregister(ctx context.Context, ws workspace.Workspace, sess session.Session, c workspace.Connection, ro bool) string {
	if msg := a.registerOnly(ctx, ws, sess, c, ro); msg != "" {
		return msg
	}
	return a.preflightOne(ctx, sess, c)
}

// registerOnly is the first half of reregister: it replaces the registration
// and dials nothing.
func (a *App) registerOnly(ctx context.Context, ws workspace.Workspace, sess session.Session, c workspace.Connection, ro bool) string {
	name := c.Name
	if name == sess.DefaultCatalog() {
		return fmt.Sprintf("%q is the workspace's own database; reopen the workspace to apply changes to it", name)
	}
	_ = sess.Detach(ctx, name) // registered-but-not-attached entries are removed too
	spec, err := workspace.SessionSpec(ws, name, a.sessionOpts(ro)...)
	if err != nil {
		return a.redact(err.Error())
	}
	if err := sess.Register(spec); err != nil {
		return a.redact(err.Error())
	}
	return ""
}

// preflightOne is the second half: connect a local file, or preflight.
func (a *App) preflightOne(ctx context.Context, sess session.Session, c workspace.Connection) string {
	if c.Type == "duckdb" || c.Type == "sqlite" {
		if err := sess.Connect(ctx, c.Name); err != nil {
			return a.redact(err.Error())
		}
		return ""
	}
	sess.Preflight(ctx, c.Name)
	return ""
}

// emitStatus tells the rail a connection's state, and the state of what depends
// on it (a lake waiting for its storage connection changes with it).
func (a *App) emitStatus(sess session.Session, name string) {
	a.emitOneStatus(sess, name)
	a.connSigMu.Lock()
	var dependents []string
	for n, d := range a.connDeps {
		for _, r := range d.Requires {
			if r == name {
				dependents = append(dependents, n)
			}
		}
	}
	a.connSigMu.Unlock()
	for _, n := range dependents {
		a.emitOneStatus(sess, n)
	}
}

func (a *App) emitOneStatus(sess session.Session, name string) {
	if st, ok := sess.Status(name); ok {
		a.emitEvent("connection:status", map[string]any{"name": name, "status": string(st.Kind), "detail": st.Message})
		return
	}
	a.emitEvent("connection:status", map[string]any{"name": name, "status": string(session.StatusNotConnected), "detail": ""})
}

// ConnectConnection dials a connection (the rail's expand, or Connect). The
// error is redacted. The result is the connection's fresh state.
func (a *App) ConnectConnection(ref ConnectionRef) (*ConnectionInfo, error) {
	_, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	cerr := sess.Connect(ctx, ref.Name)
	a.emitStatus(sess, ref.Name)
	info := a.infoOf(ref.Name)
	if cerr != nil {
		return info, errors.New(a.redact(cerr.Error()))
	}
	a.emitEvent("catalog:changed", map[string]any{"conn": ref.Name})
	return info, nil
}

func (a *App) infoOf(name string) *ConnectionInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ws == nil {
		return nil
	}
	for _, c := range a.connectionInfosLocked() {
		if c.Name == name {
			c := c
			return &c
		}
	}
	return nil
}

// DisconnectConnection detaches a connection and leaves it registered, so it
// reads "not connected" and attaches again on next use.
func (a *App) DisconnectConnection(ref ConnectionRef) (*ConnectionInfo, error) {
	ws, sess, ro, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	if ref.Name == sess.DefaultCatalog() {
		return nil, fmt.Errorf("%s is the workspace's own database and stays connected.", ref.Name)
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	if _, ok := ws.Connection(ref.Name); !ok {
		return nil, fmt.Errorf("There is no connection called %q.", ref.Name)
	}
	if err := sess.Detach(ctx, ref.Name); err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	spec, err := workspace.SessionSpec(ws, ref.Name, a.sessionOpts(ro)...)
	if err == nil {
		err = sess.Register(spec)
	}
	if err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	sess.Preflight(ctx, ref.Name)
	a.emitStatus(sess, ref.Name)
	a.emitEvent("catalog:changed", map[string]any{"conn": ref.Name})
	return a.infoOf(ref.Name), nil
}

// RefreshCatalog clears a connection's cached catalog (Postgres: its own
// metadata cache too) so tables created elsewhere appear. An empty name
// refreshes every attached connection.
func (a *App) RefreshCatalog(ref ConnectionRef) error {
	_, sess, _, err := a.snapshot()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	names := []string{ref.Name}
	if ref.Name == "" {
		names = names[:0]
		for _, st := range sess.Connections() {
			if st.Kind == session.StatusOK {
				names = append(names, st.Name)
			}
		}
	}
	for _, n := range names {
		if err := sess.RefreshCatalog(ctx, n); err != nil {
			return errors.New(a.redact(err.Error()))
		}
		a.emitEvent("catalog:changed", map[string]any{"conn": n})
	}
	return nil
}
