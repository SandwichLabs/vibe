package queries

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/engine"
)

// BindOptions are the knobs of Bind.
type BindOptions struct {
	// Now is the clock a relative date default (today-30d) resolves on.
	Now time.Time
	// Strict also requires every declared name to be used, so the header never
	// disagrees with the SQL (save-time vets). Without it the SQL may use a
	// subset of the declared names (a run of an edited buffer).
	Strict bool
}

// Bind turns a saved query's declarations and the caller's values into the typed
// params to bind. It is the one binder every surface shares (the agent's
// run_saved_query, Studio's Query Explorer, `vibe query --saved`). The SQL text
// is q.SQL, unchanged: a value only ever becomes an entry of the returned map,
// never SQL text.
//
// The declarations are validated first (a hand-written header with a bad name or
// type is refused with a clear error). Every $name the SQL uses must be
// declared; an unknown value name and a value of the wrong type are refused; a
// declared parameter with no supplied value takes its default. Finally the text
// must pass CheckParamShape: engine.BindParams rewrites every "$name" it finds,
// so a placeholder in a string literal, a bare ? or $1, a second statement
// hidden in a $tag$ body, or a parameter as the argument of query() or a file
// reader would otherwise run something the caller's own checks never saw.
func Bind(q *Query, values map[string]any, o BindOptions) (map[string]any, error) {
	if errs := artifacts.ValidateParameters(withPlaceholderDefaults(ArtifactParams(q.Parameters))); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = trimInvalid(e)
		}
		return nil, errors.New("the query's parameters are not valid: " + strings.Join(msgs, "; "))
	}
	if o.Strict {
		if err := checkDeclarations(q); err != nil {
			return nil, err
		}
	}
	m := &artifacts.Manifest{
		Parameters: ArtifactParams(q.Parameters),
		Queries:    []artifacts.Query{{Name: "saved", SQL: q.SQL, Connection: q.Connection}},
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	_, _, params, err := artifacts.Bind(m, "saved", values, now)
	if err != nil {
		return nil, errors.New(strings.ReplaceAll(trimInvalid(err), `query "saved" `, "the query "))
	}
	if err := CheckParamShape(q.SQL); err != nil {
		return nil, err
	}
	return params, nil
}

// ArtifactParams maps stored parameters to the artifact kind's, so one binder
// (artifacts.Bind) coerces, defaults and bounds the values of both. A required
// parameter with no default stays without one: a run must then supply it.
func ArtifactParams(ps []Param) []artifacts.Param {
	out := make([]artifacts.Param, len(ps))
	for i := range ps {
		p := artifacts.Param{Name: ps[i].Name, Type: ps[i].Type}
		if ps[i].Default != "" || !ps[i].Required {
			d := ps[i].Default
			p.Default = &d
		}
		out[i] = p
	}
	return out
}

// withPlaceholderDefaults gives a required parameter that has no default one
// that is valid for its type, only so the declaration check (which wants every
// parameter to have a default) judges its name and type. The binder still sees
// the parameter without one, so a run must supply it.
func withPlaceholderDefaults(ps []artifacts.Param) []artifacts.Param {
	out := slices.Clone(ps)
	for i := range out {
		if out[i].Default != nil {
			continue
		}
		d := ""
		switch out[i].Type {
		case artifacts.TypeNumber, artifacts.TypeInteger:
			d = "0"
		case artifacts.TypeBoolean:
			d = "false"
		case artifacts.TypeDate:
			d = "today"
		}
		out[i].Default = &d
	}
	return out
}

// ParseValues types text values (a command line's -p name=value) by the
// parameters' declared types, ready for Bind. A name the query does not declare
// is refused, naming the ones it does. Bind still validates what comes out.
func ParseValues(q *Query, raw map[string]string) (map[string]any, error) {
	byName := make(map[string]Param, len(q.Parameters))
	for _, p := range q.Parameters {
		byName[p.Name] = p
	}
	names := make([]string, 0, len(raw))
	for n := range raw {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make(map[string]any, len(raw))
	for _, n := range names {
		p, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("there is no parameter named %q (this query declares: %s)", n, declaredNames(q))
		}
		v, err := parseValue(p, raw[n])
		if err != nil {
			return nil, err
		}
		out[n] = v
	}
	return out, nil
}

// The same shapes the defaults and the frontend accept: no hex floats, no
// underscores, no "inf" or "nan".
var (
	decimalRe = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$`)
	integerRe = regexp.MustCompile(`^-?\d+$`)
)

func parseValue(p Param, s string) (any, error) {
	switch p.Type {
	case artifacts.TypeNumber:
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || !decimalRe.MatchString(strings.TrimSpace(s)) {
			return nil, fmt.Errorf("parameter %q must be a number (got %q)", p.Name, s)
		}
		return f, nil
	case artifacts.TypeInteger:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || !integerRe.MatchString(strings.TrimSpace(s)) {
			return nil, fmt.Errorf("parameter %q must be a whole number (got %q)", p.Name, s)
		}
		return n, nil
	case artifacts.TypeBoolean:
		switch strings.TrimSpace(s) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("parameter %q must be true or false (got %q)", p.Name, s)
	}
	return s, nil // text, date: the binder checks the rest
}

func declaredNames(q *Query) string {
	if len(q.Parameters) == 0 {
		return "none"
	}
	names := make([]string, len(q.Parameters))
	for i, p := range q.Parameters {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

func checkDeclarations(q *Query) error {
	used := engine.ParamNames(q.SQL)
	declared := map[string]bool{}
	for _, p := range q.Parameters {
		declared[p.Name] = true
	}
	usedSet := map[string]bool{}
	for _, u := range used {
		usedSet[u] = true
		if !declared[u] {
			return fmt.Errorf("the SQL uses $%s, which is not a declared parameter. Declare it in parameters ({name: %q, type, default}), or write the value into the SQL", u, u)
		}
	}
	for _, p := range q.Parameters {
		if !usedSet[p.Name] {
			return fmt.Errorf("parameter %q is declared but the SQL never uses $%s", p.Name, p.Name)
		}
	}
	return nil
}

func trimInvalid(err error) string {
	return strings.TrimPrefix(err.Error(), artifacts.ErrInvalid.Error()+": ")
}
