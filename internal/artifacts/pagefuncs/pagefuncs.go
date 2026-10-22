// Package pagefuncs is the closed function map of an HTML artifact's
// templates (docs/plans/html-artifacts.md, H2). It is a leaf shared by the
// save-time vet (internal/artifacts) and the renderer, so both parse a page
// against exactly the same names.
//
// The map is the whole API a page template has: it cannot name a connection,
// write SQL or read a file. Data-dependent functions (query, param, params,
// link) go through an Env bound per render; the rest are pure formatting and
// SVG helpers.
//
// # No trusted types
//
// No function returns template.HTML, template.JS, template.CSS, template.URL,
// template.HTMLAttr, template.JSStr or template.Srcset, nor any type that
// holds one. Everything a function returns is plain data that html/template
// escapes by context. One "safeHTML" helper would turn any database row into
// script, so TestNoFunctionReturnsATrustedType fails if one is ever added.
package pagefuncs

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Names of the functions, in the order the plan lists them. A template that
// calls anything else (including the builtin "call") is refused at save.
const (
	Query       = "query"
	Param       = "param"
	Params      = "params"
	Link        = "link"
	Number      = "number"
	Percent     = "percent"
	Date        = "date"
	Bytes       = "bytes"
	Truncate    = "truncate"
	ScaleLinear = "scaleLinear"
	ScaleBand   = "scaleBand"
	PathLine    = "pathLine"
	JSON        = "json"
)

// Result is what query returns: the named query's rows, capped by the caller.
type Result struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
}

// ParamInfo describes one declared parameter, for building controls.
type ParamInfo struct {
	Name    string
	Type    string
	Label   string
	Default string
	Options []string
}

// Env is the per-render context the data functions go through. The renderer
// (P3) implements it: Query runs the manifest's named query with the current
// parameter values, Param returns a bound value, Params the declared
// parameters, Link builds a URL to this page with some values overridden.
// pairs is name, value, name, value, ...; an Env must refuse an odd count or a
// name the manifest does not declare.
type Env interface {
	Query(name string) (Result, error)
	Param(name string) (any, error)
	Params() []ParamInfo
	Link(pairs []any) (string, error)
}

// Funcs returns the closed function map bound to env. The names never change;
// only the Env behind the data functions does.
func Funcs(env Env) template.FuncMap {
	return template.FuncMap{
		Query: func(name string) (Result, error) {
			r, err := env.Query(name)
			return plainResult(r), err
		},
		Param: func(name string) (any, error) {
			v, err := env.Param(name)
			return plain(v), err
		},
		Params:      func() []ParamInfo { return plainParams(env.Params()) },
		Link:        func(pairs ...any) (string, error) { return env.Link(pairs) },
		Number:      number,
		Percent:     percent,
		Date:        formatDate,
		Bytes:       formatBytes,
		Truncate:    truncate,
		ScaleLinear: scaleLinear,
		ScaleBand:   scaleBand,
		PathLine:    pathLine,
		JSON:        jsonValue,
	}
}

// StubEnv is an Env with no data: every query is empty, every parameter is
// the empty string, a link is "#". The save-time vet executes templates
// against it to run html/template's context escaper without a database.
type StubEnv struct{}

// Query implements Env.
func (StubEnv) Query(string) (Result, error) {
	return Result{Columns: []string{}, Rows: [][]any{}}, nil
}

// Param implements Env.
func (StubEnv) Param(string) (any, error) { return "", nil }

// Params implements Env.
func (StubEnv) Params() []ParamInfo { return []ParamInfo{} }

// Link implements Env.
func (StubEnv) Link([]any) (string, error) { return "#", nil }

