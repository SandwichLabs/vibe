package artifacts

import (
	"fmt"
	"sort"
	"time"

	"github.com/SandwichLabs/assay/internal/engine"
)

// Bind turns (manifest, query name, raw values, now) into the statement to run
// and the values to bind to it. The SQL returned is the manifest's text, byte
// for byte: a value never becomes SQL text, a path, an identifier or anything
// but an entry of the returned params map, which the engine hands to the
// database as prepared-statement arguments. Names and values are all that
// crosses from a view.
//
// An unknown parameter name, a value of the wrong type, a text over its length,
// a number outside min/max, a date that is not a real YYYY-MM-DD, or an enum
// value outside its static options is refused. A parameter missing from values
// takes its default (dates resolved on now's clock). Every supplied value is
// checked even when this query does not use it; params holds the ones the
// query uses. The set a query uses is re-derived from its SQL here, not read
// from the manifest's cached Uses.
func Bind(m *Manifest, queryName string, values map[string]any, now time.Time) (sql, connection string, params map[string]any, err error) {
	q := findQuery(m, queryName)
	if q == nil {
		return "", "", nil, invalid("there is no query named %q", queryName)
	}
	byName := make(map[string]*Param, len(m.Parameters))
	for i := range m.Parameters {
		byName[m.Parameters[i].Name] = &m.Parameters[i]
	}
	checked, err := coerceSupplied(byName, values)
	if err != nil {
		return "", "", nil, err
	}
	params = map[string]any{}
	for _, name := range engine.ParamNames(q.SQL) {
		p, ok := byName[name]
		if !ok {
			return "", "", nil, invalid("query %q uses $%s, which is not a declared parameter", q.Name, name)
		}
		v, have := checked[name]
		if !have {
			if v, err = defaultValue(p, now); err != nil {
				return "", "", nil, invalid("%v", err)
			}
		}
		params[name] = v
	}
	return q.SQL, q.Connection, params, nil
}

func findQuery(m *Manifest, name string) *Query {
	for i := range m.Queries {
		if m.Queries[i].Name == name {
			return &m.Queries[i]
		}
	}
	return nil
}

// coerceSupplied validates every supplied value (in name order, so the error
// is stable) and returns them in their bound Go types.
func coerceSupplied(byName map[string]*Param, values map[string]any) (map[string]any, error) {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make(map[string]any, len(values))
	for _, n := range names {
		p, ok := byName[n]
		if !ok {
			return nil, invalid("there is no parameter named %q", n)
		}
		v, err := coerceValue(p, values[n])
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		out[n] = v
	}
	return out, nil
}
