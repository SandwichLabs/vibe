package main

// Connections (#117): the bindings behind the connections rail, the connection
// editor, the sign-in and extension prompts and the connection selector.
//
// Write-only contract (plan 4.3): no binding here returns a secret. A password,
// token or secret key can be set, replaced and cleared, and the status says
// whether one is set; the username and key id are not secrets and are readable.
// Every error that reaches the UI is redacted.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// connTimeout bounds one connect, install or refresh. Test has its own 10 s.
const connTimeout = 60 * time.Second

var serverChangedRE = regexp.MustCompile(`\(([^()]+) to ([^()]+)\)`)

// ---- types ----

// ServerChange says a connection's server differs from the one the saved
// sign-in was made for.
type ServerChange struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// TypeSpec is one connection type for the editor's picker and form.
type TypeSpec struct {
	ID                string                   `json:"id"`
	Label             string                   `json:"label"`
	Description       string                   `json:"description"`
	Fields            []connections.Field      `json:"fields"`
	Credentials       []connections.Field      `json:"credentials"`
	RequiresExtension []string                 `json:"requiresExtension"`
	Capabilities      connections.Capabilities `json:"capabilities"`
	FingerprintFields []string                 `json:"fingerprintFields"`
	SignInMethods     []string                 `json:"signInMethods"`
	// Soon marks a type that is shown but cannot be chosen yet. Every type of
	// the registry is available since #119; the flag stays for the picker.
	Soon bool `json:"soon"`
}

var typeDescriptions = map[string]string{
	"duckdb":     "A DuckDB database file on this computer.",
	"postgres":   "A PostgreSQL server. Read it in place; nothing is copied.",
	"motherduck": "A MotherDuck cloud database.",
	"sqlite":     "A SQLite database file on this computer.",
	"s3":         "Files in an Amazon S3 bucket.",
	"gcs":        "Files in a Google Cloud Storage bucket.",
	"r2":         "Files in a Cloudflare R2 bucket.",
	"mysql":      "A MySQL or MariaDB server. Read it in place; nothing is copied.",
	"azure":      "Files in Azure Blob Storage or Data Lake (az://, azure://, abfss://).",
	"ducklake":   "A DuckLake: tables in Parquet files with their catalog in a DuckDB file.",
	"iceberg":    "An Iceberg table, read with iceberg_scan('<location>').",
	"delta":      "A Delta Lake table, read with delta_scan('<location>').",
}

// ListConnectionTypes returns the connection types, in the order the picker
// shows them, generated from the type registry.
func (a *App) ListConnectionTypes() []TypeSpec {
	infos := map[string]connections.TypeInfo{}
	for _, i := range connections.ListConnectionTypes() {
		infos[i.Name] = i
	}
	order := []string{"duckdb", "postgres", "mysql", "motherduck", "sqlite", "ducklake", "s3", "gcs", "r2", "azure", "iceberg", "delta"}
	out := make([]TypeSpec, 0, len(order))
	for _, id := range order {
		i, ok := infos[id]
		if !ok {
			continue
		}
		methods := i.SignInMethods
		if len(methods) == 0 {
			methods = []string{"keychain", "env", "prompt"}
		}
		out = append(out, TypeSpec{
			ID: id, Label: i.Label, Description: typeDescriptions[id],
			Fields: nonNilFields(i.Fields), Credentials: nonNilFields(i.Credentials),
			RequiresExtension: append([]string{}, i.Extensions...),
			Capabilities:      i.Capabilities, FingerprintFields: append([]string{}, i.FingerprintFields...),
			SignInMethods: methods,
		})
	}
	return out
}

func nonNilFields(f []connections.Field) []connections.Field {
	if f == nil {
		return []connections.Field{}
	}
	return f
}

// ConnectionList is the rail's and the selector's data.
type ConnectionList struct {
	Connections []ConnectionInfo `json:"connections"`
	// RelaunchRequired says the workspace has to be reopened (MotherDuck
	// accepts one token per process, D10); Studio does it by closing and
	// reopening the workspace session.
	RelaunchRequired bool   `json:"relaunchRequired"`
	RelaunchMessage  string `json:"relaunchMessage,omitempty"`
}

// snapshot returns the open workspace, session and read-only flag without
// holding a.mu, so a slow connect or test never blocks open, close or settings.
func (a *App) snapshot() (workspace.Workspace, session.Session, bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, nil, false, err
	}
	return a.ws, a.sess, a.readOnly, nil
}

// ListConnections lists every connection with its status, scope, shadowing,
// read-only flag and whether it is remote. It never dials.
func (a *App) ListConnections() (*ConnectionList, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	out := &ConnectionList{Connections: a.connectionInfosLocked()}
	out.RelaunchRequired, out.RelaunchMessage = a.sess.RelaunchRequired()
	return out, nil
}

// ConnectionRef names one connection.
type ConnectionRef struct {
	Name string `json:"name"`
	// Scope is shared | local | user; empty means the winning definition.
	Scope string `json:"scope"`
}

