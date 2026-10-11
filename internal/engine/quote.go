package engine

import "github.com/SandwichLabs/vibe/internal/session"

// QuoteIdent quotes a DuckDB identifier for safe interpolation into SQL. It is
// session.QuoteIdent, kept here because every package that builds SQL already
// imports engine; there is ONE implementation (see its doc for why Go's %q
// verb is never acceptable).
func QuoteIdent(ident string) string { return session.QuoteIdent(ident) }
