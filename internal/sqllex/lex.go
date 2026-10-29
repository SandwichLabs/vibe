// Package sqllex is the SQL policy's own lexer, a leaf shared by internal/agents
// and internal/queries. It exists because the agent SQL
// policy (#116) needs three things internal/usersql.Tokens deliberately drops:
// punctuation (a path argument is "( 'literal' ," or "( 'literal' )", and a
// dotted name needs its dots), byte offsets (to hand one statement of a script
// back to DuckDB), and whether a string literal is a plain '...' (the only form
// whose text is certain to be what DuckDB reads: E'..', $$..$$, U&'..' and
// adjacent-literal concatenation all decode differently, so none is accepted as
// a path).
//
// It follows the same lexical rules as usersql (line and block comments, '..'
// and ".." with doubled-quote escapes, E'..' with backslash escapes, $tag$..$tag$)
// and is only ever used to REFUSE: a statement it cannot make sense of is denied
// ("couldn't verify this query"), never waved through. DuckDB's own parser
// (duckdb_extract_statements) cross-checks the statement count.
package sqllex

import (
	"strings"
	"unicode/utf8"
)

// Kind is the class of a token.
type Kind int

const (
	Word   Kind = iota // bare word or number; Text is upper-cased, Raw as written
	Ident              // "x"; Text is the content, case kept
	String             // 'x', E'x' or $tag$x$tag$; Text is the decoded content
	Paren              // "(" or ")"
	Punct              // any other single punctuation character
)

// Tok is one token with its offsets in the source.
type Tok struct {
	Kind  Kind
	Text  string
	Raw   string
	Plain bool // String only: a standard '...' literal with no escape processing
	Start int  // byte offset in the source
	End   int
}

// IsWord reports a bare word equal to w (upper-case).
func (t Tok) IsWord(w string) bool { return t.Kind == Word && t.Text == w }

// IsPunct reports the punctuation character p.
func (t Tok) IsPunct(p string) bool { return t.Kind == Punct && t.Text == p }

// IsParen reports the parenthesis p.
func (t Tok) IsParen(p string) bool { return t.Kind == Paren && t.Text == p }

// IsName reports whether t could name something: a bare word or a quoted
// identifier (DuckDB resolves "read_csv"(...) like read_csv(...)).
func (t Tok) IsName() bool { return t.Kind == Word || t.Kind == Ident }

// IsWordRune reports a rune that can be part of a bare word.
func IsWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r >= utf8.RuneSelf
}

// Statements splits src into statements at top-level semicolons and tokenizes
// each. Empty statements are dropped.
func Statements(src string) [][]Tok {
	var out [][]Tok
	var cur []Tok
	end := func() {
		if len(cur) > 0 {
			out = append(out, cur)
		}
		cur = nil
	}
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			// DuckDB block comments nest.
			depth := 1
			i += 2
			for i < len(src) && depth > 0 {
				switch {
				case strings.HasPrefix(src[i:], "/*"):
					depth++
					i += 2
				case strings.HasPrefix(src[i:], "*/"):
					depth--
					i += 2
				default:
					i++
				}
			}
		case c == ';':
			end()
			i++
		case c == '\'' || c == '"':
			start := i
			escape := false
			// E'..' : the word E directly before the quote.
			if c == '\'' && len(cur) > 0 {
				if p := cur[len(cur)-1]; p.Kind == Word && p.End == i && p.Text == "E" {
					escape = true
					start = p.Start
					cur = cur[:len(cur)-1]
				}
			}
			var b strings.Builder
			j := i + 1
			closed := false
			for j < len(src) {
				if escape && src[j] == '\\' && j+1 < len(src) {
					_, w := utf8.DecodeRuneInString(src[j+1:])
					b.WriteString(src[j+1 : j+1+w])
					j += 1 + w
					continue
				}
				if src[j] == c {
					if j+1 < len(src) && src[j+1] == c {
						b.WriteByte(c)
						j += 2
						continue
					}
					closed = true
					j++
					break
				}
				b.WriteByte(src[j])
				j++
			}
			kind := String
			if c == '"' {
				kind = Ident
			}
			cur = append(cur, Tok{Kind: kind, Text: b.String(), Raw: src[start:min(j, len(src))], Plain: kind == String && !escape && closed, Start: start, End: min(j, len(src))})
			i = j
		case c == '$':
			// $tag$ ... $tag$ dollar quoting; a bare $ (or $1) is punctuation.
			j := i + 1
			for j < len(src) && IsWordRune(rune(src[j])) {
				j++
			}
			if j < len(src) && src[j] == '$' && (j == i+1 || !(src[i+1] >= '0' && src[i+1] <= '9')) {
				delim := src[i : j+1]
				k := strings.Index(src[j+1:], delim)
				var text string
				endPos := len(src)
				if k < 0 {
					text = src[j+1:]
				} else {
					text = src[j+1 : j+1+k]
					endPos = j + 1 + k + len(delim)
				}
				cur = append(cur, Tok{Kind: String, Text: text, Raw: src[i:endPos], Plain: false, Start: i, End: endPos})
				i = endPos
			} else {
				cur = append(cur, Tok{Kind: Punct, Text: "$", Raw: "$", Start: i, End: i + 1})
				i++
			}
		case c == '(' || c == ')':
			cur = append(cur, Tok{Kind: Paren, Text: string(c), Raw: string(c), Start: i, End: i + 1})
			i++
		default:
			r, w := utf8.DecodeRuneInString(src[i:])
			switch {
			case IsWordRune(r):
				j := i
				if r >= '0' && r <= '9' {
					// A numeric literal ends where DuckDB's does: "1a$b$" is 1 then
					// the name a$b$, "1e5$b$" is a number then a dollar quote.
					j = lexNumberEnd(src, i)
				} else {
					// An identifier may hold "$" (x$a$ is one name, not x and a quote).
					for j < len(src) {
						r2, w2 := utf8.DecodeRuneInString(src[j:])
						if !IsWordRune(r2) && r2 != '$' {
							break
						}
						j += w2
					}
				}
				raw := src[i:j]
				cur = append(cur, Tok{Kind: Word, Text: strings.ToUpper(raw), Raw: raw, Start: i, End: j})
				i = j
			case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v':
				i += w
			default:
				cur = append(cur, Tok{Kind: Punct, Text: string(r), Raw: string(r), Start: i, End: i + w})
				i += w
			}
		}
	}
	end()
	return out
}

// MatchParen returns the index of the ")" closing the "(" at toks[open], or -1.
func MatchParen(toks []Tok, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch {
		case toks[i].IsParen("("):
			depth++
		case toks[i].IsParen(")"):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// lexNumberEnd returns the end of the numeric literal at src[i] (a digit):
// digits with "_" between them, an optional fraction, and an exponent only
// when digits follow it.
func lexNumberEnd(src string, i int) int {
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	digits := func(j int) int {
		for j < len(src) && (isDigit(src[j]) || src[j] == '_' && j > 0 && isDigit(src[j-1]) && j+1 < len(src) && isDigit(src[j+1])) {
			j++
		}
		return j
	}
	j := digits(i)
	if j < len(src) && src[j] == '.' {
		j = digits(j + 1)
	}
	if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
		k := j + 1
		if k < len(src) && (src[k] == '+' || src[k] == '-') {
			k++
		}
		if k < len(src) && isDigit(src[k]) {
			j = digits(k)
		}
	}
	return j
}