// ConnectionDetail is one connection as the editor shows it.
type ConnectionDetail struct {
	Name  string                  `json:"name"`
	Scope string                  `json:"scope"`
	Def   workspace.ConnectionDef `json:"def"`
	// Fingerprint is workspace.Fingerprint(Def): what a saved sign-in is bound
	// to. The editor compares it with ConnectionFingerprint(draft); the
	// comparison is Go's, so every field that counts is counted. A one-way hash
	// of non-secret values.
	Fingerprint string `json:"fingerprint"`
	// Shadows are the lower scopes that also define this name.
	Shadows []string `json:"shadows"`
	// ShadowedBy is set when this is not the winning definition: the scope that
	// overrides it on this computer.
	ShadowedBy string `json:"shadowedBy,omitempty"`
	// SignIn is this person's sign-in method; SignInHint the team file's
	// suggestion (it has no effect).
	SignIn     string                   `json:"signIn"`
	SignInHint string                   `json:"signInHint,omitempty"`
	Credential secrets.CredentialStatus `json:"credential"`
	Info       ConnectionInfo           `json:"info"`
}

func scopeStrings(s []workspace.Scope) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		out = append(out, string(x))
	}
	return out
}

// scopeChoice reads the scope the editor sent; the wording is Studio's.
func scopeChoice(s string) (workspace.Scope, error) {
	sc, ok := workspace.ParseScope(s)
	if !ok {
		return "", fmt.Errorf("Choose where to save this connection: Team, This computer or All my workspaces.")
	}
	return sc, nil
}

// scopeDef reads name's definition out of one scope's file.
func scopeDef(ws workspace.Workspace, scope workspace.Scope, name string) (workspace.ConnectionDef, bool) {
	var defs []workspace.ConnectionDef
	switch scope {
	case workspace.ScopeShared:
		defs = ws.Config().Connections
	case workspace.ScopeLocal:
		defs = ws.Local().Connections
	case workspace.ScopeUser:
		if u, err := workspace.LoadUserConfig(); err == nil {
			defs = u.Connections
		}
	}
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return workspace.ConnectionDef{}, false
}

// GetConnection describes the winning definition of name.
func (a *App) GetConnection(name string) (*ConnectionDetail, error) {
	return a.GetConnectionInScope(ConnectionRef{Name: name})
}

// GetConnectionInScope describes name's definition in one scope (the shadowed
// Team copy behind "Look at the Team one"), or the winning one with no scope.
// Nothing in the result is a secret.
func (a *App) GetConnectionInScope(ref ConnectionRef) (*ConnectionDetail, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	winConn, ok := ws.Connection(ref.Name)
	if !ok {
		return nil, fmt.Errorf("There is no connection called %q.", ref.Name)
	}
	win := &winConn
	d := &ConnectionDetail{
		Name: win.Name, Scope: string(win.Scope), Def: win.ConnectionDef,
		Shadows: scopeStrings(win.Shadows), SignIn: win.SignIn, SignInHint: win.SignInHint,
	}
	if ref.Scope != "" && ref.Scope != string(win.Scope) {
		sc, err := scopeChoice(ref.Scope)
		if err != nil {
			return nil, err
		}
		def, ok := scopeDef(ws, sc, ref.Name)
		if !ok {
			return nil, fmt.Errorf("There is no %s copy of %q.", scopeWord(ref.Scope), ref.Name)
		}
		d.Scope, d.Def, d.ShadowedBy = ref.Scope, def, string(win.Scope)
		d.Shadows = []string{}
	}
	d.Fingerprint = workspace.Fingerprint(d.Def)
	a.mu.RLock()
	for _, ci := range a.connectionInfosLocked() {
		if ci.Name == ref.Name {
			d.Info = ci
		}
	}
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	st, err := ws.CredentialStatus(ctx, ref.Name)
	if err != nil {
		st = secrets.CredentialStatus{Method: win.SignIn, Detail: a.redact(err.Error())}
	}
	d.Credential = st
	return d, nil
}

// ConnectionFingerprint is the fingerprint of a draft definition from the
// editor form, to compare with ConnectionDetail.Fingerprint: when they differ,
// Save re-binds the saved sign-in. It reads only the definition (never a
// credential) and returns only the hash.
func (a *App) ConnectionFingerprint(def workspace.ConnectionDef) string {
	return workspace.Fingerprint(def)
}

func scopeWord(s string) string {
	switch s {
	case "shared":
		return "Team"
	case "local":
		return "This computer"
	}
	return "All my workspaces"
}

// redact scrubs credential-shaped text and every secret value the app has seen.
func (a *App) redact(s string) string {
	return secrets.RedactPatterns(a.secrets.Redactor().Redact(s))
}

// ---- save, move, delete ----

// SaveConnectionRequest saves a definition. SignIn is this person's sign-in
// method (keychain, prompt or env:PREFIX); empty keeps the current one. It never
// carries a credential: those go through SetConnectionCredentials.
type SaveConnectionRequest struct {
	Scope  string                  `json:"scope"`
	Def    workspace.ConnectionDef `json:"def"`
	IsNew  bool                    `json:"isNew"`
	SignIn string                  `json:"signIn"`
	// ConfirmRebind is the person's yes to using an environment sign-in for a
	// server other than the one it was chosen for. The UI sets it only after
	// showing the server-changed confirmation; without it the backend refuses to
	// re-bind, whatever a client sends.
	ConfirmRebind bool `json:"confirmRebind"`
}

// rebindPrefix marks the refusal the editor answers with its confirmation.
const rebindPrefix = "confirm-rebind: "

