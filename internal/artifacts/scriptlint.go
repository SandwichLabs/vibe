package artifacts

import (
	"fmt"
	"strings"
)

// CheckScriptSyntax is a cheap structural check of view.js for the mistakes
// that make a script fail to load: an unterminated string, template literal,
// comment or regular expression, and unbalanced or mismatched (), [] and {}.
// It also requires the text "draw" somewhere, which the worker host calls (a
// substring test: self["draw"] and globalThis.draw both define it).
//
// It is NOT a JavaScript parser (no pure-Go one is a dependency of this
// module, and one would have to track the newest syntax the webview accepts or
// refuse valid scripts). It reads strings, template literals, comments and
// regular expression literals well enough to count brackets, and it reports
// only what it is sure of: every problem is one a browser would also reject.
// Returned problems carry a line number. src must be valid UTF-8.
func CheckScriptSyntax(src string) []string {
	sc := &jsScan{src: []rune(src), line: 1}
	sc.run()
	problems := sc.problems
	if len(problems) == 0 && !strings.Contains(src, "draw") {
		problems = append(problems, ScriptFile+" defines no draw: the worker calls a global function draw({params, theme, width, height}) (function draw(ctx) { ... } or self.draw = ...)")
	}
	return problems
}

type jsBracket struct {
	r    rune
	line int
	cond bool // a "(" that follows if/while/for/with
}

type jsScan struct {
	src       []rune
	pos       int
	line      int
	stack     []jsBracket
	last      rune // last significant character, for regex-or-division
	lastWord  string
	condParen bool // the ")" just closed ended an if/while/for/with condition
	problems  []string
}

func (s *jsScan) fail(line int, format string, args ...any) {
	if len(s.problems) < 3 {
		s.problems = append(s.problems, fmt.Sprintf("%s line %d: %s", ScriptFile, line, fmt.Sprintf(format, args...)))
	}
}

func (s *jsScan) peek(off int) rune {
	if s.pos+off < len(s.src) {
		return s.src[s.pos+off]
	}
	return 0
}

func (s *jsScan) run() {
	for s.pos < len(s.src) && len(s.problems) == 0 {
		s.step()
	}
	for len(s.problems) == 0 && len(s.stack) > 0 {
		b := s.stack[len(s.stack)-1]
		s.fail(b.line, "%q is never closed", string(b.r))
	}
}

func (s *jsScan) skipLine() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

func (s *jsScan) skipBlockComment() {
	start := s.line
	s.pos += 2
	for s.pos < len(s.src) {
		if s.src[s.pos] == '*' && s.peek(1) == '/' {
			s.pos += 2
			return
		}
		if s.src[s.pos] == '\n' {
			s.line++
		}
		s.pos++
	}
	s.fail(start, "a /* comment is never closed")
}

func (s *jsScan) skipString(q rune) {
	start := s.line
	s.pos++
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			switch {
			case s.peek(1) == '\r' && s.peek(2) == '\n':
				s.line++
				s.pos += 3
			case s.peek(1) == '\n':
				s.line++
				s.pos += 2
			default:
				s.pos += 2
			}
			continue
		case q:
			s.pos++
			s.last = q
			return
		case '\n':
			s.fail(start, "a string starting with %c is not closed before the end of the line", q)
			return
		}
		s.pos++
	}
	s.fail(start, "a string starting with %c is never closed", q)
}

// skipTemplate reads a template literal, including nested ${ ... } holes (which
// may hold strings, templates and braces of their own).
func (s *jsScan) skipTemplate() {
	start := s.line
	s.pos++
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			s.pos += 2
			continue
		case '`':
			s.pos++
			s.last = '`'
			return
		case '\n':
			s.line++
		case '$':
			if s.peek(1) == '{' {
				s.pos += 2
				if !s.skipHole() {
					return
				}
				continue
			}
		}
		s.pos++
	}
	s.fail(start, "a template literal (`) is never closed")
}

