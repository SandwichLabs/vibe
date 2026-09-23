package engine

import "strings"

// QuoteIdent quotes a DuckDB identifier (table or column name) for safe
// interpolation into a SQL string. This is the ONE identifier quoter for the
// whole codebase — every fmt.Sprintf that builds a SQL statement with a
// table/column name must go through this, never Go's `%q` verb.
//
// Go's `%q` is a Go string literal quoter: it escapes with backslashes
// (`"a\"b"`). DuckDB identifiers are double-quoted SQL identifiers, whose
// only escape is doubling the quote character (`"a""b"`) — DuckDB does not
// interpret a backslash inside a quoted identifier as an escape at all, so
// `%q`-quoted input containing a literal backslash-quote sequence can close
// the identifier early and splice attacker-controlled SQL into the
// statement (proven: a column or table name containing a `"` let a crafted
// CSV header or a shared .vibe file's stored table name run an arbitrary
// COPY ... TO '<path>', an arbitrary-file-write primitive). See
// internal/agents/skills_test.go's injection regression tests.
func QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}
