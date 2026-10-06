// Package sessiontest builds Sessions for the tests of packages that depend on
// one. It is for _test.go files only: production code must use session.New or
// session.Wrap, which harden the instance and fail closed
// (internal/session's TestUnhardenedOnlyUsedByTests enforces this).
package sessiontest

import (
	"database/sql"

	"github.com/sandwich-labs/vibe-app/internal/session"
)

// ForTests wraps a bare *sql.DB (for example sql.Open("duckdb", "")) in a
// Session without hardening or the configuration lock.
func ForTests(db *sql.DB, opts ...session.Option) session.Session {
	return session.UnhardenedForTests(db, opts...)
}
