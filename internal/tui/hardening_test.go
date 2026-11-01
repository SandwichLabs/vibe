package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/notebooks"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeForTerminal_StripsControlAndEscapeChars(t *testing.T) {
	// \x1b[31m is an ANSI "red" escape sequence; \x07 is BEL.
	in := "hello\x1b[31mRED\x1b[0mworld\x07!"
	out := sanitizeForTerminal(in, false)
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "\x07")
	// The ESC byte itself is dropped; the printable text that followed it
	// (no longer an active escape sequence once ESC is gone) is left as
	// inert plain text rather than being parsed/removed as a "sequence" —
	// sanitizeForTerminal doesn't parse ANSI grammar, it just removes what
	// makes it dangerous.
	assert.Equal(t, "hello[31mRED[0mworld!", out)
}

func TestSanitizeForTerminal_NewlineHandling(t *testing.T) {
	in := "line1\nline2\ttabbed"
	assert.Equal(t, "line1\nline2 tabbed", sanitizeForTerminal(in, true))
	assert.Equal(t, "line1 line2 tabbed", sanitizeForTerminal(in, false))
}

func TestFormatCellValue_NullAndNaNInf(t *testing.T) {
	assert.Equal(t, "NULL", formatCellValue(nil))
	assert.Equal(t, "NaN", formatCellValue(math.NaN()))
	assert.Equal(t, "+Inf", formatCellValue(math.Inf(1)))
	assert.Equal(t, "-Inf", formatCellValue(math.Inf(-1)))
	assert.Equal(t, "42", formatCellValue(42))
}

func TestFormatCellValue_StripsEscapeSequences(t *testing.T) {
	v := "safe\x1b[2Jtext"
	assert.NotContains(t, formatCellValue(v), "\x1b")
}

func TestFormatResultSet_TruncatesAtRowCap(t *testing.T) {
	cols := []engine.Column{{Name: "n", Type: "INTEGER"}}
	rows := make([][]any, replRowCap+50)
	for i := range rows {
		rows[i] = []any{i}
	}
	rs := &engine.ResultSet{Columns: cols, Rows: rows}
	out := formatResultSet(rs, 80)
	assert.Contains(t, out, "1000+ rows")
	assert.NotContains(t, out, "1050")
	assert.NotContains(t, out, "at least")
	assert.Contains(t, out, "(1000 shown)")
	// Data value for row 1050 (index 1049, beyond the cap) must not appear.
	assert.NotContains(t, out, "1049")
}

func TestFormatResultSet_NoTruncationUnderCap(t *testing.T) {
	cols := []engine.Column{{Name: "n", Type: "INTEGER"}}
	rs := &engine.ResultSet{Columns: cols, Rows: [][]any{{1}, {2}}}
	out := formatResultSet(rs, 80)
	assert.NotContains(t, out, "more row(s) not shown")
	assert.Contains(t, out, "│     2 │")
	assert.Contains(t, out, "int32")
	assert.NotContains(t, out, "shown")
}

func TestFormatResultSet_NullValueRendersAsNULL(t *testing.T) {
	cols := []engine.Column{{Name: "n", Type: "INTEGER"}}
	rs := &engine.ResultSet{Columns: cols, Rows: [][]any{{nil}}}
	out := formatResultSet(rs, 80)
	assert.Contains(t, out, "NULL")
}

func TestCapRowsIfSafe(t *testing.T) {
	cases := []struct {
		query       string
		readOnly    bool
		wantWrapped bool
	}{
		{"SELECT * FROM t", true, true},
		{"  select * from t  ", true, true},
		{"WITH x AS (SELECT 1) SELECT * FROM x", true, true},
		{"SELECT * FROM t LIMIT 10", true, false}, // already has a LIMIT
		{"SHOW TABLES", true, false},              // not a valid subquery
		{"DESCRIBE t", true, false},
		{"PRAGMA table_info('t')", true, false},
		{"PRAGMA version", true, false},
		{"SELECT 1; SELECT 2", true, false}, // multi-statement, even if (hypothetically) classified read
		{"", true, false},
		// The caller-supplied readOnly=false is authoritative: even a
		// SELECT-shaped string must never be wrapped when the real
		// classifier (agents.IsReadOnlyStatement, called by
		// executeREPLQuery before capRowsIfSafe) said it wasn't actually a
		// read — e.g. `WITH x AS (INSERT INTO t ...) SELECT * FROM x`
		// LOOKS like a SELECT by prefix, but is a real write.
		{"WITH x AS (INSERT INTO t VALUES (1) RETURNING 1) SELECT * FROM x", false, false},
		{"INSERT INTO t VALUES (1)", false, false},
	}
	for _, c := range cases {
		got := capRowsIfSafe(c.query, c.readOnly)
		if c.wantWrapped {
			assert.Contains(t, got, "_slaw_repl_cap", "expected %q (readOnly=%v) to be wrapped", c.query, c.readOnly)
			assert.Contains(t, got, "LIMIT")
		} else {
			assert.Equal(t, c.query, got, "expected %q (readOnly=%v) to pass through unchanged", c.query, c.readOnly)
		}
	}
}

func TestREPLModel_AppendOutput_CapsBuffer(t *testing.T) {
	r := NewREPLModel()
	big := strings.Repeat("x", replOutputBufferCap+10000)
	r.AppendOutput(big)
	assert.LessOrEqual(t, len(r.OutputContent()), replOutputBufferCap+len("… (earlier output truncated) …\n"))
	assert.Contains(t, r.OutputContent(), "earlier output truncated")
}

func TestREPLModel_AppendOutput_StripsEscapeSequences(t *testing.T) {
	r := NewREPLModel()
	r.AppendOutput("hello\x1b[31mworld")
	assert.NotContains(t, r.OutputContent(), "\x1b")
}

func TestDumpNotebook_SanitizesSourceAndTitle(t *testing.T) {
	nb := notebooks.Notebook{
		Title: "Evil\x1b[2JTitle",
		Cells: []notebooks.Cell{
			{ID: "c1", Type: notebooks.CellMarkdown, Source: "hello\x1b[31mworld"},
		},
	}
	out := dumpNotebook(nb)
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, "Evil[2JTitle")
	assert.Contains(t, out, "hello[31mworld")
}

func TestFormatResultSet_NoColumns(t *testing.T) {
	assert.Equal(t, "-- OK (no results)", formatResultSet(&engine.ResultSet{}, 80))
}

func TestFormatResultSet_StripsEscapes(t *testing.T) {
	rs := &engine.ResultSet{Columns: []engine.Column{{Name: "s\x1b[31m", Type: "VARCHAR"}}, Rows: [][]any{{"a\x1b[2Jb"}}}
	assert.NotContains(t, formatResultSet(rs, 80), "\x1b")
}

func TestFormatResultSet_WideResultFitsWidth(t *testing.T) {
	var cols []engine.Column
	row := []any{}
	for i := 0; i < 12; i++ {
		cols = append(cols, engine.Column{Name: fmt.Sprintf("column_%d", i), Type: "VARCHAR"})
		row = append(row, "some fairly long value")
	}
	rs := &engine.ResultSet{Columns: cols, Rows: [][]any{row, row}}
	for _, w := range []int{60, 80, 120} {
		out := formatResultSet(rs, w)
		for _, l := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, utf8.RuneCountInString(l), w, "width %d: %q", w, l)
		}
	}
}