// setSignIn applies a sign-in method; a re-bind of an environment sign-in to a
// changed server needs confirm.
func (a *App) setSignIn(ws workspace.Workspace, name, method string, confirm bool) error {
	if confirm {
		return ws.RebindSignIn(name, method)
	}
	if err := ws.SetSignIn(name, method); err != nil {
		if errors.Is(err, workspace.ErrRebindNeedsConfirm) {
			return errors.New(rebindPrefix + err.Error())
		}
		return err
	}
	return nil
}

// SaveConnection adds or updates a definition in the chosen scope and applies it
// to the open session without dialling. The name is immutable after the first
// save: an update names an existing connection.
func (a *App) SaveConnection(req SaveConnectionRequest) (*ConnectionDetail, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	scope, err := scopeChoice(req.Scope)
	if err != nil {
		return nil, err
	}
	req.Def.Name = strings.TrimSpace(req.Def.Name)
	if req.IsNew {
		err = ws.AddConnection(scope, req.Def)
	} else {
		err = ws.UpdateConnection(scope, req.Def)
	}
	if err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	if req.SignIn != "" {
		if err := a.setSignIn(ws, req.Def.Name, req.SignIn, req.ConfirmRebind); err != nil {
			a.syncConnections() // the definition was saved; the binding is unchanged and still guards it
			return nil, errors.New(a.redact(err.Error()))
		}
	}
	a.syncConnections()
	a.emitEvent("workspace:changed", map[string]any{"kind": "connections"})
	return a.GetConnection(req.Def.Name)
}

// MoveConnectionRequest moves a connection to another scope.
type MoveConnectionRequest struct {
	Name string `json:"name"`
	To   string `json:"to"`
}

// MoveConnection changes where a connection is saved. The saved sign-in follows
// it (it is bound to the workspace id, the name and the server fingerprint).
func (a *App) MoveConnection(req MoveConnectionRequest) (*ConnectionDetail, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	to, err := scopeChoice(req.To)
	if err != nil {
		return nil, err
	}
	cur, ok := ws.Connection(req.Name)
	if !ok || cur.Scope == "" {
		return nil, fmt.Errorf("There is no connection called %q.", req.Name)
	}
	from := cur.Scope
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	if err := ws.MoveConnection(ctx, req.Name, from, to); err != nil {
		return nil, errors.New(a.redact(err.Error()))
	}
	a.syncConnections()
	a.emitEvent("workspace:changed", map[string]any{"kind": "connections"})
	return a.GetConnection(req.Name)
}

// DeleteConnection removes a definition from one scope. Its saved sign-in goes
// with it when nothing else keeps using it. Your database isn't touched.
func (a *App) DeleteConnection(ref ConnectionRef) error {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return err
	}
	scope, err := scopeChoice(ref.Scope)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	if err := ws.RemoveConnection(ctx, scope, ref.Name); err != nil {
		return errors.New(a.redact(err.Error()))
	}
	a.syncConnections()
	a.emitEvent("workspace:changed", map[string]any{"kind": "connections"})
	return nil
}

// ---- keeping the session in step with the definitions ----

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
	a.connSigMu.Lock()
	a.connSigs = sigs
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
	a.connSigMu.Lock()
	old := a.connSigs
	a.connSigs = map[string]string{}
	for n, c := range cur {
		a.connSigs[n] = connSignature(c)
	}
	next := a.connSigs
	a.connSigMu.Unlock()

	var issues []workspace.SessionIssue
	drop := map[string]bool{}
	for n := range old {
		if _, ok := cur[n]; !ok {
			_ = sess.Detach(ctx, n)
			drop[n] = true
		}
	}
	for n, c := range cur {
		if old[n] == next[n] {
			continue
		}
		drop[n] = true
		if msg := a.reregister(ctx, ws, sess, c, ro); msg != "" {
			issues = append(issues, workspace.SessionIssue{Connection: n, Message: msg})
		}
		a.emitStatus(sess, n)
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
	if c.Type == "duckdb" || c.Type == "sqlite" {
		if err := sess.Connect(ctx, name); err != nil {
			return a.redact(err.Error())
		}
		return ""
	}
	sess.Preflight(ctx, name)
	return ""
}

func (a *App) emitStatus(sess session.Session, name string) {
	if st, ok := sess.Status(name); ok {
		a.emitEvent("connection:status", map[string]any{"name": name, "status": string(st.Kind), "detail": st.Message})
		return
	}
	a.emitEvent("connection:status", map[string]any{"name": name, "status": string(session.StatusNotConnected), "detail": ""})
}

// ---- connect, disconnect, refresh ----

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

// ---- the catalog tree ----

// CatalogRequest asks for one level of a connection's tree: its schemas (no
// schema), a schema's tables and views, or a table's columns.
type CatalogRequest struct {
	Connection string `json:"connection"`
	Schema     string `json:"schema"`
	Table      string `json:"table"`
}

// CatalogNode is one schema, table, view or column.
type CatalogNode struct {
	Kind      string `json:"kind"` // schema | table | view | column
	Name      string `json:"name"`
	Qualified string `json:"qualified,omitempty"`
	// Rows is an estimate (DuckDB statistics) when known; -1 otherwise.
	Rows     int64  `json:"rows"`
	DataType string `json:"dataType,omitempty"`
	PK       bool   `json:"pk,omitempty"`
	FK       bool   `json:"fk,omitempty"`
}

