package engine

import (
	"fmt"
	"strings"
)

// TerminalOpts selects how TerminalText treats the characters that need care.
type TerminalOpts struct {
	// KeepNewlines keeps \n (and turns \r into \n) for multi-line content such
	// as a notebook cell's source. Otherwise \n and \r become a space so a
	// value that must stay on one line (a table cell) cannot break the row.
	KeepNewlines bool
	// StripC1 also drops the C1 controls U+0080 to U+009F, which some
	// terminals treat as escape introducers.
	StripC1 bool
}

// TerminalText makes untrusted text safe to print to a terminal. A table cell,
// a query result, or a notebook cell's source is user- or agent-authored data,
// not trusted terminal input: without this, an embedded escape sequence could
// move the cursor, recolor the screen or spoof what is shown. Tabs become a
// space; every other C0 control and DEL, ESC included, is dropped, which
// neutralizes a sequence because the printable characters after the ESC turn
// into inert plain text.
func TerminalText(s string, o TerminalOpts) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			if o.KeepNewlines {
				b.WriteRune('\n')
			} else {
				b.WriteRune(' ')
			}
		case r == '\t':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// Dropped.
		case o.StripC1 && r >= 0x80 && r < 0xa0:
			// Dropped.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CellText is a result cell as display text: null for a SQL NULL, a string as
// it is, anything else through %v. It does not sanitize; pass the result to
// TerminalText.
func CellText(v any, null string) string {
	switch x := v.(type) {
	case nil:
		return null
	case string:
		return x
	}
	return fmt.Sprintf("%v", v)
}
