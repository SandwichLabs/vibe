package repl

import (
	"sync"
	"unicode/utf8"
)

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// inputFilter cleans the byte stream before the line editor sees it, because
// the editor echoes what it is given: a C1 control (xterm.js reads U+009B as
// CSI), a bidirectional format character or the interrupt marker is dropped
// wherever it appears, and inside a bracketed paste (where the editor takes
// every key as text) so is every control character and ESC. It keeps state
// between calls, so a marker or a character split across two Input calls is
// still seen whole.
type inputFilter struct {
	mu    sync.Mutex
	carry []byte
	paste bool
}

func hasPrefixBytes(b, marker []byte) bool {
	return len(b) <= len(marker) && string(marker[:len(b)]) == string(b)
}

func (f *inputFilter) apply(p []byte) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	data := append(append([]byte{}, f.carry...), p...)
	f.carry = nil
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		rest := data[i:]
		switch b := rest[0]; {
		case b == 0x1b:
			var n int
			var stash bool
			if out, n, stash = f.escape(out, rest); stash {
				f.carry = append([]byte{}, rest...)
				return out
			}
			i += n
		case b < 0x80:
			out = f.control(out, b)
			i++
		default:
			if !utf8.FullRune(rest) {
				f.carry = append([]byte{}, rest...)
				return out
			}
			r, n := utf8.DecodeRune(rest)
			if r != utf8.RuneError && !isBidiRune(r) && !(r >= 0x80 && r < 0xa0) && r != interruptRune {
				out = append(out, rest[:n]...)
			}
			i += n
		}
	}
	return out
}

// escape handles an ESC: a paste marker passes (and flips the paste state), a
// possible start of one waits for more bytes, and any other ESC passes outside
// a paste and is dropped inside one.
func (f *inputFilter) escape(out, rest []byte) ([]byte, int, bool) {
	for i, m := range [][]byte{pasteStart, pasteEnd} {
		if len(rest) >= len(m) && string(rest[:len(m)]) == string(m) {
			f.paste = i == 0
			return append(out, m...), len(m), false
		}
		if len(rest) < len(m) && hasPrefixBytes(rest, m) {
			return out, 0, true
		}
	}
	if f.paste {
		return out, 1, false
	}
	return append(out, 0x1b), 1, false
}

// control passes an ASCII byte; inside a paste only line breaks and a space
// for a tab.
func (f *inputFilter) control(out []byte, b byte) []byte {
	if !f.paste || b >= 0x20 && b != 0x7f || b == '\r' || b == '\n' {
		return append(out, b)
	}
	if b == '\t' {
		return append(out, ' ')
	}
	return out
}

// filterState is what the filter remembers between calls.
type filterState struct {
	carry []byte
	paste bool
}

func (f *inputFilter) snapshot() filterState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return filterState{append([]byte{}, f.carry...), f.paste}
}

func (f *inputFilter) restore(st filterState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.carry, f.paste = st.carry, st.paste
}
