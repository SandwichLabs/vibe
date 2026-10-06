package session

import (
	"fmt"
	"os"
	"sort"

	"github.com/sandwich-labs/vibe-app/internal/secrets"
)

// ConnectionType is the seam #115 extends: each type validates its Spec and
// emits its attach SQL. Rule (plan §4.2): a DSN or credential never appears in
// an ATTACH string, and every identifier and literal goes through QuoteIdent /
// QuoteLiteral. v1 of the seam only knows the file-based duckdb type, which
// needs no secret.
type ConnectionType interface {
	Name() string
	// Validate checks the Spec's type-specific fields (no network).
	Validate(Spec) error
	// AttachSQL returns the statements that attach the catalog.
	AttachSQL(Spec) ([]string, error)
	// Remote reports whether the catalog's schema can change outside this
	// process (the schema cache then trusts only explicit invalidation).
	Remote() bool
}

// SecretStep is one CREATE TEMPORARY SECRET a type needs before its attach.
// Name is a secret name (ValidateName), Type DuckDB's secret type.
type SecretStep struct {
	Name   string
	Type   string
	Params []SecretParam
}

// CredentialedType is implemented by types that reach a remote service: they
// need an extension, usually a credential, and emit secret DDL separately from
// the attach statement so that no credential can ever appear in an ATTACH
// string (plan §4.2).
type CredentialedType interface {
	ConnectionType
	// Extensions lists the DuckDB extensions the type needs (names as
	// duckdb_extensions() knows them, or their aliases). The session loads
	// them when installed and reports extension-missing when not; it never
	// installs on its own (consent).
	Extensions() []string
	// NeedsCredential reports whether Connect must resolve a credential.
	NeedsCredential(Spec) bool
	// SecretSteps returns the secret DDL for the resolved credential. The
	// session runs it on its dedicated non-logging path.
	SecretSteps(Spec, secrets.Credential) ([]SecretStep, error)
}

// CataloglessType is implemented by storage connections (s3, gcs, r2): they
// only create secrets and attach no catalog.
type CataloglessType interface {
	ConnectionType
	Catalogless() bool
}

// HubTokenType is implemented by MotherDuck: its token is a hub-wide setting
// that must be in place before the configuration is locked (spike #107).
type HubTokenType interface {
	ConnectionType
	UsesHubToken() bool
}

// Refresher is implemented by remote types whose catalog caches its own
// metadata (Postgres: CALL pg_clear_cache()).
type Refresher interface {
	RefreshSQL() string
}

// Registry maps type names to implementations.
type Registry struct {
	types map[string]ConnectionType
}

// NewRegistry returns a registry holding the built-in types.
func NewRegistry() *Registry {
	r := &Registry{types: map[string]ConnectionType{}}
	r.Register(duckdbFileType{})
	return r
}

// Register adds or replaces a type.
func (r *Registry) Register(t ConnectionType) { r.types[t.Name()] = t }

// Get returns a type by name.
func (r *Registry) Get(name string) (ConnectionType, error) {
	t, ok := r.types[name]
	if !ok {
		return nil, fmt.Errorf("unknown connection type %q", name)
	}
	return t, nil
}

// Names lists registered type names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.types))
	for n := range r.types {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// duckdbFileType attaches a local DuckDB file: ATTACH 'path' AS name (READ_ONLY).
type duckdbFileType struct{}

func (duckdbFileType) Name() string { return "duckdb" }
func (duckdbFileType) Remote() bool { return false }

func (duckdbFileType) Validate(s Spec) error {
	if s.Path == "" {
		return fmt.Errorf("duckdb connection %q needs a path", s.Name)
	}
	return nil
}

func (duckdbFileType) AttachSQL(s Spec) ([]string, error) {
	if s.ReadOnly {
		// A read-only attach of a missing file is a confusing DuckDB error.
		if _, err := os.Stat(s.Path); err != nil {
			return nil, fmt.Errorf("database file for %q is not readable: %w", s.Name, err)
		}
	}
	stmt := "ATTACH " + QuoteLiteral(s.Path) + " AS " + QuoteIdent(s.Name)
	if s.ReadOnly {
		stmt += " (READ_ONLY)"
	}
	return []string{stmt}, nil
}