// toFloat converts the numeric kinds a database row or a template constant can
// hold. A number held as text ("12.5") is accepted; anything else is an error.
func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return finite(x)
	case float32:
		return finite(float64(x))
	case int:
		return float64(x), nil
	case int8:
		return float64(x), nil
	case int16:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case uint:
		return float64(x), nil
	case uint8:
		return float64(x), nil
	case uint16:
		return float64(x), nil
	case uint32:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	case json.Number:
		return parseFloat(string(x))
	case string:
		return parseFloat(x)
	}
	return 0, fmt.Errorf("%v is not a number", v)
}

func parseFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return finite(f)
}

func finite(f float64) (float64, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%v is not a finite number", f)
	}
	return f, nil
}

// formatFloat renders a float with a fixed number of decimals and thousands
// separators, as plain text.
func formatFloat(f float64, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	if decimals > 10 {
		decimals = 10
	}
	s := strconv.FormatFloat(math.Abs(f), 'f', decimals, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if f < 0 && strings.Trim(out, "0.,") != "" {
		out = "-" + out
	}
	return out
}

// number formats v with thousands separators. The optional second argument is
// the number of decimals (default 0).
func number(v any, decimals ...int) (string, error) {
	f, err := toFloat(v)
	if err != nil {
		return "", err
	}
	d := 0
	if len(decimals) > 0 {
		d = decimals[0]
	}
	return formatFloat(f, d), nil
}

// percent formats a fraction as a percentage: 0.256 is "25.6%". The optional
// second argument is the number of decimals (default 1).
func percent(v any, decimals ...int) (string, error) {
	f, err := toFloat(v)
	if err != nil {
		return "", err
	}
	d := 1
	if len(decimals) > 0 {
		d = decimals[0]
	}
	return formatFloat(f*100, d) + "%", nil
}

// dateLayouts are the layouts a date value may be read from.
var dateLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}

// formatDate formats a time or a date text. The layout is one of the closed
// names "iso" (default), "short" (Jan 2, 2006), "long" (January 2, 2006) or
// "month" (Jan 2006), never a free Go layout.
func formatDate(v any, layout ...string) (string, error) {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case string:
		ok := false
		for _, l := range dateLayouts {
			if p, err := time.Parse(l, x); err == nil {
				t, ok = p, true
				break
			}
		}
		if !ok {
			return "", fmt.Errorf("%q is not a date", x)
		}
	default:
		return "", fmt.Errorf("%v is not a date", v)
	}
	name := "iso"
	if len(layout) > 0 {
		name = layout[0]
	}
	switch name {
	case "iso":
		return t.Format("2006-01-02"), nil
	case "short":
		return t.Format("Jan 2, 2006"), nil
	case "long":
		return t.Format("January 2, 2006"), nil
	case "month":
		return t.Format("Jan 2006"), nil
	}
	return "", fmt.Errorf("date layout %q is not one of iso, short, long, month", name)
}

