package engine

import "strings"

// IsReadOnlyViolation reports whether err is DuckDB refusing a write because
// the catalog is attached read-only. DuckDB returns a plain error, so its
// wording is read here, once, not in an adapter.
func IsReadOnlyViolation(err error) bool {
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "read-only") || strings.Contains(low, "readonly") || strings.Contains(low, "read only")
}
