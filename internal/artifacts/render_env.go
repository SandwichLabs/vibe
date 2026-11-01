package artifacts

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/SandwichLabs/slaw/internal/artifacts/pagefuncs"
)

// MaxQueryResultBytes caps one query result of any artifact query (level 1,
// a live A2UI binding or notebook SQL cell, a worker view, a page). The shared scan layer enforces
// it while rows are read (engine.WithMaxResultBytes); a page also checks what
// its template sees, below, in case a runner does not.
const MaxQueryResultBytes = 16 << 20

// memoQuery is one named query's outcome within a render: run on first use,
// then reused (a failure is remembered too, so a query is never retried).
type memoQuery struct {
	res pagefuncs.Result
	err error
}

// pageEnv is the pagefuncs.Env of one render. Its values are fixed for the
// render, so a query's memo key is its name: one run per (query, values).
type pageEnv struct {
	ctx context.Context
	run PageRunner
	m   *Manifest
	// supplied are the explicit values (typed, checked); defaults every
	// parameter's default on this render's clock.
	supplied map[string]any
	defaults map[string]any
	memo     map[string]*memoQuery
	// failed names the query that failed, for the fixed error block.
	failed string
}

// Query implements pagefuncs.Env: run the named manifest query once, lazily.
func (e *pageEnv) Query(name string) (pagefuncs.Result, error) {
	if q, ok := e.memo[name]; ok {
		return q.res, q.err
	}
	q := &memoQuery{}
	e.memo[name] = q
	if findQuery(e.m, name) == nil {
		q.err = fmt.Errorf("there is no query named %q", name)
		e.fail(name)
		return q.res, q.err
	}
	if err := e.ctx.Err(); err != nil {
		q.err = err
		return q.res, q.err
	}
	rows, err := e.run(e.ctx, name, e.supplied)
	if err != nil || rows == nil {
		if err == nil {
			err = errPageQuery
		}
		q.err = fmt.Errorf("%w: %w", errPageQuery, err)
		e.fail(name)
		return q.res, q.err
	}
	// Studio's runner already stopped reading at MaxQueryResultBytes; this is the
	// backstop for a PageRunner that does not, and bounds what the template copies.
	if pagefuncs.SizeOf(rows.Rows) > MaxQueryResultBytes {
		q.err = fmt.Errorf("%w: the result is over %d bytes", errPageQuery, MaxQueryResultBytes)
		e.fail(name)
		return q.res, q.err
	}
	q.res = pagefuncs.Result{Columns: rows.Columns, Rows: rows.Rows, Truncated: rows.Truncated}
	return q.res, nil
}

// fail records the first failing query.
func (e *pageEnv) fail(name string) {
	if e.failed == "" {
		e.failed = name
	}
}

// Param implements pagefuncs.Env: the value a query would be bound to.
func (e *pageEnv) Param(name string) (any, error) {
	if !hasParam(e.m, name) {
		return nil, fmt.Errorf("there is no parameter named %q", name)
	}
	if v, ok := e.supplied[name]; ok {
		return v, nil
	}
	return e.defaults[name], nil
}

// Params implements pagefuncs.Env: the declared parameters, in order, with
// their resolved defaults and static options.
func (e *pageEnv) Params() []pagefuncs.ParamInfo {
	out := make([]pagefuncs.ParamInfo, 0, len(e.m.Parameters))
	for _, p := range e.m.Parameters {
		out = append(out, pagefuncs.ParamInfo{
			Name: p.Name, Type: p.Type, Label: p.Label,
			Default: fmt.Sprint(e.defaults[p.Name]), Options: append([]string(nil), p.Options...),
		})
	}
	return out
}

// Link implements pagefuncs.Env: a URL to this page with some parameters
// overridden. It is relative to the page's own route ("page?region=EU"), so it
// keeps the token path, and it can only ever be that route: there is no way to
// pass a scheme, a host or a path. A name that is not declared, an odd number
// of arguments and a value the parameter would refuse are errors, so a link
// that renders is a link that works.
func (e *pageEnv) Link(pairs []any) (string, error) {
	if len(pairs)%2 != 0 {
		return "", fmt.Errorf("link takes parameter names and values in pairs")
	}
	q := url.Values{}
	for name, v := range e.supplied {
		q.Set(name, linkText(v))
	}
	for i := 0; i < len(pairs); i += 2 {
		name, ok := pairs[i].(string)
		p := findParam(e.m, name)
		if !ok || p == nil {
			return "", fmt.Errorf("link: %v is not a declared parameter", pairs[i])
		}
		text, err := linkValueText(pairs[i+1])
		if err != nil {
			return "", fmt.Errorf("link: %w", err)
		}
		typed, err := parseQueryValue(p, text)
		if err == nil {
			_, err = coerceValue(p, typed)
		}
		if err != nil {
			return "", fmt.Errorf("link: the value for %s is not valid", name)
		}
		q.Set(name, text)
	}
	if len(q) == 0 {
		return pageRoute, nil
	}
	return pageRoute + "?" + q.Encode(), nil
}

// pageRoute is the route name a link points at, relative to the page's URL.
const pageRoute = "page"

func findParam(m *Manifest, name string) *Param {
	for i := range m.Parameters {
		if m.Parameters[i].Name == name {
			return &m.Parameters[i]
		}
	}
	return nil
}

// linkText writes a checked, typed value the way it is read back from a query
// string.
func linkText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// linkValueText turns a template value into the text of a query-string value:
// strings, booleans and numbers only (a row cell is one of these).
func linkValueText(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(x), nil
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("a link value must be text, a number or true/false")
}

// MaxPageQueryBytes bounds the raw query string of a page request.
const MaxPageQueryBytes = 8 << 10

// ParsePageQuery turns the query string of a page request into typed
// parameter values: each key must be a declared parameter, appear once, and
// read as its parameter's type. The values are NOT yet checked against the
// parameter's limits: Bind does that on every query (and RenderHTMLPage once
// up front), so this is the only place the string form exists. Values come
// from nowhere else.
func ParsePageQuery(m *Manifest, q url.Values) (map[string]any, error) {
	out := make(map[string]any, len(q))
	names := make([]string, 0, len(q))
	for n := range q {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := findParam(m, n)
		if p == nil {
			return nil, invalid("there is no parameter named %q", truncateForError(n))
		}
		if len(q[n]) != 1 {
			return nil, invalid("parameter %q is given more than once", n)
		}
		v, err := parseQueryValue(p, q[n][0])
		if err != nil {
			return nil, err
		}
		out[n] = v
	}
	return out, nil
}

// parseQueryValue reads one query-string value as its parameter's JSON-shaped
// Go type (the types coerceValue takes).
func parseQueryValue(p *Param, s string) (any, error) {
	switch p.Type {
	case TypeNumber:
		if !decimalRe.MatchString(s) {
			return nil, invalid("parameter %q must be a number", p.Name)
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, invalid("parameter %q must be a number", p.Name)
		}
		return f, nil
	case TypeInteger:
		if !integerRe.MatchString(s) {
			return nil, invalid("parameter %q must be a whole number", p.Name)
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, invalid("parameter %q must be a whole number", p.Name)
		}
		return n, nil
	case TypeBoolean:
		switch strings.ToLower(s) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, invalid("parameter %q must be true or false", p.Name)
	}
	return s, nil
}
