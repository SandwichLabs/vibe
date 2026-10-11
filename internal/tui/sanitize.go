package tui

import "github.com/SandwichLabs/vibe/internal/engine"

// sanitizeForTerminal is engine.TerminalText for the TUI: it strips control
// characters (ESC included) from untrusted text. Newlines are kept when
// allowNewlines is true; otherwise they and tabs collapse to a space.
func sanitizeForTerminal(s string, allowNewlines bool) string {
	return engine.TerminalText(s, engine.TerminalOpts{KeepNewlines: allowNewlines})
}
