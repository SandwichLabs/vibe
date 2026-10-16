package storageglob

import (
	"strings"
	"unicode/utf8"
)

// A small SQL tokenizer with byte offsets. It exists because the rewrite
// replaces exact source spans, so it needs the offsets DuckDB's own parser
// reports (query_location) to line up with tokens, and it only ever REFUSES to
// rewrite when the two disagree: the parser decides what a pattern is, the
// tokenizer only says where it sits and that it is a plain '...' literal.

type tokKind int

const (
	tkWord   tokKind = iota // bare word or number; Text is upper-cased
	tkIdent                 // "x"; Text is the content
	tkString                // 'x', E'x' or $tag$x$tag$; Text is the decoded content
	tkParen                 // "(" or ")"
	tkPunct                 // any other single character
)

type tok struct {
	Kind  tokKind
	Text  string
	Plain bool // tkString only: a standard '...' literal, closed, with no escape processing
	Start int  // byte offset in the source
	End   int
}

func (t tok) isWord(w string) bool  { return t.Kind == tkWord && t.Text == w }
func (t tok) isParen(p string) bool { return t.Kind == tkParen && t.Text == p }

func isWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r >= utf8.RuneSelf
}

// statements splits src at top-level semicolons and tokenizes each; empty
// statements are dropped. Comments are skipped (DuckDB's block comments nest).
func statements(src string) [][]tok {
	var out [][]tok
	var cur []tok
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
			i = skipBlockComment(src, i)
		case c == ';':
			end()
			i++
		case c == '\'' || c == '"':
			var t tok
			t, i = quoted(src, i, c, &cur)
			cur = append(cur, t)
		case c == '$':
			var t tok
			var ok bool
			if t, i, ok = dollarQuoted(src, i); ok {
				cur = append(cur, t)
			} else {
				cur = append(cur, tok{Kind: tkPunct, Text: "$", Start: i - 1, End: i})
			}
		case c == '(' || c == ')':
			cur = append(cur, tok{Kind: tkParen, Text: string(c), Start: i, End: i + 1})
			i++
		default:
			r, w := utf8.DecodeRuneInString(src[i:])
			switch {
			case isWordRune(r):
				j := i
				for j < len(src) {
					r2, w2 := utf8.DecodeRuneInString(src[j:])
					if !isWordRune(r2) {
						break
					}
					j += w2
				}
				cur = append(cur, tok{Kind: tkWord, Text: strings.ToUpper(src[i:j]), Start: i, End: j})
				i = j
			case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v':
				i += w
			default:
				cur = append(cur, tok{Kind: tkPunct, Text: string(r), Start: i, End: i + w})
				i += w
			}
		}
	}
	end()
	return out
}

func skipBlockComment(src string, i int) int {
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
	return i
}

// quoted lexes a '...' or "..." token starting at src[i]. An E prefix that the
// caller already appended as a word is folded into the string token (the
// parser reports the E as the literal's start, and the literal is not plain).
func quoted(src string, i int, q byte, cur *[]tok) (tok, int) {
	start := i
	escape := false
	if q == '\'' && len(*cur) > 0 {
		if p := (*cur)[len(*cur)-1]; p.Kind == tkWord && p.End == i && p.Text == "E" {
			escape = true
			start = p.Start
			*cur = (*cur)[:len(*cur)-1]
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
		if src[j] == q {
			if j+1 < len(src) && src[j+1] == q {
				b.WriteByte(q)
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
	kind := tkString
	if q == '"' {
		kind = tkIdent
	}
	return tok{Kind: kind, Text: b.String(), Plain: kind == tkString && !escape && closed, Start: start, End: min(j, len(src))}, j
}

// dollarQuoted lexes $tag$...$tag$ at src[i]; ok is false for a bare $ (or $1).
// On false the returned index is i+1.
func dollarQuoted(src string, i int) (tok, int, bool) {
	j := i + 1
	for j < len(src) && isWordRune(rune(src[j])) {
		j++
	}
	if j < len(src) && src[j] == '$' && (j == i+1 || !(src[i+1] >= '0' && src[i+1] <= '9')) {
		delim := src[i : j+1]
		k := strings.Index(src[j+1:], delim)
		text, endPos := src[j+1:], len(src)
		if k >= 0 {
			text = src[j+1 : j+1+k]
			endPos = j + 1 + k + len(delim)
		}
		return tok{Kind: tkString, Text: text, Start: i, End: endPos}, endPos, true
	}
	return tok{}, i + 1, false
}

// matchParen returns the index of the ")" closing the "(" at toks[open], or -1.
func matchParen(toks []tok, open int) int {
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
