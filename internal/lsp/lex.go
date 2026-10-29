package lsp

import "strings"

// tokKind is a lexical class: just enough SQL to find where the cursor is.
type tokKind int

const (
	tokWord   tokKind = iota // identifier or keyword
	tokQuoted                // "quoted identifier"
	tokString                // 'string literal'
	tokNumber
	tokPunct // one character: . , ( ) ; = and the rest
	tokComment
	tokParam // $1 or $name
)

type token struct {
	kind       tokKind
	text       string // for tokQuoted, the name without quotes
	start, end int    // byte offsets into the source
	// open marks a string, quoted name or block comment that runs off the end
	// of the text (no closing quote yet), and every -- comment.
	open bool
}

// upper is the word in upper case (keywords compare this way).
func (t token) upper() string { return strings.ToUpper(t.text) }

// isName reports whether t can name a catalog object.
func (t token) isName() bool {
	return t.kind == tokQuoted || (t.kind == tokWord && !isKeyword(t.upper()))
}

// lex splits SQL into tokens. It never fails: an unterminated string, quoted
// name or block comment runs to the end of the input, which is what an editor
// mid-keystroke looks like.
func lex(src string) []token {
	var toks []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			j := strings.IndexAny(src[i:], "\r\n")
			end := len(src)
			if j >= 0 {
				end = i + j
			}
			toks = append(toks, token{kind: tokComment, start: i, end: end, open: true})
			i = end
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end, open := scanBlockComment(src, i)
			toks = append(toks, token{kind: tokComment, start: i, end: end, open: open})
			i = end
		case c == '$':
			tok := scanDollar(src, i)
			toks = append(toks, tok)
			i = tok.end
		case (c == 'e' || c == 'E') && i+1 < len(src) && src[i+1] == '\'':
			// E'...': backslash escapes the next character.
			end, text, open := scanString(src, i+1, '\'', true)
			toks = append(toks, token{kind: tokString, text: text, start: i, end: end, open: open})
			i = end
		case c == '\'' || c == '"':
			end, text, open := scanQuoted(src, i, c)
			kind := tokString
			if c == '"' {
				kind = tokQuoted
			}
			toks = append(toks, token{kind: kind, text: text, start: i, end: end, open: open})
			i = end
		case isWordStart(c):
			j := i + 1
			for j < len(src) && isWordPart(src[j]) {
				j++
			}
			toks = append(toks, token{kind: tokWord, text: src[i:j], start: i, end: j})
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.' || src[j] == '_') {
				j++
			}
			toks = append(toks, token{kind: tokNumber, text: src[i:j], start: i, end: j})
			i = j
		default:
			toks = append(toks, token{kind: tokPunct, text: src[i : i+1], start: i, end: i + 1})
			i++
		}
	}
	return toks
}

// scanBlockComment reads the /* */ comment starting at src[i]. As in DuckDB
// (and PostgreSQL) block comments nest: "/* a /* b */ c */" is one comment. It
// returns the end offset and whether the comment is still open at the end.
func scanBlockComment(src string, i int) (int, bool) {
	depth := 1
	j := i + 2
	for j < len(src) {
		switch {
		case src[j] == '/' && j+1 < len(src) && src[j+1] == '*':
			depth++
			j += 2
		case src[j] == '*' && j+1 < len(src) && src[j+1] == '/':
			depth--
			j += 2
			if depth == 0 {
				return j, false
			}
		default:
			j++
		}
	}
	return len(src), true
}

// scanQuoted reads a quoted run starting at src[i] == q, where a doubled quote
// is an escaped one. It returns the end offset, the unquoted text and whether
// the run is still open at the end of src.
func scanQuoted(src string, i int, q byte) (int, string, bool) {
	return scanString(src, i, q, false)
}

