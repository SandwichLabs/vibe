// Package connname holds the connection-name rule (the name is the catalog
// alias in SQL). It is a dependency-free leaf so stores can validate a name
// without importing the DuckDB-backed session package.
package connname

import (
	"fmt"
	"regexp"
)

var re = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Valid reports whether name satisfies the catalog-alias rule (plan §4.2).
func Valid(name string) bool { return re.MatchString(name) }

// Validate returns a descriptive error when name is not a valid connection name.
func Validate(name string) error {
	if !Valid(name) {
		return fmt.Errorf("invalid connection name %q: must match %s", name, re.String())
	}
	return nil
}
