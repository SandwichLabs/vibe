package agents

import (
	"encoding/json"
	"unicode/utf8"
)

// RowResultBudgetChars caps the JSON size of a run_query row result before
// it reaches the model (chat, task, or MCP all dispatch through
// Handler.Handle, so applying this here — not in internal/llm or
// internal/mcp separately — covers all three surfaces at once). Ported from
// schoolwork-workspace's src/agent/tool-budget.js: a district table is large
// enough that `SELECT *` against it can run tens of thousands of rows — as
// much context as the rest of a conversation combined — and money spent on
// rows the model will never individually read. See
// docs/AI_CELL_AUTHORING.md "Result truncation".
const RowResultBudgetChars = 15_000

// UIEventPayloadBudgetBytes caps a single tool-result event forwarded to the
// Studio UI (AgentPanel). Independent of RowResultBudgetChars: this exists
// to keep one Wails/stream event small for the frontend, not to keep the
// model's context small — the row budget already did that. Applied in
// internal/llm's runner, not here, since it's a UI concern rather than a
// tool-contract one.
const UIEventPayloadBudgetBytes = 64 * 1024

const rowBudgetClipSuffix = "…[clipped]"

// wrapperOverhead reserves room for the fixed keys/punctuation of the
// {"columns":...,"rows":[...],"truncated":{"shown":N,"total":N,"note":"..."}}
// envelope around the row slice being sized here — generous rather than
// exact, since exactness would require re-marshaling the whole envelope on
// every candidate row count.
const wrapperOverhead = 512

// budgetRows truncates rows (each a positional slice aligned with the
// caller's column list) to fit within budgetChars once marshaled as JSON,
// cutting only on whole-row boundaries. Returns the rows to emit (unchanged
// when nothing needed cutting) and a truncated summary (nil when nothing was
// cut). A single row that alone exceeds the budget gets its long string
// values clipped (see clipOversizedRow) instead of being dropped entirely —
// dropping it would hand the caller zero rows for a query that matched real
// data, which is worse than a visibly-clipped one.
func budgetRows(rows [][]any) (out [][]any, truncated map[string]any) {
	const budgetChars = RowResultBudgetChars
	if len(rows) == 0 {
		return rows, nil
	}

	full, err := json.Marshal(rows)
	if err != nil || len(full) <= budgetChars {
		return rows, nil
	}

	rowStrs := make([][]byte, len(rows))
	bodyLen := 0
	shown := 0
	for i, row := range rows {
		s, err := json.Marshal(row)
		if err != nil {
			s = []byte("null")
		}
		rowStrs[i] = s
		add := len(s)
		if i > 0 {
			add++ // joining comma
		}
		if bodyLen+add+wrapperOverhead > budgetChars {
			break
		}
		bodyLen += add
		shown = i + 1
	}

	if shown == 0 {
		clipped := clipOversizedRow(rows[0], budgetChars)
		return [][]any{clipped}, map[string]any{
			"shown": 1,
			"total": len(rows),
			"note":  "Result budget reached, and even the first row alone exceeded it. Its long values were shortened (marked \"…[clipped]\") so the columns and shape are still visible — do not treat a clipped value as complete. Add LIMIT or select fewer columns.",
		}
	}

	return rows[:shown], map[string]any{
		"shown": shown,
		"total": len(rows),
		"note":  "Result budget reached. Add LIMIT or aggregate if you need the whole set.",
	}
}

// clipOversizedRow shortens long string values in a single row (a
// positional slice) so at least something goes out for a row too big for
// the budget on its own — see budgetRows's doc comment. Binary-searches the
// largest per-string clip length that still fits the budget so as much of
// each value survives as the budget allows.
func clipOversizedRow(row []any, maxChars int) []any {
	maxLen := 0
	for _, v := range row {
		if s, ok := v.(string); ok && len(s) > maxLen {
			maxLen = len(s)
		}
	}

	withClip := func(cap int) []any {
		candidate := make([]any, len(row))
		for i, v := range row {
			if s, ok := v.(string); ok && len(s) > cap {
				candidate[i] = SafeTruncate(s, cap) + rowBudgetClipSuffix
			} else {
				candidate[i] = v
			}
		}
		return candidate
	}
	fits := func(cap int) bool {
		b, err := json.Marshal(withClip(cap))
		return err == nil && len(b)+wrapperOverhead <= maxChars
	}

	if !fits(0) {
		// Not even zero-length strings fit — the size is in the row's shape
		// (a long array/object value), not its text. Show only the width.
		nulled := make([]any, len(row))
		return nulled
	}

	lo, hi := 0, maxLen
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return withClip(lo)
}

// SafeTruncate cuts s to at most n bytes without splitting a multi-byte
// UTF-8 rune in half (which would produce invalid UTF-8 and a mangled
// replacement-character tail once re-encoded/displayed). It walks backward
// from n to the start of the rune straddling that boundary, so the result
// is always <= n bytes, never > n. Exported so internal/llm's UI-event cap
// (capForUI) can reuse the same rune-safe truncation instead of a second,
// byte-slicing copy.
func SafeTruncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
