package pagefuncs

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"reflect"
	"strings"
)

// ErrStringBudget is returned by a metered function whose result would take a
// render over its string budget.
var ErrStringBudget = errors.New("the page builds too much text")

// Meter is a render's string budget. Check says whether n more bytes of
// string would still fit (it charges nothing, so a function can refuse before
// it allocates); Charge spends n.
type Meter interface {
	Check(n int64) error
	Charge(n int64) error
}

// FuncsMetered is Funcs for a render: every function that returns a string
// charges the result's length against m, and the builtins that build strings
// from their arguments (printf, print, println, js, html, urlquery) are
// replaced by versions that estimate their result from their arguments and
// refuse before allocating when it cannot fit. A template can otherwise hold a
// huge string only in a variable, where neither the output cap nor the
// context check sees it (printf of ten copies of a string, ten times over, is
// a gigabyte in a few lines). The names are the builtins' own, so the page
// language does not change; Funcs (used by the save-time vet, which never
// executes a page) is unchanged.
func FuncsMetered(env Env, m Meter) template.FuncMap {
	f := Funcs(env)
	f[Number] = func(v any, decimals ...int) (string, error) { return charged(m)(number(v, decimals...)) }
	f[Percent] = func(v any, decimals ...int) (string, error) { return charged(m)(percent(v, decimals...)) }
	f[Date] = func(v any, layout ...string) (string, error) { return charged(m)(formatDate(v, layout...)) }
	f[Bytes] = func(v any) (string, error) { return charged(m)(formatBytes(v)) }
	f[Truncate] = func(s string, n int) (string, error) { return charged(m)(truncate(s, n), nil) }
	f[PathLine] = func(xs, ys []any) (string, error) { return charged(m)(pathLine(xs, ys)) }
	f[Link] = func(pairs ...any) (string, error) { return charged(m)(env.Link(pairs)) }
	f[JSON] = func(v any) (any, error) {
		out, err := jsonValue(v)
		if err != nil {
			return nil, err
		}
		raw, merr := json.Marshal(out)
		if merr != nil {
			return nil, fmt.Errorf("the value cannot be written as JSON")
		}
		return out, m.Charge(int64(len(raw)))
	}
	f["printf"] = func(format string, args ...any) (string, error) {
		if err := m.Check(estimatePrintf(format, args)); err != nil {
			return "", err
		}
		return charged(m)(fmt.Sprintf(format, args...), nil)
	}
	f["print"] = func(args ...any) (string, error) {
		if err := m.Check(argBytes(args)); err != nil {
			return "", err
		}
		return charged(m)(fmt.Sprint(args...), nil)
	}
	f["println"] = func(args ...any) (string, error) {
		if err := m.Check(argBytes(args)); err != nil {
			return "", err
		}
		return charged(m)(fmt.Sprintln(args...), nil)
	}
	// js, html and urlquery can multiply their input (up to six bytes for each).
	f["js"] = escaper(m, template.JSEscaper)
	f["html"] = escaper(m, template.HTMLEscaper)
	f["urlquery"] = escaper(m, template.URLQueryEscaper)
	return f
}

// charged charges a string result and passes it (and its error) through.
func charged(m Meter) func(string, error) (string, error) {
	return func(s string, err error) (string, error) {
		if err != nil {
			return "", err
		}
		if cerr := m.Charge(int64(len(s))); cerr != nil {
			return "", cerr
		}
		return s, nil
	}
}

func escaper(m Meter, esc func(...any) string) func(...any) (string, error) {
	return func(args ...any) (string, error) {
		if err := m.Check(6 * argBytes(args)); err != nil {
			return "", err
		}
		return charged(m)(esc(args...), nil)
	}
}

// sizeLimit stops SizeOf's walk, and the walk over all of a call's
// arguments together: it is above any render's string budget (64 MB), so
// anything past it is refused anyway, and it bounds the walk itself (about
// sizeLimit/32 element visits per call), which runs inside one action, where
// the render's context check cannot interrupt it.
const sizeLimit = 128 << 20

// SizeOf estimates the bytes a value holds: every string and []byte inside it
// (through slices, arrays, maps, structs, pointers and interfaces) plus about
// 32 bytes per element. It stops once past sizeLimit and does not follow a
// cycle twice. A row set is a [][]any, so a single 5 MB cell counts as 5 MB.
func SizeOf(v any) int64 {
	var n int64
	walkSize(reflect.ValueOf(v), &n, map[uintptr]bool{})
	return n
}

// sizeArgs sizes each argument once, sharing one running total so the whole
// walk stops past sizeLimit, and returns the total and the largest argument.
// Each argument has its own seen set: an argument named twice counts twice.
func sizeArgs(args []any) (total, longest int64) {
	for _, a := range args {
		before := total
		walkSize(reflect.ValueOf(a), &total, map[uintptr]bool{})
		longest = max(longest, total-before)
		if total > sizeLimit {
			break
		}
	}
	return total, longest
}

func walkSize(v reflect.Value, n *int64, seen map[uintptr]bool) {
	if *n > sizeLimit || !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		*n += int64(v.Len()) + 16
	case reflect.Interface:
		walkSize(v.Elem(), n, seen)
	case reflect.Pointer:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		walkSize(v.Elem(), n, seen)
	case reflect.Slice, reflect.Array:
		walkList(v, n, seen)
	case reflect.Map:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		*n += 32 + int64(v.Len())*32
		it := v.MapRange()
		for it.Next() && *n <= sizeLimit {
			walkSize(it.Key(), n, seen)
			walkSize(it.Value(), n, seen)
		}
	case reflect.Struct:
		*n += 32
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkSize(v.Field(i), n, seen)
			}
		}
	default:
		*n += 32
	}
}

// walkList sizes a slice or array: bytes by length, other elements one by one
// (a slice already walked is not walked again).
func walkList(v reflect.Value, n *int64, seen map[uintptr]bool) {
	if v.Type().Elem().Kind() == reflect.Uint8 {
		*n += int64(v.Len()) + 16
		return
	}
	if v.Kind() == reflect.Slice {
		if v.IsNil() || (v.Len() > 0 && seen[v.Pointer()]) {
			return
		}
		if v.Len() > 0 {
			seen[v.Pointer()] = true
		}
	}
	*n += 32 + int64(v.Len())*32
	for i := 0; i < v.Len() && *n <= sizeLimit; i++ {
		walkSize(v.Index(i), n, seen)
	}
}

// argBytes is what the arguments can contribute to a result that joins them.
func argBytes(args []any) int64 {
	n, _ := sizeArgs(args)
	return n
}

// estimatePrintf bounds what Sprintf(format, args...) can write before it runs:
// the format itself, every width and precision in it (fmt accepts up to a
// million), each verb repeating the longest argument (a %[1]s may name one
// argument any number of times), and every argument once (extra arguments are
// appended as %!(EXTRA ...)).
func estimatePrintf(format string, args []any) int64 {
	total, longest := sizeArgs(args)
	est := int64(len(format)) + total
	verbs := int64(strings.Count(format, "%"))
	est += verbs * (longest + 64)
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		run := 0
		for j := i + 1; j < len(format); j++ {
			c := format[j]
			switch {
			case c >= '0' && c <= '9':
				run = min(run*10+int(c-'0'), 1_000_000)
				continue
			case c == '*':
				est += 1_000_000
			case c == '.' || c == '+' || c == '-' || c == '#' || c == ' ' || c == '[' || c == ']':
			default:
				i = j
				j = len(format)
			}
			est += int64(run)
			run = 0
		}
		est += int64(run)
	}
	return est
}
