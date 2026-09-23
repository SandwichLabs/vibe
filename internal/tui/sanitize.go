package tui

import "strings"

// sanitizeForTerminal strips ASCII control characters — including ESC
// (0x1b), which begins an ANSI escape sequence — from untrusted text before
// it reaches the terminal. A table cell, a query result, or a notebook
// cell's source is user/agent-authored data, not trusted terminal control
// input: without this, a value containing an embedded escape sequence could
// move the cursor, change colors, or clear the screen, corrupting (or in
// the worst case spoofing) what the TUI displays. Dropping the ESC byte is
// what actually neutralizes a sequence — the printable characters that
// would have followed it become inert plain text instead.
//
// Newlines are preserved when allowNewlines is true (multi-line content
// like a notebook cell's markdown/sql source); otherwise they, and tabs,
// collapse to a single space so a value that must stay on one line (a
// table cell) can't break the row layout.
func sanitizeForTerminal(s string, allowNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			if allowNewlines {
				b.WriteRune('\n')
			} else {
				b.WriteRune(' ')
			}
		case r == '\t':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// Drop every other C0 control and DEL, including ESC.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
