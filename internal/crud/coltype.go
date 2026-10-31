package crud

import (
	"fmt"
	"regexp"
	"strings"
)

// SafeCastType reports whether t can be interpolated as the target of
// CAST(? AS <t>). The type text comes from the attached database's own catalog,
// which a hostile database controls, so it is never trusted: outside quoted
// sections only letters, digits, "_", spaces, commas, parentheses and square
// brackets are allowed (no ";", "-", "/", "." or operators); "..." and '...'
// sections must be terminated (a doubled quote is an escape) and hold no control
// characters; parentheses must balance and commas must sit inside them. That
// covers DECIMAL(10,2), TIMESTAMP WITH TIME ZONE, INTEGER[], STRUCT(a INT,
// "b c" VARCHAR), MAP(...), UNION(...) and ENUM('a','b') as DuckDB prints them,
// and nothing that can end the cast or the statement.
func SafeCastType(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	depth := 0
	hasAlpha := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasAlpha = true
		case c >= '0' && c <= '9', c == '_', c == ' ', c == '[', c == ']':
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth < 0 {
				return false
			}
		case c == ',':
			if depth == 0 {
				return false
			}
		case c == '"' || c == '\'':
			// A quote glued to a word is a prefixed string (E'..', N'..', B'..', U&'..') whose
			// escapes DuckDB reads differently from skipQuoted.
			if i > 0 && isWordByte(t[i-1]) {
				return false
			}
			end, ok := skipQuoted(t, i)
			if !ok {
				return false
			}
			i = end
		default:
			return false
		}
	}
	return hasAlpha && depth == 0
}

// skipQuoted returns the index of the closing quote of the section that opens at
// s[i], treating a doubled quote as an escape. ok is false when the section is
// unterminated or holds a control character.
func skipQuoted(s string, i int) (int, bool) {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[j] < 0x20 || s[j] == 0x7f:
			return 0, false
		case s[j] != q:
		case j+1 < len(s) && s[j+1] == q:
			j++
		default:
			return j, true
		}
	}
	return 0, false
}

// agentExprKeyword finds the expression-bearing keywords anywhere in the text, so
// an untokenizable column type still has its refusal name CHECK when its
// expression fails the character rules.
var agentExprKeyword = regexp.MustCompile(`(?i)\b(check|generated)\b`)

var agentNumber = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

const agentTypeHelp = "a DuckDB type with optional PRIMARY KEY, NOT NULL, UNIQUE, REFERENCES t(col) and a literal DEFAULT (a number, a single-quoted string, TRUE, FALSE, NULL, CURRENT_TIMESTAMP or CURRENT_DATE)"

// CheckAgentColumnType validates a column type string the agent supplied to
// create_table or add_column. It is stricter than ValidColumnType: DEFAULT must
// be a literal (so no function call, operator, cast or parenthesised
// expression), and CHECK, AS (generated column) and GENERATED are refused,
// because each of those is an expression the agent could use to compute at
// every insert. People's table designer keeps ValidColumnType.
func CheckAgentColumnType(t string) error {
	toks, reason := agentTokens(t)
	if reason != "" {
		if m := agentExprKeyword.FindString(t); m != "" {
			return fmt.Errorf("invalid column type %q: %s is not allowed for the agent; allowed: %s", t, strings.ToUpper(m), agentTypeHelp)
		}
		return fmt.Errorf("invalid column type %q: %s; allowed: %s", t, reason, agentTypeHelp)
	}
	for i, tok := range toks {
		switch strings.ToUpper(tok) {
		case "CHECK", "AS", "GENERATED":
			return fmt.Errorf("invalid column type %q: %s is not allowed for the agent; allowed: %s", t, strings.ToUpper(tok), agentTypeHelp)
		case "DEFAULT":
			if !agentLiteralAt(toks, i+1) {
				return fmt.Errorf("invalid column type %q: DEFAULT must be a literal; allowed: %s", t, agentTypeHelp)
			}
		}
	}
	return nil
}

// agentLiteralAt reports whether toks[i] is a default literal that is not
// followed by something that would extend it into an expression.
func agentLiteralAt(toks []string, i int) bool {
	if i >= len(toks) {
		return false
	}
	tok := toks[i]
	switch strings.ToUpper(tok) {
	case "TRUE", "FALSE", "NULL", "CURRENT_TIMESTAMP", "CURRENT_DATE":
	default:
		if !isStringTok(tok) && !agentNumber.MatchString(tok) {
			return false
		}
	}
	if i+1 < len(toks) {
		n := toks[i+1]
		if n == "(" || n == "[" || isStringTok(n) || agentNumber.MatchString(n) {
			return false
		}
	}
	return true
}

func isStringTok(s string) bool { return strings.HasPrefix(s, "'") }

// agentTokens splits t into words, single-quoted strings (kept whole, quotes
// included) and the punctuation ( ) , [ ]. Anything else, including a double
// quote, is refused with a reason; a string glued to a word (E'x') is too. The
// reason is empty when the text is accepted.
func agentTokens(t string) (toks []string, reason string) {
	t = strings.TrimSpace(t)
	depth := 0
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c == ' ' || c == '\t':
		case c == '\'':
			if i > 0 && isWordByte(t[i-1]) {
				return nil, "a string literal must stand alone"
			}
			end, ok := skipQuoted(t, i)
			if !ok {
				return nil, "unterminated string literal"
			}
			toks = append(toks, t[i:end+1])
			i = end
		case c == ',' && depth == 0:
			return nil, "a comma is only allowed inside parentheses"
		case strings.IndexByte("(),[]", c) >= 0:
			if depth += parenDelta(c); depth < 0 {
				return nil, "unbalanced parentheses"
			}
			toks = append(toks, string(c))
		case isWordByte(c) || isNumByte(c):
			j := i
			for j < len(t) && (isWordByte(t[j]) || isNumByte(t[j])) {
				j++
			}
			if w := t[i:j]; !agentNumber.MatchString(w) && strings.ContainsAny(w, "-.+") {
				return nil, "operators and punctuation are not allowed"
			}
			toks = append(toks, t[i:j])
			i = j - 1
		default:
			return nil, fmt.Sprintf("character %q is not allowed", c)
		}
	}
	if depth != 0 {
		return nil, "unbalanced parentheses"
	}
	if len(toks) == 0 || !isAlphaByte(toks[0][0]) {
		return nil, "it must start with a type name"
	}
	return toks, ""
}

func parenDelta(c byte) int {
	switch c {
	case '(':
		return 1
	case ')':
		return -1
	}
	return 0
}

func isNumByte(c byte) bool { return c == '-' || c == '.' || c == '+' }

func isAlphaByte(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