// CatalogListing is one level of the tree. When the connection has only one
// schema, Flattened is true and Nodes are its tables directly (the schema level
// is hidden), with Schema naming it.
type CatalogListing struct {
	Kind      string        `json:"kind"` // schemas | tables | columns
	Schema    string        `json:"schema"`
	Flattened bool          `json:"flattened"`
	Nodes     []CatalogNode `json:"nodes"`
}

// ListCatalog returns one lazy level of a connection's tree. It never dials: a
// connection that is not connected is an error the rail answers by connecting
// first.
func (a *App) ListCatalog(req CatalogRequest) (*CatalogListing, error) {
	_, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	if err := session.ValidateName(req.Connection); err != nil {
		return nil, fmt.Errorf("There is no connection called %q.", req.Connection)
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	fail := func(err error) (*CatalogListing, error) { return nil, errors.New(a.redact(err.Error())) }

	if req.Table != "" {
		schema := req.Schema
		if schema == "" {
			if schema, err = sess.DefaultSchema(ctx, req.Connection); err != nil {
				return fail(err)
			}
		}
		ref := session.TableRef{Catalog: req.Connection, Schema: schema, Name: req.Table}
		cols, err := engine.TableColumns(sess.DB(), ref)
		if err != nil {
			return fail(err)
		}
		out := &CatalogListing{Kind: "columns", Schema: schema, Nodes: []CatalogNode{}}
		for _, c := range cols {
			out.Nodes = append(out.Nodes, CatalogNode{Kind: "column", Name: c.Name, DataType: c.Type, PK: c.PK, FK: c.FK, Rows: -1})
		}
		return out, nil
	}
	schemas := []string{}
	if req.Schema == "" {
		all, err := sess.ListSchemas(ctx, req.Connection)
		if err != nil {
			return fail(err)
		}
		for _, sc := range all {
			// Catalog plumbing, not data (Postgres keeps pg_toast and per-session
			// pg_temp_N schemas).
			if sc == "pg_toast" || strings.HasPrefix(sc, "pg_temp_") || strings.HasPrefix(sc, "pg_toast_temp_") {
				continue
			}
			schemas = append(schemas, sc)
		}
		// The schema level is hidden only while a connection has just `main`
		// (SPEC 05): a Postgres database with only `public` still shows it.
		if !(len(schemas) == 1 && schemas[0] == "main") {
			out := &CatalogListing{Kind: "schemas", Nodes: []CatalogNode{}}
			for _, s := range schemas {
				out.Nodes = append(out.Nodes, CatalogNode{Kind: "schema", Name: s, Rows: -1})
			}
			return out, nil
		}
		req.Schema = schemas[0] // only one: hide the level
	}
	tabs, err := sess.ListTables(ctx, req.Connection, req.Schema)
	if err != nil {
		return fail(err)
	}
	est := a.estimatedRows(ctx, sess, req.Connection, req.Schema)
	out := &CatalogListing{Kind: "tables", Schema: req.Schema, Flattened: len(schemas) == 1 && schemas[0] == "main", Nodes: []CatalogNode{}}
	for _, t := range tabs {
		kind := "table"
		if strings.Contains(strings.ToUpper(t.Type), "VIEW") {
			kind = "view"
		}
		n := CatalogNode{Kind: kind, Name: t.Name, Qualified: t.Ref().String(), Rows: -1}
		if r, ok := est[t.Name]; ok && kind == "table" {
			n.Rows = r
		}
		out.Nodes = append(out.Nodes, n)
	}
	return out, nil
}

// estimatedRows reads DuckDB's table statistics (free, approximate) for the
// tables of one schema. Remote catalogs often have none: the map is then empty.
func (a *App) estimatedRows(ctx context.Context, sess session.Session, catalog, schema string) map[string]int64 {
	out := map[string]int64{}
	rows, err := sess.DB().QueryContext(ctx,
		`SELECT table_name, estimated_size FROM duckdb_tables() WHERE database_name = $1 AND schema_name = $2`, catalog, schema)
	if err != nil {
		return out
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var n string
		var sz int64
		if rows.Scan(&n, &sz) == nil && sz > 0 {
			out[n] = sz
		}
	}
	return out
}

// TableCapabilities says whether a table can be edited and, when not, why.
type TableCapabilities struct {
	Connection string `json:"connection"`
	Editable   bool   `json:"editable"`
	// Reason is connection-read-only | view | locked | workspace-read-only |
	// not-connected ("" when editable).
	Reason string `json:"reason"`
	// Text is the plain-language reason for the banner.
	Text string `json:"text"`
}

// GetTableCapabilities explains why a table is read-only, for the datasheet's
// banner (SPEC 07 E). It never dials.
func (a *App) GetTableCapabilities(table string) (*TableCapabilities, error) {
	_, sess, ro, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	ref, err := engine.ParseTable(table)
	if err != nil {
		return nil, fmt.Errorf("invalid table name")
	}
	ctx := a.ctxOrBackground()
	if resolved, rerr := sess.Resolve(ctx, ref); rerr == nil {
		ref = resolved
	}
	caps := &TableCapabilities{Connection: ref.Catalog, Editable: true}
	var st *session.Status
	if s, ok := sess.Status(ref.Catalog); ok {
		st = &s
	}
	switch {
	case st != nil && st.Kind == session.StatusLockedBy:
		caps.Editable, caps.Reason = false, "locked"
		caps.Text = fmt.Sprintf("%s is in use by another program, so its tables can only be read until it lets go.", ref.Catalog)
	case ro && st != nil && !connections.IsRemote(st.Type):
		caps.Editable, caps.Reason = false, "workspace-read-only"
		caps.Text = "This workspace is open read-only because another program has it."
	case st != nil && st.ReadOnly:
		caps.Editable, caps.Reason = false, "connection-read-only"
		caps.Text = fmt.Sprintf("%s is set to read-only, so rows can't be added, edited or deleted here.", ref.Catalog)
	}
	if caps.Editable {
		if tabs, err := sess.ListTables(ctx, ref.Catalog, ref.Schema); err == nil {
			for _, t := range tabs {
				if t.Name == ref.Name && strings.Contains(strings.ToUpper(t.Type), "VIEW") {
					caps.Editable, caps.Reason, caps.Text = false, "view", "This is a view, so it can't be edited."
				}
			}
		}
	}
	return caps, nil
}

// ---- credentials (write-only) ----

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
	if req.Field == "" {
		err = ws.ClearCredentials(ctx, req.Name)
	} else {
		c, ok := ws.Connection(req.Name)
		if !ok {
			return nil, fmt.Errorf("There is no connection called %q.", req.Name)
		}
		err = ws.Resolver().ClearCredentialField(ctx, workspace.CredentialRef(ws.ID(), c.Scope, c.Name), c.SignIn, req.Field)
		if errors.Is(err, secrets.ErrNotFound) {
			err = errors.New("There is nothing saved to clear for this connection.")
		}
	}
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

// ---- test ----

// TestConnectionRequest tests a definition without saving it. Credentials are
// what the person just typed (new, never returned); fields left empty fall back
// to the saved credential of an existing connection of the same name, but only
// while the definition still points at the server it was saved for.
type TestConnectionRequest struct {
	Scope       string                  `json:"scope"`
	Def         workspace.ConnectionDef `json:"def"`
	SignIn      string                  `json:"signIn"`
	Credentials CredentialInput         `json:"credentials"`
}

// TestError is a failed test in plain language, plus the redacted raw text.
type TestError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// TestResultView is what TestConnection returns. Nothing in it is a secret.
type TestResultView struct {
	OK      bool       `json:"ok"`
	Status  string     `json:"status"`
	Ms      int64      `json:"ms"`
	Count   int        `json:"count"`
	Detail  string     `json:"detail,omitempty"`
	Summary string     `json:"summary,omitempty"`
	Error   *TestError `json:"error,omitempty"`
	// InstallExtension names an extension to install (then test again).
	InstallExtension string `json:"installExtension,omitempty"`
}

// TestConnection attaches the definition in a throwaway session and probes it,
// with a 10 second limit. It never touches the workspace's own session.
func (a *App) TestConnection(req TestConnectionRequest) (*TestResultView, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	scope := workspace.ScopeLocal
	if req.Scope != "" {
		if scope, err = scopeChoice(req.Scope); err != nil {
			return nil, err
		}
	}
	def := req.Def
	if def.Name == "" {
		def.Name = "test"
	}
	typed := req.Credentials.toCredential()
	a.secrets.Redactor().AddCredential(typed)
	fail := func(code, msg, details string) *TestResultView {
		return &TestResultView{Status: string(session.StatusError), Error: &TestError{Code: code, Message: msg, Details: a.redact(details)}}
	}
	rc, err := workspace.ResolveConnection(ws, workspace.Connection{ConnectionDef: def, Scope: scope}, nil)
	if err != nil {
		return fail("invalid", a.redact(err.Error()), err.Error()), nil
	}
	cred, serverChanged := a.testBaseCredential(ws, rc, def, req.SignIn)
	if serverChanged {
		// P3: when the definition points somewhere other than the server the saved
		// sign-in was made for, only credentials typed in this edit are used, never
		// a stored one of any method (keychain, environment or prompt).
		// A prefilled username is not a typed credential: the secret must be typed.
		if connections.NeedsCredential(def.Type) && !typed.HasSecret() {
			return fail("server-changed", "The server changed, so your saved sign-in won't be used. Enter it again to test.", ""), nil
		}
	}
	if typed.User != "" {
		cred.User = typed.User
	}
	if typed.KeyID != "" {
		cred.KeyID = typed.KeyID
	}
	if typed.Password != "" {
		cred.Password = typed.Password
	}
	if typed.Token != "" {
		cred.Token = typed.Token
	}
	if typed.Secret != "" {
		cred.Secret = typed.Secret
	}
	a.secrets.Redactor().AddCredential(cred)
	// The test never reads a read-only flag a person might have off.
	res := connections.Test(a.ctxOrBackground(), rc.Def(true), cred, connections.WithTestRedactor(a.secrets.Redactor()))
	out := &TestResultView{OK: res.OK, Status: string(res.Status), Ms: res.Latency.Milliseconds(), Count: res.Count, Detail: res.Detail, InstallExtension: res.InstallExtension}
	if res.OK {
		out.Summary = a.redact(res.Detail)
		return out, nil
	}
	out.Error = classifyTestError(res, rc.Resolved)
	out.Error.Details = a.redact(res.Error)
	out.Error.Message = a.redact(out.Error.Message)
	return out, nil
}

// ListMotherDuckDatabasesRequest asks which databases a MotherDuck token can
// attach. Credentials are what the person just typed (write-only: never
// returned); with none typed, the sign-in method in effect supplies the token
// (keychain, or an env: variable), like Test.
type ListMotherDuckDatabasesRequest struct {
	// Name is the connection being edited ("" for a new one).
	Name        string          `json:"name"`
	Scope       string          `json:"scope"`
	SignIn      string          `json:"signIn"`
	Credentials CredentialInput `json:"credentials"`
	// Install is the person's consent to download the MotherDuck extension.
	Install bool `json:"install"`
}

// MotherDuckDatabase is one entry of the database picker.
type MotherDuckDatabase struct {
	Name  string `json:"name"`
	Share bool   `json:"share"`
}

// MotherDuckDatabasesView is what ListMotherDuckDatabases returns. Nothing in
// it is a secret.
type MotherDuckDatabasesView struct {
	OK        bool                 `json:"ok"`
	Status    string               `json:"status"`
	Databases []MotherDuckDatabase `json:"databases"`
	// Source says where the token came from ("typed", "keychain", "env"),
	// never the token itself.
	Source string `json:"source,omitempty"`
	// Message is a redacted plain-language reason when OK is false.
	Message          string `json:"message,omitempty"`
	InstallExtension string `json:"installExtension,omitempty"`
}

// ListMotherDuckDatabases lists the MotherDuck databases and shares the token
// can attach, in a subprocess (a process holds one MotherDuck token), with the
// same 10 second limit as Test. It never touches the workspace's own session.
func (a *App) ListMotherDuckDatabases(req ListMotherDuckDatabasesRequest) (*MotherDuckDatabasesView, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	empty := func(status, msg string) *MotherDuckDatabasesView {
		return &MotherDuckDatabasesView{Status: status, Databases: []MotherDuckDatabase{}, Message: a.redact(msg)}
	}
	typed := req.Credentials.toCredential()
	if strings.HasPrefix(req.SignIn, "env:") {
		typed = secrets.Credential{} // an environment sign-in never uses a typed token
	}
	a.secrets.Redactor().AddCredential(typed)
	cred, source := secrets.Credential{Token: typed.Token}, "typed"
	if typed.Token == "" {
		var ok bool
		if cred, source, ok = a.savedMotherDuckToken(ws, req); !ok {
			return empty(string(session.StatusNeedsCredentials), "Enter a MotherDuck token, or choose an environment variable that holds one, to load your databases."), nil
		}
	}
	a.secrets.Redactor().AddCredential(cred)
	res := connections.ListMotherDuckDatabases(a.ctxOrBackground(), cred, connections.WithInstall(req.Install), connections.WithTestRedactor(a.secrets.Redactor()))
	out := &MotherDuckDatabasesView{OK: res.OK, Status: string(res.Status), Databases: []MotherDuckDatabase{}, Source: source, InstallExtension: res.InstallExtension}
	for _, d := range res.Databases {
		out.Databases = append(out.Databases, MotherDuckDatabase{Name: d.Name, Share: d.Share})
	}
	if !res.OK {
		out.Message = a.redact(listFailureMessage(res))
	}
	return out, nil
}

func listFailureMessage(res connections.MDDatabasesResult) string {
	switch {
	case res.InstallExtension != "" || res.Status == session.StatusExtensionMissing:
		return "Vibe's database engine needs the motherduck extension, which isn't installed yet. Install it, then load again."
	case res.Status == session.StatusNeedsCredentials:
		return "MotherDuck turned down this token. Check it and try again."
	case strings.Contains(strings.ToLower(res.Error), "timed out"):
		return "No answer from MotherDuck after 10 seconds. Check your network connection."
	}
	return "Couldn't load your MotherDuck databases. You can still type the database name. " + res.Error
}

// savedMotherDuckToken is the token the sign-in method in effect supplies when
// none was typed: the keychain or prompt entry of the connection being edited,
// or an env: variable. It is looked up against the saved definition, not the
// form (the Database field is what this call is for choosing).
func (a *App) savedMotherDuckToken(ws workspace.Workspace, req ListMotherDuckDatabasesRequest) (secrets.Credential, string, bool) {
	method := req.SignIn
	var saved *workspace.Connection
	if c, ok := ws.Connection(req.Name); ok && req.Name != "" {
		saved = &c
		if method == "" {
			method = c.SignIn
		}
	}
	if method == "" {
		method = "keychain"
	}
	kind, _, perr := secrets.ParseMethod(method)
	if perr != nil {
		return secrets.Credential{}, "", false
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	switch {
	case kind == "env":
		ref := secrets.Ref{Scope: secrets.ScopeWorkspace, Name: "motherduck"}
		cred, err := ws.Resolver().Resolve(ctx, ref, method, secrets.Binding{Type: "motherduck"})
		if err != nil || cred.Token == "" {
			return secrets.Credential{}, "", false
		}
		return cred, "env", true
	case saved != nil && saved.Type == "motherduck":
		rc, err := workspace.ResolveConnection(ws, *saved, nil)
		if err != nil {
			return secrets.Credential{}, "", false
		}
		cred, err := ws.Resolver().Resolve(ctx, workspace.CredentialRef(ws.ID(), rc.Scope, saved.Name), method, rc.Binding())
		if err != nil || cred.Token == "" {
			return secrets.Credential{}, "", false
		}
		return cred, kind, true
	}
	return secrets.Credential{}, "", false
}

// testBaseCredential is the saved credential a test starts from: this person's
// sign-in by the method in effect. It returns serverChanged (and no credential)
// when the definition being tested differs, in any fingerprint field, from the
// saved one, or when the saved sign-in is no longer bound to the saved
// definition (a pull changed it): a stored credential is then never used, by any
// method. A connection that does not exist yet has nothing stored, but an
// environment sign-in the person chose for it is theirs to use.
func (a *App) testBaseCredential(ws workspace.Workspace, rc workspace.ResolvedConnection, def workspace.ConnectionDef, signIn string) (secrets.Credential, bool) {
	method := signIn
	var saved *workspace.Connection
	if c, ok := ws.Connection(def.Name); ok {
		saved = &c
		if method == "" {
			method = c.SignIn
		}
	}
	if method == "" {
		method = "keychain"
	}
	kind, _, _ := secrets.ParseMethod(method)
	if saved == nil && kind != "env" {
		return secrets.Credential{}, false
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	if saved != nil {
		savedResolved, err := saved.ConnectionDef.Resolve(nil, saved.Scope)
		if err != nil || workspace.Fingerprint(savedResolved) != workspace.Fingerprint(rc.Resolved) {
			return secrets.Credential{}, true
		}
		// The saved sign-in itself must still match its definition (the same guard
		// the session applies): keychain and prompt carry their own fingerprint,
		// an environment sign-in is bound by the choice.
		if st, err := ws.CredentialStatus(ctx, def.Name); err != nil || !st.FingerprintMatches {
			return secrets.Credential{}, true
		}
		if kind == "env" && saved.SignInFingerprint != workspace.Fingerprint(savedResolved) {
			return secrets.Credential{}, true
		}
	}
	ref := workspace.CredentialRef(ws.ID(), rc.Scope, def.Name)
	cred, err := ws.Resolver().Resolve(ctx, ref, method, rc.Binding())
	if err != nil {
		return secrets.Credential{}, false
	}
	return cred, false
}

var (
	reHostFor = regexp.MustCompile(`host='([^']+)'`)
)

// classifyTestError maps a failed Test to a plain-language line (SPEC 06).
func classifyTestError(res connections.TestResult, def workspace.ConnectionDef) *TestError {
	raw := res.Error
	low := strings.ToLower(raw)
	host := def.Host
	if host == "" {
		if m := reHostFor.FindStringSubmatch(raw); m != nil {
			host = m[1]
		}
	}
	if host == "" {
		host = "the server"
	}
	e := &TestError{Code: "error", Message: "Vibe couldn't connect with these settings."}
	switch {
	case res.InstallExtension != "" || res.Status == session.StatusExtensionMissing:
		e.Code = "extension-missing"
		e.Message = fmt.Sprintf("Vibe's database engine needs the %s extension, which isn't installed yet. Install it, then test again.", orStr(res.InstallExtension, "missing"))
	case strings.Contains(low, "timed out"):
		e.Code = "timeout"
		e.Message = "No answer after 10 seconds. Check the address and whether you need a VPN."
	case strings.Contains(low, "password authentication failed") || strings.Contains(low, "authentication failed") ||
		strings.Contains(low, "invalid token") || strings.Contains(low, "not authenticated") || res.Status == session.StatusNeedsCredentials:
		e.Code = "bad-signin"
		e.Message = fmt.Sprintf("The server turned down the username or password. Check them and try again. If you're sure they're right, ask whoever runs the database whether your account can reach %s.", host)
		if def.Type == "motherduck" || strings.HasPrefix(def.Type, "s3") || def.Type == "gcs" || def.Type == "r2" || def.Type == "azure" {
			e.Message = "The service turned down these keys or this token. Check them and try again."
		}
	case strings.Contains(low, "does not exist") && strings.Contains(low, "database"):
		e.Code = "db-not-found"
		e.Message = fmt.Sprintf("The server answered, but it has no database called %s.", orStr(def.Database, "that"))
	case strings.Contains(low, "ssl") && (strings.Contains(low, "require") || strings.Contains(low, "not supported")):
		e.Code = "ssl-required"
		e.Message = "The server needs an encrypted connection. Open Advanced options and set SSL mode to require."
	case strings.Contains(low, "keychain"):
		e.Code = "keychain"
		e.Message = "Vibe can't reach your keychain. Use environment variables for this connection instead."
	case strings.Contains(low, "could not connect") || strings.Contains(low, "connection refused") || strings.Contains(low, "no such host") ||
		strings.Contains(low, "could not translate host") || strings.Contains(low, "network is unreachable") || strings.Contains(low, "unable to connect"):
		e.Code = "unreachable"
		e.Message = fmt.Sprintf("Vibe couldn't reach %s. Is the VPN on?", host)
	}
	return e
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---- extensions ----

// ExtensionInfo is one DuckDB extension a connection type needs.
type ExtensionInfo struct {
	Name      string   `json:"name"`
	Purpose   string   `json:"purpose"`
	NeededBy  []string `json:"neededBy"` // connections in this workspace
	Installed bool     `json:"installed"`
}

var extensionPurpose = map[string]string{
	"postgres":   "Read PostgreSQL databases.",
	"httpfs":     "Read files from S3, Google Cloud Storage and R2.",
	"sqlite":     "Read SQLite files.",
	"motherduck": "Connect to MotherDuck.",
	"mysql":      "Read MySQL and MariaDB databases.",
	"azure":      "Read files from Azure Blob Storage and Data Lake.",
	"ducklake":   "Read and write DuckLake lakehouses.",
	"iceberg":    "Read Iceberg tables.",
	"delta":      "Read Delta Lake tables.",
}

// ListExtensions lists the extensions the connection types need, whether each is
// installed, and which of this workspace's connections need it. It does not
// need an open workspace (App settings show it anyway).
func (a *App) ListExtensions() ([]ExtensionInfo, error) {
	needed := map[string][]string{}
	var ws workspace.Workspace
	a.mu.RLock()
	ws = a.ws
	a.mu.RUnlock()
	if ws != nil {
		for _, c := range ws.Connections() {
			if ti, err := connections.TypeInfoFor(c.Type); err == nil {
				for _, e := range ti.Extensions {
					needed[e] = append(needed[e], c.Name)
				}
			}
		}
	}
	names := []string{}
	for _, ti := range connections.ListConnectionTypes() {
		for _, e := range ti.Extensions {
			dup := false
			for _, n := range names {
				dup = dup || n == e
			}
			if !dup {
				names = append(names, e)
			}
		}
	}
	sort.Strings(names)
	installed := a.installedExtensions(names)
	out := make([]ExtensionInfo, 0, len(names))
	for _, n := range names {
		nb := needed[n]
		if nb == nil {
			nb = []string{}
		}
		sort.Strings(nb)
		out = append(out, ExtensionInfo{Name: n, Purpose: extensionPurpose[n], NeededBy: nb, Installed: installed[n]})
	}
	return out, nil
}

// installedExtensions asks a throwaway DuckDB that uses the app's private
// extension directory which of names are installed there.
func (a *App) installedExtensions(names []string) map[string]bool {
	out := map[string]bool{}
	dir, _ := session.DefaultDirs()
	db, err := session.OpenDB("", session.WithExtensionDirectory(dir))
	if err != nil {
		return out
	}
	defer db.Close() //nolint:errcheck
	return session.ExtensionsInstalled(context.Background(), session.FromDB(db), names)
}

// InstallExtensionRequest is the person's consent to download one extension.
type InstallExtensionRequest struct {
	Name string `json:"name"`
}

// InstallExtension downloads an extension a connection type needs, after the
// person said yes in the consent dialog (never automatic), then re-checks the
// connections that were waiting for it.
func (a *App) InstallExtension(req InstallExtensionRequest) error {
	_, sess, _, err := a.snapshot()
	if err != nil {
		return err
	}
	a.emitEvent("extension:progress", map[string]any{"name": req.Name, "state": "installing"})
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 5*time.Minute)
	defer cancel()
	if err := sess.InstallExtension(ctx, req.Name); err != nil {
		a.emitEvent("extension:progress", map[string]any{"name": req.Name, "state": "error"})
		return errors.New(a.redact(err.Error()))
	}
	for _, st := range sess.Connections() {
		if st.Kind == session.StatusExtensionMissing {
			sess.Preflight(ctx, st.Name)
			a.emitStatus(sess, st.Name)
		}
	}
	a.emitEvent("extension:progress", map[string]any{"name": req.Name, "state": "done"})
	a.emitEvent("workspace:changed", map[string]any{"kind": "connections"})
	return nil
}

// ---- after a run ----

var leadingNoise = regexp.MustCompile(`^(?:\s+|--[^\n]*(?:\n|$)|/\*(?s:.*?)\*/)+`)

// isReadOnlyStatement reports whether sqlText starts like a statement that
// cannot change a catalog (SELECT, WITH, FROM, EXPLAIN, DESCRIBE, SHOW, VALUES,
// SUMMARIZE). Anything else (CREATE, DROP, ALTER, INSERT, ATTACH, ...) may change
// what the rail lists.
func isReadOnlyStatement(sqlText string) bool {
	t := leadingNoise.ReplaceAllString(sqlText, "")
	t = strings.TrimLeft(t, "( \t\r\n")
	end := strings.IndexFunc(t, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '(' || r == ';' })
	if end < 0 {
		end = len(t)
	}
	switch strings.ToLower(t[:end]) {
	case "select", "with", "from", "explain", "describe", "show", "values", "summarize":
		return true
	}
	return false
}

// statusSnapshot records every connection's state, to tell after a run which
// ones a statement dialled (lazy attach) or changed.
func statusSnapshot(sess session.Session) map[string]session.StatusKind {
	out := map[string]session.StatusKind{}
	for _, st := range sess.Connections() {
		out[st.Name] = st.Kind
	}
	return out
}

// afterRun tells the rail what a run changed: a connection that attached (or
// failed) gets its status refreshed, and anything that is not a plain read, or
// that attached something, emits catalog:changed so open levels reload.
func (a *App) afterRun(sess session.Session, before map[string]session.StatusKind, sqlText string) {
	changed := false
	for _, st := range sess.Connections() {
		if before[st.Name] != st.Kind {
			changed = true
			a.emitStatus(sess, st.Name)
		}
	}
	if changed || !isReadOnlyStatement(sqlText) {
		a.emitEvent("catalog:changed", map[string]any{"conn": ""})
	}
}
