package artifacts

import (
	"math"
	"strconv"
)

// paramOut is Param as artifact.yaml writes it: the same fields, with the
// default typed by the parameter (an integer default is the YAML integer 0,
// not the string "0"). Decoding is unchanged: a YAML scalar of any kind reads
// into Param.Default as its text, so files written either way load.
type paramOut struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Label        string   `yaml:"label,omitempty"`
	Default      any      `yaml:"default"`
	MaxLength    int      `yaml:"max_length,omitempty"`
	Min          *float64 `yaml:"min,omitempty"`
	Max          *float64 `yaml:"max,omitempty"`
	Options      []string `yaml:"options,omitempty"`
	OptionsQuery string   `yaml:"options_query,omitempty"`
}

// MarshalYAML writes the default as a number or boolean when the parameter's
// type says so and the text is that value's canonical spelling; anything else
// stays a string, so the file reads back to the same text.
func (p Param) MarshalYAML() (any, error) {
	return paramOut{
		Name: p.Name, Type: p.Type, Label: p.Label, Default: TypedDefault(&p),
		MaxLength: p.MaxLength, Min: p.Min, Max: p.Max, Options: p.Options, OptionsQuery: p.OptionsQuery,
	}, nil
}

// TypedDefault is p's default as a value of the parameter's type: an int64 for
// an integer, a float64 for a number, a bool for a boolean, when the stored
// text is that value's canonical spelling; otherwise the text itself (nil when
// there is no default). The YAML encoder must write the value
// back as the same text, so a number it would write with an exponent stays text.
func TypedDefault(p *Param) any {
	if p.Default == nil {
		return nil
	}
	s := *p.Default
	switch p.Type {
	case TypeInteger:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(n, 10) == s {
			return n
		}
	case TypeNumber:
		return typedNumber(s)
	case TypeBoolean:
		if s == "true" || s == "false" {
			return s == "true"
		}
	}
	return s
}

// typedNumber is a number default as a value the YAML encoder writes without an
// exponent (it uses one from 1e6 and below 1e-4), or the text.
func typedNumber(s string) any {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(n, 10) == s && n > -1_000_000 && n < 1_000_000 {
		return n
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || strconv.FormatFloat(f, 'f', -1, 64) != s {
		return s
	}
	if a := math.Abs(f); a >= 1e-4 && a < 1e6 {
		return f
	}
	return s
}