// scanString is scanQuoted with, when escapes is set, a backslash escaping the
// next character (E'...' strings).
func scanString(src string, i int, q byte, escapes bool) (int, string, bool) {
	var b strings.Builder
	j := i + 1
	for j < len(src) {
		if escapes && src[j] == '\\' && j+1 < len(src) {
			b.WriteByte(src[j])
			b.WriteByte(src[j+1])
			j += 2
			continue
		}
		if src[j] == q {
			if j+1 < len(src) && src[j+1] == q {
				b.WriteByte(q)
				j += 2
				continue
			}
			return j + 1, b.String(), false
		}
		b.WriteByte(src[j])
		j++
	}
	return len(src), b.String(), true
}

// scanDollar reads the token starting at a "$": a dollar-quoted string
// ($$...$$ or $tag$...$tag$, text = the content, open when the closing tag is
// missing), a parameter ($1, $name), or a lone "$" punctuation character.
func scanDollar(src string, i int) token {
	j := i + 1
	for j < len(src) && isTagByte(src[j]) {
		j++
	}
	digitFirst := j > i+1 && src[i+1] >= '0' && src[i+1] <= '9'
	if j < len(src) && src[j] == '$' && !digitFirst {
		delim := src[i : j+1]
		body := j + 1
		if k := strings.Index(src[body:], delim); k >= 0 {
			return token{kind: tokString, text: src[body : body+k], start: i, end: body + k + len(delim)}
		}
		return token{kind: tokString, text: src[body:], start: i, end: len(src), open: true}
	}
	if j > i+1 {
		return token{kind: tokParam, text: src[i:j], start: i, end: j}
	}
	return token{kind: tokPunct, text: "$", start: i, end: i + 1}
}

func isTagByte(c byte) bool {
	return c == '_' || c >= 0x80 || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isWordStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isWordPart(c byte) bool { return isWordStart(c) || c >= '0' && c <= '9' || c == '$' }

// statementBounds returns the [start, end) of the statement holding offset:
// the text between the semicolons around it, ignoring those in strings,
// quoted names and comments.
func statementBounds(toks []token, offset, n int) (int, int) {
	start, end := 0, n
	for _, t := range toks {
		if t.kind != tokPunct || t.text != ";" {
			continue
		}
		if t.end <= offset {
			start = t.end
		} else if t.start >= offset {
			end = t.start
			break
		}
	}
	return start, end
}

// keywords is the subset of SQL keywords the completer offers and treats as
// non-names. DuckDB-specific shorthands (FROM-first, SUMMARIZE, PIVOT, QUALIFY,
// EXCLUDE) are included.
var keywords = []string{
	"SELECT", "FROM", "WHERE", "GROUP", "BY", "ORDER", "HAVING", "LIMIT", "OFFSET",
	"JOIN", "LEFT", "RIGHT", "FULL", "INNER", "OUTER", "CROSS", "ON", "USING", "AS",
	"AND", "OR", "NOT", "IN", "IS", "NULL", "LIKE", "ILIKE", "BETWEEN", "CASE", "WHEN",
	"THEN", "ELSE", "END", "DISTINCT", "ALL", "UNION", "INTERSECT", "EXCEPT", "WITH",
	"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "CREATE", "TABLE", "VIEW",
	"DROP", "ALTER", "DESCRIBE", "SUMMARIZE", "SHOW", "EXPLAIN", "ANALYZE", "PIVOT",
	"UNPIVOT", "QUALIFY", "EXCLUDE", "REPLACE", "ASC", "DESC", "NULLS", "FIRST", "LAST",
	"TRUE", "FALSE", "CAST", "OVER", "PARTITION", "WINDOW", "FILTER", "RECURSIVE",
	"ASOF", "POSITIONAL", "ANTI", "SEMI", "LATERAL", "NATURAL", "COPY", "TO",
}

var keywordSet = func() map[string]bool {
	m := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		m[k] = true
	}
	return m
}()

func isKeyword(upper string) bool { return keywordSet[upper] }

// relationKeywords are followed by a table (or a list of them, for FROM).
var relationKeywords = map[string]bool{
	"FROM": true, "JOIN": true, "INTO": true, "UPDATE": true, "TABLE": true,
	"DESCRIBE": true, "SUMMARIZE": true, "PIVOT": true, "UNPIVOT": true,
}
