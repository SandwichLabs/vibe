package lsp

import "time"

// withLiveDebounce shortens the rebuild debounce so tests do not wait on it.
func withLiveDebounce(d time.Duration) LiveOption { return func(l *Live) { l.debounce = d } }

// Complete is completeAt without the offset and the incomplete flag.
func Complete(text string, offset int, cat *Catalog) []Item {
	items, _, _ := completeAt(text, offset, cat)
	return items
}
