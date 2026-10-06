package session

import (
	"fmt"
	"os"
	"sort"
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
