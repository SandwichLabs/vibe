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
	"github.com/sandwich-labs/vibe-app/internal/storageurl"
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
	case "mysql":
		port := d.Port
		if port == 0 {
			port = 3306
		}
		return net.JoinHostPort(d.Host, strconv.Itoa(port)) + "/" + d.Database
	case "s3", "gcs", "r2", "azure":
		return d.Scope
	case "sqlite", "duckdb", "ducklake":
		return filepath.Base(d.Path)
	case "iceberg", "delta":
		if connections.IsDataURL(d.Path) {
			return d.Path
		}
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
	if c.Scope == ScopeShared && OwnScopeOnlyType(c.Type) {
		// Skipped, not fatal: the rest of the workspace opens and this one is
		// reported as an issue.
		return ResolvedConnection{}, fmt.Errorf("connection %q: %s. This one is in the team's vibe.yaml: ask the person who committed it to move it to their own connections", c.Name, OwnScopeOnlyMessage(c.Type))
	}
	resolved, err := c.ConnectionDef.Resolve(lookup, c.Scope)
	if err != nil {
		return ResolvedConnection{}, err
	}
	out := ResolvedConnection{Connection: c, Resolved: resolved}
	if usesLocalPath(resolved) {
		p, err := ResolveLocalPath(ws.Root(), resolved.Path, c.Scope)
		if err != nil {
			return ResolvedConnection{}, fmt.Errorf("connection %q: %w", c.Name, err)
		}
		out.Path = p
	} else if resolved.Type == "iceberg" || resolved.Type == "delta" {
		out.Path = resolved.Path // a storage URL: read with a storage connection's secret
	}
	if dp := resolved.Options["data_path"]; resolved.Type == "ducklake" && dp != "" && !connections.IsDataURL(dp) {
		abs, err := ResolveLocalPath(ws.Root(), dp, c.Scope)
		if err != nil {
			return ResolvedConnection{}, fmt.Errorf("connection %q: data path: %w", c.Name, err)
		}
		opts := make(map[string]string, len(resolved.Options))
		for k, v := range resolved.Options {
			opts[k] = v
		}
		opts["data_path"] = abs
		out.Resolved.Options = opts
	}
	if err := checkStorageURLs(ws, out.Resolved, out.Path, lookup); err != nil {
		return ResolvedConnection{}, fmt.Errorf("connection %q: %w", c.Name, err)
	}
	return out, nil
}

// storageTypes are the connection types that add a scoped storage secret.
var storageTypes = map[string]bool{"s3": true, "gcs": true, "r2": true, "azure": true}

// checkStorageURLs holds a DuckLake data path and an Iceberg or Delta location
// that is a URL to the scope of a storage connection of this workspace: the
// lake or table is read with that connection's secret, and a definition that
// named any other bucket would either fail or reach data nobody configured.
// Local paths are held to the workspace by ResolveLocalPath.
func checkStorageURLs(ws Workspace, d ConnectionDef, path string, lookup LookupFunc) error {
	var url, what string
	switch d.Type {
	case "ducklake":
		url, what = d.Options["data_path"], "data path"
	case "iceberg", "delta":
		url, what = path, "location"
	}
	if url == "" || !connections.IsDataURL(url) {
		return nil
	}
	if err := storageurl.Validate(url); err != nil {
		return fmt.Errorf("the %s is not a plain storage URL: %w", what, err)
	}
	for _, c := range ws.Connections() {
		if !storageTypes[c.Type] || c.Name == d.Name {
			continue
		}
		sd, err := c.ConnectionDef.Resolve(lookup, c.Scope)
		if err != nil || sd.Scope == "" {
			continue
		}
		// One normaliser, shared with the agent's URL rule: a literal prefix
		// match on the scope's folder form.
		if storageurl.Within(url, sd.Scope) {
			return nil
		}
	}
	return fmt.Errorf("the %s %q is not inside the scope of any S3, GCS, R2 or Azure connection of this workspace; add one whose scope contains it", what, url)
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

// usesLocalPath reports whether the definition's path is a file or folder on
// this computer that must be resolved against the workspace: duckdb, sqlite and
// ducklake files, and the folder of an iceberg or delta table (a storage URL is
// left alone).
func usesLocalPath(d ConnectionDef) bool {
	switch d.Type {
	case "duckdb", "sqlite", "ducklake":
		return true
	case "iceberg", "delta":
		return !connections.IsDataURL(d.Path)
	}
	return false
}
