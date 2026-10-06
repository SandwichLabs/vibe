package workspace

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envRefRE = regexp.MustCompile(`\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// LookupFunc resolves an environment variable.
type LookupFunc func(name string) (string, bool)

// OSLookup is os.LookupEnv.
func OSLookup(name string) (string, bool) { return os.LookupEnv(name) }

// Interpolate expands ${NAME} in s. A reference to an unset variable is a
// hard error (an empty value is fine: it is set). "$$" is a literal "$". A bare
// "$NAME" is left alone: only the braced form is a reference.
func Interpolate(s string, lookup LookupFunc) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}
	if lookup == nil {
		lookup = OSLookup
	}
	var missing []string
	out := envRefRE.ReplaceAllStringFunc(s, func(m string) string {
		if m == "$$" {
			return "$"
		}
		name := m[2 : len(m)-1]
		v, ok := lookup(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("environment variable %s is not set", strings.Join(missing, ", "))
	}
	return out, nil
}

// Resolve expands ${ENV} references in the non-secret fields of a definition:
// path, host, database, region, scope and option values. The name, type and
// credentials fields are never interpolated (a definition has no secret
// fields, and an environment variable must never be able to rewrite what a
// connection is called or how it signs in). A missing variable is an error
// naming the connection and field, never the value.
//
// Interpolation is only for the scopes that belong to one person (local and
// user): the shared vibe.yaml is written by someone else and its values are
// used as written (review #109, D-B). A variable whose name looks like a
// credential is refused even there.
func (d ConnectionDef) Resolve(lookup LookupFunc, scope Scope) (ConnectionDef, error) {
	if scope == ScopeShared {
		if err := d.ValidateIn(ScopeShared); err != nil {
			return ConnectionDef{}, err
		}
		return d, nil
	}
	if err := d.ValidateIn(scope); err != nil {
		return ConnectionDef{}, err
	}
	out := d
	fields := []struct {
		name string
		dst  *string
	}{{"path", &out.Path}, {"host", &out.Host}, {"database", &out.Database}, {"region", &out.Region}, {"scope", &out.Scope}}
	for _, f := range fields {
		v, err := Interpolate(*f.dst, lookup)
		if err != nil {
			return ConnectionDef{}, fmt.Errorf("connection %q: %s: %w", d.Name, f.name, err)
		}
		*f.dst = v
	}
	if len(d.Options) > 0 {
		out.Options = make(map[string]string, len(d.Options))
		for k, v := range d.Options {
			ev, err := Interpolate(v, lookup)
			if err != nil {
				return ConnectionDef{}, fmt.Errorf("connection %q: option %q: %w", d.Name, k, err)
			}
			out.Options[k] = ev
		}
	}
	// An expanded value must still not smuggle in a password or a newline.
	if err := out.Validate(); err != nil {
		return ConnectionDef{}, fmt.Errorf("after expanding environment variables: %w", err)
	}
	return out, nil
}
