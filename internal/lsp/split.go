package lsp

import "strings"

// Split returns the complete statements of src in order, each trimmed and
// without its ';', the text after the last ';' (rest), and whether rest ends
// inside an unterminated string, quoted name, dollar-quoted string or block
// comment (open). A ';' inside a string, quoted name, dollar-quoted string or
// comment is not a boundary. rest holding only whitespace and comments is
// returned as "". Empty statements (";;") are dropped.
//
// It is the console's statement splitter, so it has to agree with DuckDB's
// lexer about what is a string or a comment: a boundary it adds that DuckDB
// does not see would run a fragment of a comment or string as its own
// statement, and a boundary it misses merely leaves one text for DuckDB to
// split itself. That is why block comments nest and a dollar tag may hold
// non-ASCII letters, as in DuckDB. The console's differential test runs the
// same scripts through DuckDB whole and through Split piece by piece.
func Split(src string) (stmts []string, rest string, open bool) {
	toks := lex(src)
	start := 0
	for _, t := range toks {
		if t.kind != tokPunct || t.text != ";" {
			continue
		}
		if s := strings.TrimSpace(src[start:t.start]); hasCode(lex(s)) {
			stmts = append(stmts, s)
		}
		start = t.end
	}
	rest = strings.TrimSpace(src[start:])
	tail := lex(rest)
	if !hasCode(tail) {
		return stmts, "", false
	}
	for _, t := range tail {
		if t.open && t.kind != tokComment || t.kind == tokComment && t.open && strings.HasPrefix(rest[t.start:], "/*") {
			open = true
		}
	}
	return stmts, rest, open
}

// hasCode reports whether toks holds anything but comments.
func hasCode(toks []token) bool {
	for _, t := range toks {
		if t.kind != tokComment {
			return true
		}
	}
	return false
}