// skipHole scans a ${ ... } hole to its closing brace with a sub-scanner that
// shares nothing but the line count.
func (s *jsScan) skipHole() bool {
	start := s.line
	sub := &jsScan{src: s.src, pos: s.pos, line: s.line}
	sub.stack = []jsBracket{{'$', s.line, false}}
	for sub.pos < len(sub.src) && len(sub.problems) == 0 && len(sub.stack) > 0 {
		sub.step()
	}
	s.line, s.pos = sub.line, sub.pos
	if len(sub.problems) > 0 {
		s.problems = append(s.problems, sub.problems...)
		return false
	}
	if len(sub.stack) > 0 {
		s.fail(start, "a ${ in a template literal is never closed")
		return false
	}
	return true
}

// step runs one iteration of run's loop.
func (s *jsScan) step() {
	c := s.src[s.pos]
	switch {
	case c == '\n':
		s.line++
		s.pos++
	case c == ' ' || c == '\t' || c == '\r':
		s.pos++
	case c == '/' && s.peek(1) == '/':
		s.skipLine()
	case c == '/' && s.peek(1) == '*':
		s.skipBlockComment()
	case c == '"' || c == '\'':
		s.skipString(c)
	case c == '`':
		s.skipTemplate()
	case c == '/' && s.regexAllowed():
		s.skipRegex()
	case isJSIdentStart(c):
		s.word()
	default:
		s.punct(c)
	}
}

// regexAllowed reports whether a "/" here starts a regular expression: after
// an operator, an opening bracket, a comma, a semicolon, the start of the
// file, or a keyword that takes an expression. After an identifier, a number or
// a closing bracket it is division.
func (s *jsScan) regexAllowed() bool {
	switch s.last {
	case 0, '(', '[', '{', ',', ';', ':', '=', '!', '&', '|', '?', '+', '-', '*', '%', '<', '>', '~', '^':
		return true
	case ')':
		return s.condParen
	case ']', '}', '"', '\'', '`':
		return false
	}
	switch s.lastWord {
	case "return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else", "yield", "await":
		return true
	}
	return false
}

func (s *jsScan) skipRegex() {
	start := s.line
	s.pos++
	inClass := false
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == '\\':
			s.pos += 2
			continue
		case c == '\n':
			s.fail(start, "a regular expression is not closed before the end of the line")
			return
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			s.pos++
			for s.pos < len(s.src) && isJSIdentPart(s.src[s.pos]) {
				s.pos++
			}
			s.last, s.lastWord = '/', ""
			return
		}
		s.pos++
	}
	s.fail(start, "a regular expression is never closed")
}

func (s *jsScan) word() {
	start := s.pos
	for s.pos < len(s.src) && isJSIdentPart(s.src[s.pos]) {
		s.pos++
	}
	w := string(s.src[start:s.pos])
	s.last, s.lastWord = 'a', w
}

func (s *jsScan) punct(c rune) {
	s.pos++
	prevWord := s.lastWord
	s.lastWord = ""
	switch c {
	case '(', '[', '{':
		cond := c == '(' && (prevWord == "if" || prevWord == "while" || prevWord == "for" || prevWord == "with")
		s.stack = append(s.stack, jsBracket{c, s.line, cond})
	case '+', '-':
		// A postfix ++ or -- ends an operand, so a "/" after it divides.
		if s.last == c && s.pos >= 2 && s.src[s.pos-2] == c {
			s.last = 'a'
			return
		}
	case ')', ']', '}':
		s.closeBracket(c)
	}
	s.last = c
}

var jsPair = map[rune]rune{')': '(', ']': '[', '}': '{'}

func (s *jsScan) closeBracket(c rune) {
	n := len(s.stack)
	if n == 0 {
		s.fail(s.line, "%q has no matching %q", string(c), string(jsPair[c]))
		return
	}
	top := s.stack[n-1]
	if top.r == '$' && c == '}' {
		s.stack = s.stack[:n-1]
		return
	}
	if top.r != jsPair[c] {
		s.fail(s.line, "%q closes the %q opened on line %d", string(c), string(top.r), top.line)
		return
	}
	s.stack = s.stack[:n-1]
	s.condParen = c == ')' && top.cond
}

func isJSIdentStart(c rune) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c > 0x7f || c >= '0' && c <= '9'
}

func isJSIdentPart(c rune) bool { return isJSIdentStart(c) }
