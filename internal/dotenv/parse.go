// Package dotenv reads a workspace's .env file and remembers, per person, whether
// it may be used (direnv-style allow and deny).
//
// The file is parsed as plain dotenv text: KEY=value, export KEY=value, quotes,
// comments. Nothing is executed and no command substitution happens. Values
// are returned to Go callers only; every status type in this package carries
// variable names, never values.
package dotenv

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// File is the name of the dotenv file at the root of a workspace.
const File = ".env"

// MaxSize is the largest .env Vibe reads. A bigger file is ignored whole.
const MaxSize = 64 << 10

// Parsed is the result of Parse.
type Parsed struct {
	// Vars are the valid assignments (later lines win).
	Vars map[string]string
	// Names are the variable names, sorted.
	Names []string
	// Warnings describe skipped lines by number and, when it could be read,
	// the variable name. They never contain a value.
	Warnings []string
}

// Parse parses dotenv text. It is lenient: a line it cannot read is skipped
// with a names-only warning. There is no variable expansion.
func Parse(data []byte) Parsed {
	p := Parsed{Vars: map[string]string{}}
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		n := i + 1
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		key, rawVal, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validName(key) {
			p.Warnings = append(p.Warnings, fmt.Sprintf("line %d is not KEY=value and was skipped", n))
			continue
		}
		val, err := parseValue(strings.TrimLeft(rawVal, " \t"))
		if errors.Is(err, errUnterminated) {
			// A multi-line quoted value (a PEM block): skip to the line that
			// closes it, so continuation lines never read as variables.
			q := strings.TrimLeft(rawVal, " \t")[:1]
			closed := false
			for j := i + 1; j < len(lines); j++ {
				if strings.Contains(lines[j], q) {
					i, closed = j, true
					break
				}
			}
			if !closed {
				i = len(lines)
			}
			p.Warnings = append(p.Warnings, fmt.Sprintf("line %d (%s): multi-line values are not supported; skipped", n, key))
			continue
		}
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("line %d (%s): %s; skipped", n, key, err))
			continue
		}
		p.Vars[key] = val
	}
	for k := range p.Vars {
		p.Names = append(p.Names, k)
	}
	sort.Strings(p.Names)
	return p
}

func validName(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// errUnterminated marks a quoted value that does not close on its line.
var errUnterminated = errors.New("unterminated quote")

func parseValue(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	switch s[0] {
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", errUnterminated
		}
		if err := onlyComment(s[end+2:]); err != nil {
			return "", err
		}
		return s[1 : end+1], nil
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '\\':
				if i+1 >= len(s) {
					return "", errUnterminated
				}
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case 'r':
					b.WriteByte('\r')
				case '"', '\\', '$', '\'':
					b.WriteByte(s[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(s[i])
				}
			case '"':
				if err := onlyComment(s[i+1:]); err != nil {
					return "", err
				}
				return b.String(), nil
			default:
				b.WriteByte(c)
			}
		}
		return "", errUnterminated
	}
	// Unquoted: an inline comment starts at whitespace followed by '#'.
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
			s = s[:i]
			break
		}
	}
	return strings.TrimSpace(s), nil
}

func onlyComment(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return fmt.Errorf("text after the closing quote")
}
