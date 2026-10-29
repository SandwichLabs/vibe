package repl

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// cellWidth is how many terminal cells s takes (grapheme-aware; ambiguous-width
// glyphs count as narrow, which is xterm.js's default too).
func cellWidth(s string) int { return runewidth.StringWidth(s) }

// padRight pads s with spaces on the right to w cells.
func padRight(s string, w int) string {
	if n := w - cellWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// padLeft pads s with spaces on the left to w cells.
func padLeft(s string, w int) string {
	if n := w - cellWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// center puts s in w cells; the odd space goes to the right.
func center(s string, w int) string {
	n := w - cellWidth(s)
	if n <= 0 {
		return s
	}
	left := n / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", n-left)
}

// wrapCell splits s into lines of at most w cells (hard wrap, whole
// characters). An empty s is one empty line.
func wrapCell(s string, w int) []string {
	if w < 1 || cellWidth(s) <= w {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	curW := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if curW+rw > w && curW > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW += rw
	}
	return append(lines, cur.String())
}
