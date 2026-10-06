package workspace

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/connections"
	"github.com/sandwich-labs/vibe-app/internal/secrets"
)

// SnapshotEnv returns an environment lookup over a copy of the process
// environment taken now. Build the secrets Resolver with it
// (secrets.WithEnv): opening a MotherDuck connection removes MOTHERDUCK_TOKEN
// from the live environment (so a stray token is never picked up silently),
// and an env:MOTHERDUCK sign-in must keep working afterwards.
func SnapshotEnv() func(string) string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return func(k string) string { return m[k] }
}

// credentialRef is the keychain reference of a connection's credential: user
// connections are keyed by name alone, workspace ones (shared and local) by
// workspace id and name. A move between those two kinds migrates the entry.
func credentialRef(wsID string, scope Scope, name string) secrets.Ref {
	if scope == ScopeUser {
		return secrets.UserRef(name)
	}
	return secrets.WorkspaceRef(wsID, name)
}

// ServerDescription is the host-level, non-secret description stored beside a
// credential for the "server changed" message.
func ServerDescription(d ConnectionDef) string {
	switch d.Type {
	case "postgres":
		port := d.Port
		if port == 0 {
			port = 5432
		}
		return net.JoinHostPort(d.Host, strconv.Itoa(port)) + "/" + d.Database
	case "motherduck":
		return "md:" + d.Database
	case "s3", "gcs", "r2":
		return d.Scope
	case "sqlite", "duckdb":
		return filepath.Base(d.Path)
	}
	return d.Type
}

// BindingFor is the fingerprint binding of an environment-resolved definition.
func BindingFor(d ConnectionDef) secrets.Binding {
	return secrets.Binding{Fingerprint: Fingerprint(d), Server: ServerDescription(d)}
}

// toConnDef converts a resolved definition into the type registry's shape.
// path is the resolved, absolute file path for file types.
func toConnDef(d ConnectionDef, readOnly bool, path string) connections.Def {
	return connections.Def{
		Name: d.Name, Type: d.Type, Path: path, Host: d.Host, Port: d.Port, Database: d.Database,
		Region: d.Region, Scope: d.Scope, ReadOnly: readOnly, Options: d.Options,
	}
}

// signInGuard withholds an env: credential unless this person's sign-in was
// chosen for exactly this definition. A keychain or prompt credential carries
// its own fingerprint (checked by the resolver); an environment sign-in has no
// stored value, so the binding lives with the choice (review #115).
func signInGuard(c Connection, resolved ConnectionDef) error {
	if kind, _, err := secrets.ParseMethod(c.SignIn); err != nil || kind != "env" {
		return nil //nolint:nilerr // an unparseable method is rejected by the resolver
	}
	if c.SignInFingerprint != "" && c.SignInFingerprint == Fingerprint(resolved) {
		return nil
	}
	return &secrets.ErrFingerprintChanged{Old: c.SignInServer, New: ServerDescription(resolved)}
}

// ResolvedConnection is a merged connection with its ${ENV} references
// expanded and its file path resolved.
type ResolvedConnection struct {
	Connection
	Resolved ConnectionDef
	// Path is the absolute file path for duckdb and sqlite connections.
	Path string
}

// Def converts the resolved connection for the type registry. readOnly is the
// effective flag (the definition's, or forced by a read-only open).
func (r ResolvedConnection) Def(readOnly bool) connections.Def {
	return toConnDef(r.Resolved, readOnly, r.Path)
}

// Binding is the credential binding of the resolved definition.
func (r ResolvedConnection) Binding() secrets.Binding { return BindingFor(r.Resolved) }

// ResolveConnection expands and path-resolves one connection of ws.
func ResolveConnection(ws Workspace, c Connection, lookup LookupFunc) (ResolvedConnection, error) {
	resolved, err := c.ConnectionDef.Resolve(lookup, c.Scope)
	if err != nil {
		return ResolvedConnection{}, err
	}
	out := ResolvedConnection{Connection: c, Resolved: resolved}
	if resolved.Type == "duckdb" || resolved.Type == "sqlite" {
		p, err := ResolveLocalPath(ws.Root(), resolved.Path, c.Scope)
		if err != nil {
			return ResolvedConnection{}, fmt.Errorf("connection %q: %w", c.Name, err)
		}
		out.Path = p
	}
	return out, nil
}

// ConnectionCredential resolves the person's credential for one connection of
// ws by their sign-in method. It is a Go-level helper for Test and the CLI; it
// is never bound across the Wails bridge, MCP or the agent (write-only). The
// result is registered with the resolver's redactor.
func ConnectionCredential(ctx context.Context, ws Workspace, name string, lookup LookupFunc) (secrets.Credential, ResolvedConnection, error) {
	for _, c := range ws.Connections() {
		if c.Name != name {
			continue
		}
		rc, err := ResolveConnection(ws, c, lookup)
		if err != nil {
			return secrets.Credential{}, ResolvedConnection{}, err
		}
		if !connections.NeedsCredential(rc.Resolved.Type) {
			return secrets.Credential{}, rc, nil
		}
		if err := signInGuard(c, rc.Resolved); err != nil {
			return secrets.Credential{}, rc, err
		}
		cred, err := ws.Resolver().Resolve(ctx, credentialRef(ws.ID(), c.Scope, c.Name), c.SignIn, rc.Binding())
		return cred, rc, err
	}
	return secrets.Credential{}, ResolvedConnection{}, fmt.Errorf("unknown connection %q", name)
}
