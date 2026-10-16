package artifacts

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// isoDate is the layout of a date parameter value.
const isoDate = "2006-01-02"

var (
	decimalRe  = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	integerRe  = regexp.MustCompile(`^-?[0-9]+$`)
	todayRelRe = regexp.MustCompile(`^today-([1-9][0-9]{0,3})d$`)
)

// MaxRelativeDays is the largest n of a "today-<n>d" date default.
const MaxRelativeDays = 3660

// ResolveDateDefault turns a date default into an ISO date on now's clock: a
// literal YYYY-MM-DD, or one of the closed set today, today-<n>d (1 to 3660),
// start-of-month, start-of-year. Resolving happens at run time, so "the last
// 90 days" never goes stale.
func ResolveDateDefault(def string, now time.Time) (string, error) {
	y, mo, d := now.Date()
	switch def {
	case "today":
		return now.Format(isoDate), nil
	case "start-of-month":
		return time.Date(y, mo, 1, 0, 0, 0, 0, now.Location()).Format(isoDate), nil
	case "start-of-year":
		return time.Date(y, time.January, 1, 0, 0, 0, 0, now.Location()).Format(isoDate), nil
	}
	if m := todayRelRe.FindStringSubmatch(def); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n > MaxRelativeDays {
			return "", fmt.Errorf("date default %q: at most %d days back", def, MaxRelativeDays)
		}
		return time.Date(y, mo, d-n, 0, 0, 0, 0, now.Location()).Format(isoDate), nil
	}
	if err := checkISODate(def); err != nil {
		return "", fmt.Errorf("date default %q is not YYYY-MM-DD, today, today-<n>d, start-of-month or start-of-year", def)
	}
	return def, nil
}

// checkISODate accepts only a real calendar date written YYYY-MM-DD.
func checkISODate(s string) error {
	t, err := time.Parse(isoDate, s)
	if err != nil || t.Format(isoDate) != s || len(s) != len(isoDate) {
		return fmt.Errorf("%q is not a valid YYYY-MM-DD date", s)
	}
	return nil
}

// defaultValue resolves a parameter's default to the Go value Bind passes.
func defaultValue(p *Param, now time.Time) (any, error) {
	if p.Default == nil {
		return nil, fmt.Errorf("parameter %q has no default", p.Name)
	}
	def := *p.Default
	switch p.Type {
	case TypeNumber:
		if !decimalRe.MatchString(def) {
			return nil, fmt.Errorf("parameter %q: default %q is not a number", p.Name, def)
		}
		f, err := strconv.ParseFloat(def, 64)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: default %q is not a number", p.Name, def)
		}
		return coerceValue(p, f)
	case TypeInteger:
		if !integerRe.MatchString(def) {
			return nil, fmt.Errorf("parameter %q: default %q is not an integer", p.Name, def)
		}
		n, err := strconv.ParseInt(def, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: default %q is not an integer in range", p.Name, def)
		}
		return coerceValue(p, n)
	case TypeBoolean:
		if def != "true" && def != "false" {
			return nil, fmt.Errorf("parameter %q: default %q must be true or false", p.Name, def)
		}
		return def == "true", nil
	case TypeDate:
		iso, err := ResolveDateDefault(def, now)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", p.Name, err)
		}
		return coerceValue(p, iso)
	}
	return coerceValue(p, def)
}

// coerceValue checks one raw value against the parameter and returns the Go
// type that is bound: text and enum string, number float64, integer int64,
// boolean bool, date an ISO string. Values arrive as JSON, so a wrong JSON type
// (a string for a number, a number for a boolean) is refused, not converted.
func coerceValue(p *Param, raw any) (any, error) {
	switch p.Type {
	case TypeText:
		return coerceText(p, raw)
	case TypeEnum:
		return coerceEnum(p, raw)
	case TypeNumber:
		return coerceNumber(p, raw)
	case TypeInteger:
		return coerceInteger(p, raw)
	case TypeBoolean:
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("parameter %q must be true or false", p.Name)
		}
		return b, nil
	case TypeDate:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("parameter %q must be a date written YYYY-MM-DD", p.Name)
		}
		if err := checkISODate(s); err != nil {
			return nil, fmt.Errorf("parameter %q: %w", p.Name, err)
		}
		return s, nil
	}
	return nil, fmt.Errorf("parameter %q has unknown type %q", p.Name, p.Type)
}

func stringValue(p *Param, raw any) (string, error) {
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("parameter %q must be text", p.Name)
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", fmt.Errorf("parameter %q must be valid text without NUL characters", p.Name)
	}
	return s, nil
}

func textLimit(p *Param) int {
	if p.MaxLength > 0 && p.MaxLength < MaxTextLength {
		return p.MaxLength
	}
	return MaxTextLength
}

func coerceText(p *Param, raw any) (any, error) {
	s, err := stringValue(p, raw)
	if err != nil {
		return nil, err
	}
	if n := utf8.RuneCountInString(s); n > textLimit(p) {
		return nil, fmt.Errorf("parameter %q is %d characters, over its %d limit", p.Name, n, textLimit(p))
	}
	return s, nil
}

func coerceEnum(p *Param, raw any) (any, error) {
	s, err := stringValue(p, raw)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(s) > MaxTextLength {
		return nil, fmt.Errorf("parameter %q is over %d characters", p.Name, MaxTextLength)
	}
	// An options_query enum cannot be checked here (its rows are data); the
	// value is still only ever a bound string.
	if p.OptionsQuery != "" {
		return s, nil
	}
	for _, o := range p.Options {
		if o == s {
			return s, nil
		}
	}
	return nil, fmt.Errorf("parameter %q: %q is not one of its options", p.Name, s)
}

// floatOf reads a JSON-shaped number from the Go number types a caller may
// hold; a string is not a number.
func floatOf(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func inRange(p *Param, f float64) error {
	if p.Min != nil && f < *p.Min {
		return fmt.Errorf("parameter %q is below its minimum %v", p.Name, *p.Min)
	}
	if p.Max != nil && f > *p.Max {
		return fmt.Errorf("parameter %q is above its maximum %v", p.Name, *p.Max)
	}
	return nil
}

func coerceNumber(p *Param, raw any) (any, error) {
	f, ok := floatOf(raw)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("parameter %q must be a finite number", p.Name)
	}
	if err := inRange(p, f); err != nil {
		return nil, err
	}
	return f, nil
}

func coerceInteger(p *Param, raw any) (any, error) {
	var n int64
	switch v := raw.(type) {
	case int:
		n = int64(v)
	case int32:
		n = int64(v)
	case int64:
		n = v
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return coerceIntegralFloat(p, raw)
		}
		n = i
	default:
		return coerceIntegralFloat(p, raw)
	}
	return finishInteger(p, n)
}

func coerceIntegralFloat(p *Param, raw any) (any, error) {
	f, ok := floatOf(raw)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > MaxSafeInteger {
		return nil, fmt.Errorf("parameter %q must be a whole number within +/-2^53", p.Name)
	}
	return finishInteger(p, int64(f))
}

func finishInteger(p *Param, n int64) (any, error) {
	if n > MaxSafeInteger || n < -MaxSafeInteger {
		return nil, fmt.Errorf("parameter %q must be a whole number within +/-2^53", p.Name)
	}
	if err := inRange(p, float64(n)); err != nil {
		return nil, err
	}
	return n, nil
}
