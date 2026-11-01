package repl

import "github.com/SandwichLabs/assay/internal/engine"

// Everything the console prints that it did not author goes through these two:
// engine.TerminalText with the C1 controls and the bidirectional format
// characters dropped as well (ESC is always dropped, so an escape sequence
// becomes inert text).
var (
	oneLineOpts   = engine.TerminalOpts{StripC1: true, StripBidi: true}
	multiLineOpts = engine.TerminalOpts{StripC1: true, StripBidi: true, KeepNewlines: true}
)

// safe is s as one line of terminal-safe text.
func safe(s string) string { return engine.TerminalText(s, oneLineOpts) }

// safeMultiline is s as terminal-safe text that keeps its line breaks.
func safeMultiline(s string) string { return engine.TerminalText(s, multiLineOpts) }

// isBidiRune reports a Unicode bidirectional format character.
func isBidiRune(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}
