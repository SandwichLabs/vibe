package engine

import (
	"fmt"
	"strings"
)

// CapRows wraps one SELECT/WITH statement (no trailing semicolon) in a
// subquery that returns at most limit rows, so a huge table is never fully
// materialised by a caller that only displays a page of it.
func CapRows(sql string, limit int) string {
	return fmt.Sprintf("SELECT * FROM (%s) AS _vibe_repl_cap LIMIT %d", sql, limit)
}

// CapRowsIfSafe applies CapRows when it is safe to: readOnly (decided by the
// caller from agents.IsReadOnlyStatement, never a prefix guess), a single
// statement, starting with SELECT or WITH, and with no LIMIT of its own.
// Anything else (SHOW/DESCRIBE/PRAGMA/EXPLAIN/SUMMARIZE, a multi-statement
// input, a write, an empty string) is returned untouched.
func CapRowsIfSafe(query string, readOnly bool, limit int) string {
	if !readOnly {
		return query
	}
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), ";")
	if trimmed == "" || strings.Contains(trimmed, ";") {
		return query
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return query
	}
	if strings.Contains(upper, "LIMIT") {
		return query
	}
	return CapRows(trimmed, limit)
}