// formatBytes renders a byte count in powers of 1024: 1536 is "1.5 KiB".
func formatBytes(v any) (string, error) {
	f, err := toFloat(v)
	if err != nil {
		return "", err
	}
	if f < 1024 {
		return strconv.FormatFloat(f, 'f', 0, 64) + " B", nil
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	i := -1
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " " + units[i], nil
}

// truncate shortens s to at most n characters, ending in an ellipsis when it
// cut anything.
func truncate(s string, n int) string {
	if n < 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// scaleLinear maps v from the domain [d0, d1] to the range [r0, r1].
func scaleLinear(d0, d1, r0, r1, v any) (float64, error) {
	vals := make([]float64, 5)
	for i, x := range []any{d0, d1, r0, r1, v} {
		f, err := toFloat(x)
		if err != nil {
			return 0, err
		}
		vals[i] = f
	}
	if vals[0] == vals[1] {
		return vals[2], nil
	}
	return vals[2] + (vals[4]-vals[0])/(vals[1]-vals[0])*(vals[3]-vals[2]), nil
}

// Band is one slot of scaleBand.
type Band struct {
	Start, Width float64
}

// scaleBand splits [r0, r1] into n equal bands with a 10% gap and returns band
// i (0-based).
func scaleBand(n, i, r0, r1 any) (Band, error) {
	vals := make([]float64, 4)
	for k, x := range []any{n, i, r0, r1} {
		f, err := toFloat(x)
		if err != nil {
			return Band{}, err
		}
		vals[k] = f
	}
	count, idx := vals[0], vals[1]
	if count < 1 || idx < 0 || idx >= count {
		return Band{}, fmt.Errorf("band %v is outside 0..%v", idx, count-1)
	}
	step := (vals[3] - vals[2]) / count
	pad := step * 0.1
	return Band{Start: vals[2] + step*idx + pad/2, Width: step - pad}, nil
}

// maxPathPoints bounds pathLine so a page cannot build an unbounded string.
const maxPathPoints = 10000

// pathLine builds an SVG path "M x,y L x,y ..." from two equal-length lists of
// numbers. It returns plain text of digits and letters only.
func pathLine(xs, ys []any) (string, error) {
	if len(xs) != len(ys) {
		return "", fmt.Errorf("pathLine needs as many y values (%d) as x values (%d)", len(ys), len(xs))
	}
	if len(xs) > maxPathPoints {
		return "", fmt.Errorf("pathLine is limited to %d points", maxPathPoints)
	}
	var b strings.Builder
	for i := range xs {
		x, err := toFloat(xs[i])
		if err != nil {
			return "", err
		}
		y, err := toFloat(ys[i])
		if err != nil {
			return "", err
		}
		if i == 0 {
			b.WriteString("M")
		} else {
			b.WriteString(" L")
		}
		b.WriteString(strconv.FormatFloat(x, 'f', 2, 64) + "," + strconv.FormatFloat(y, 'f', 2, 64))
	}
	return b.String(), nil
}

// jsonValue returns v as plain JSON data (maps, slices, strings, numbers,
// booleans), for a <script type="application/json"> block, where
// html/template's JS escaper writes it as a JSON literal. Going through
// encoding/json strips every Go type (a template.HTML hidden in a row would
// come out as an ordinary string), so the value is data and nothing else.
func jsonValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("the value cannot be written as JSON")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("the value cannot be written as JSON")
	}
	return out, nil
}

// plain returns v with every Go type that html/template trusts (or any named
// string type at all) replaced by an ordinary string, recursively through
// slices and maps. The data a template sees is the database's, and an Env must
// not be able to smuggle template.HTML, template.JS or template.URL past the
// escaper; normalising here means a careless Env cannot either.
func plain(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		if rv.Type() == reflect.TypeOf("") {
			return v
		}
		if n, ok := v.(json.Number); ok {
			return n // a number literal, written as a number; not a trusted type
		}
		return rv.String()
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = plain(rv.Index(i).Interface())
		}
		return out
	case reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = plain(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			key, _ := plain(k.Interface()).(string)
			if key == "" {
				key = fmt.Sprint(plain(k.Interface()))
			}
			out[key] = plain(rv.MapIndex(k).Interface())
		}
		return out
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return plain(rv.Elem().Interface())
	case reflect.Struct:
		if _, ok := v.(time.Time); ok {
			return v
		}
		return fmt.Sprint(v) // an unknown struct is shown, never trusted or called into
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Sprint(v)
	}
	return v
}

func plainResult(r Result) Result {
	out := Result{Columns: make([]string, len(r.Columns)), Rows: make([][]any, len(r.Rows)), Truncated: r.Truncated}
	for i, c := range r.Columns {
		out.Columns[i] = string([]byte(c))
	}
	for i, row := range r.Rows {
		out.Rows[i] = make([]any, len(row))
		for j, cell := range row {
			out.Rows[i][j] = plain(cell)
		}
	}
	return out
}

func plainParams(in []ParamInfo) []ParamInfo {
	out := make([]ParamInfo, len(in))
	for i, p := range in {
		p.Options = append([]string(nil), p.Options...)
		out[i] = p
	}
	return out
}
