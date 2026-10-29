package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// legacyCLIClean and legacyTUIClean are the two sanitizers TerminalText replaced
// (cmd/query.go cleanCell and tui.sanitizeForTerminal), kept verbatim so the
// output of the shared one is pinned byte for byte.
func legacyCLIClean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
}

func legacyTUIClean(s string, allowNewlines bool) string {
	var b strings.Builder
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
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var terminalInputs = []string{
	"", "plain", "tab\there", "line1\nline2\r\nline3\rend", "\x1b[31mred\x1b[0m",
	"del\x7fchar", "c1\u0085next\u009bcsi", "nul\x00byte", "bell\x07", "unicode é 日本 🙂",
	"\x1b]0;title\x07", "mix\t\n\x1b\u0080\u009f end", "  ",
}

func TestTerminalText_MatchesBothLegacySanitizers(t *testing.T) {
	for _, in := range terminalInputs {
		assert.Equal(t, legacyCLIClean(in), TerminalText(in, TerminalOpts{StripC1: true}), "cli %q", in)
		assert.Equal(t, legacyTUIClean(in, false), TerminalText(in, TerminalOpts{}), "tui one-line %q", in)
		assert.Equal(t, legacyTUIClean(in, true), TerminalText(in, TerminalOpts{KeepNewlines: true}), "tui multi-line %q", in)
	}
}

func TestCellText(t *testing.T) {
	assert.Equal(t, "", CellText(nil, ""))
	assert.Equal(t, "NULL", CellText(nil, "NULL"))
	assert.Equal(t, "s", CellText("s", "NULL"))
	assert.Equal(t, "42", CellText(42, ""))
	assert.Equal(t, fmt.Sprintf("%v", []int{1}), CellText([]int{1}, ""))
}

func TestTerminalText_StripBidi(t *testing.T) {
	for _, r := range []rune{0x061c, 0x200e, 0x200f, 0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069} {
		s := "a" + string(r) + "b"
		assert.Equal(t, s, TerminalText(s, TerminalOpts{}), "kept without the option: %U", r)
		assert.Equal(t, "ab", TerminalText(s, TerminalOpts{StripBidi: true}), "%U", r)
	}
	// Letters of right-to-left scripts are text, not format characters.
	assert.Equal(t, "שלום مرحبا", TerminalText("שלום مرحبا", TerminalOpts{StripBidi: true, StripC1: true}))
}
