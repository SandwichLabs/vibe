// Package sessiontest builds Sessions for the tests of packages that depend on
// one. It is for _test.go files only: production code must use session.New, which
// hardens the instance and fails closed
// (internal/session's TestUnhardenedOnlyUsedByTests enforces this).
package sessiontest

import (
	"database/sql"

	"github.com/SandwichLabs/vibe/internal/session"
)

// ForTests wraps a bare *sql.DB (for example sql.Open("duckdb", "")) in a
// Session without hardening or the configuration lock.
func ForTests(db *sql.DB, opts ...session.Option) session.Session {
	return session.UnhardenedForTests(db, opts...)
}

// ScopeAcceptingType is a catalog-less connection type that accepts any Params
// (a "scope" among them) and is NOT a storage type: the session must export no
// scope for it, however its definition is spelled.
type ScopeAcceptingType struct{ TypeName string }

func (t ScopeAcceptingType) Name() string                             { return t.TypeName }
func (t ScopeAcceptingType) Remote() bool                             { return true }
func (t ScopeAcceptingType) Catalogless() bool                        { return true }
func (t ScopeAcceptingType) Validate(session.Spec) error              { return nil }
func (t ScopeAcceptingType) AttachSQL(session.Spec) ([]string, error) { return nil, nil }

// StorageScopedType is the same shape but declares itself a storage type, so
// its scope is exported.
type StorageScopedType struct{ ScopeAcceptingType }

func (StorageScopedType) StorageScope() bool { return true }
