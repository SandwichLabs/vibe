package agents

import (
	"strings"
	"unicode/utf8"
)

// This file is the policy's own SQL lexer. It exists because the agent SQL
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

type lexKind int

const (
	lkWord   lexKind = iota // bare word or number; Text is upper-cased, Raw as written
	lkIdent                 // "x"; Text is the content, case kept
	lkString                // 'x', E'x' or $tag$x$tag$; Text is the decoded content
	lkParen                 // "(" or ")"
	lkPunct                 // any other single punctuation character
)

type lexTok struct {
	Kind  lexKind
	Text  string
	Raw   string
	Plain bool // lkString only: a standard '...' literal with no escape processing
	Start int  // byte offset in the source
	End   int
}

func (t lexTok) isWord(w string) bool  { return t.Kind == lkWord && t.Text == w }
func (t lexTok) isPunct(p string) bool { return t.Kind == lkPunct && t.Text == p }
func (t lexTok) isParen(p string) bool { return t.Kind == lkParen && t.Text == p }

// name reports whether t could name something: a bare word or a quoted
// identifier (DuckDB resolves "read_csv"(...) like read_csv(...)).
func (t lexTok) isName() bool { return t.Kind == lkWord || t.Kind == lkIdent }

func isLexWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r >= utf8.RuneSelf
}

// lexStatements splits src into statements at top-level semicolons and tokenizes
// each. Empty statements are dropped.
func lexStatements(src string) [][]lexTok {
	var out [][]lexTok
	var cur []lexTok
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
				if p := cur[len(cur)-1]; p.Kind == lkWord && p.End == i && p.Text == "E" {
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
			kind := lkString
			if c == '"' {
				kind = lkIdent
			}
			cur = append(cur, lexTok{Kind: kind, Text: b.String(), Raw: src[start:min(j, len(src))], Plain: kind == lkString && !escape && closed, Start: start, End: min(j, len(src))})
			i = j
		case c == '$':
			// $tag$ ... $tag$ dollar quoting; a bare $ (or $1) is punctuation.
			j := i + 1
			for j < len(src) && isLexWordRune(rune(src[j])) {
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
				cur = append(cur, lexTok{Kind: lkString, Text: text, Raw: src[i:endPos], Plain: false, Start: i, End: endPos})
				i = endPos
			} else {
				cur = append(cur, lexTok{Kind: lkPunct, Text: "$", Raw: "$", Start: i, End: i + 1})
				i++
			}
		case c == '(' || c == ')':
			cur = append(cur, lexTok{Kind: lkParen, Text: string(c), Raw: string(c), Start: i, End: i + 1})
			i++
		default:
			r, w := utf8.DecodeRuneInString(src[i:])
			switch {
			case isLexWordRune(r):
				j := i
				for j < len(src) {
					r2, w2 := utf8.DecodeRuneInString(src[j:])
					if !isLexWordRune(r2) {
						break
					}
					j += w2
				}
				raw := src[i:j]
				cur = append(cur, lexTok{Kind: lkWord, Text: strings.ToUpper(raw), Raw: raw, Start: i, End: j})
				i = j
			case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v':
				i += w
			default:
				cur = append(cur, lexTok{Kind: lkPunct, Text: string(r), Raw: string(r), Start: i, End: i + w})
				i += w
			}
		}
	}
	end()
	return out
}

// matchParen returns the index of the ")" closing the "(" at toks[open], or -1.
func matchParen(toks []lexTok, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch {
		case toks[i].isParen("("):
			depth++
		case toks[i].isParen(")"):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
