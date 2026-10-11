package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// replSubmitMsg is sent when the user presses Enter in the REPL with input.
type replSubmitMsg struct {
	query string
}

// replResultMsg carries the result of a REPL query execution back to the REPL.
type replResultMsg struct {
	output string
	// wrote reports whether the statement that produced this result was a
	// write, not a plain read — computed in executeREPLQuery (app.go) via
	// agents.IsReadOnlyStatement, the real DuckDB-parser-backed classifier
	// (not a keyword-prefix guess). Drives App.unsaved.
	wrote bool
}

// REPLModel manages the SQL REPL in the bottom panel.
type REPLModel struct {
	input         textinput.Model
	output        viewport.Model
	history       []string
	histIdx       int
	outputContent string // accumulated output for viewport
	width         int
	height        int
}

// NewREPLModel creates a new REPL model.
func NewREPLModel() REPLModel {
	ti := textinput.New()
	ti.Prompt = "sql> "
	ti.PromptStyle = replPromptStyle
	ti.CharLimit = 4096
	ti.Width = 60

	vp := viewport.New(80, 4)

	return REPLModel{
		input:   ti,
		output:  vp,
		history: make([]string, 0),
		histIdx: -1,
		width:   80,
		height:  6,
	}
}

// Focus gives focus to the REPL text input.
func (r *REPLModel) Focus() tea.Cmd {
	return r.input.Focus()
}

// Blur removes focus from the REPL text input.
func (r *REPLModel) Blur() {
	r.input.Blur()
}

// Focused returns whether the text input is focused.
func (r *REPLModel) Focused() bool {
	return r.input.Focused()
}

// SetSize updates the REPL dimensions.
func (r *REPLModel) SetSize(width, height int) {
	r.width = width
	r.height = height
	r.input.Width = width - lipgloss.Width(r.input.Prompt) - 2
	// Output viewport gets height minus the input line.
	vpHeight := height - 1
	if vpHeight < 1 {
		vpHeight = 1
	}
	r.output.Width = width
	r.output.Height = vpHeight
}

// AppendOutput adds output text and scrolls to the bottom.
// replOutputBufferCap bounds the REPL's accumulated output history. A long
// session running many queries would otherwise grow r.outputContent
// unboundedly; once it's over the cap, AppendOutput drops the OLDEST
// content (at a line boundary) rather than the newest — the result the
// user just ran is always visible.
const replOutputBufferCap = 512 * 1024 // 512KB of scrollback text

func (r *REPLModel) AppendOutput(text string) {
	text = sanitizeForTerminal(text, true)
	if r.outputContent != "" {
		r.outputContent += "\n"
	}
	r.outputContent += text
	if len(r.outputContent) > replOutputBufferCap {
		cut := len(r.outputContent) - replOutputBufferCap
		if nl := strings.IndexByte(r.outputContent[cut:], '\n'); nl >= 0 {
			cut += nl + 1
		}
		r.outputContent = "… (earlier output truncated) …\n" + r.outputContent[cut:]
	}
	r.output.SetContent(replOutputStyle.Render(r.outputContent))
	r.output.GotoBottom()
}

// Update handles messages for the REPL.
func (r REPLModel) Update(msg tea.Msg) (REPLModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			val := r.input.Value()
			if val != "" {
				r.history = append(r.history, val)
				r.input.SetValue("")
				r.histIdx = -1
				return r, func() tea.Msg {
					return replSubmitMsg{query: val}
				}
			}
			return r, nil
		case tea.KeyUp:
			if len(r.history) > 0 {
				if r.histIdx < 0 {
					r.histIdx = len(r.history) - 1
				} else if r.histIdx > 0 {
					r.histIdx--
				}
				r.input.SetValue(r.history[r.histIdx])
				r.input.CursorEnd()
			}
			return r, nil
		case tea.KeyDown:
			if r.histIdx >= 0 {
				r.histIdx++
				if r.histIdx >= len(r.history) {
					r.histIdx = -1
					r.input.SetValue("")
				} else {
					r.input.SetValue(r.history[r.histIdx])
					r.input.CursorEnd()
				}
			}
			return r, nil
		}
	}

	var cmd tea.Cmd
	r.input, cmd = r.input.Update(msg)
	return r, cmd
}

// View renders the REPL within the given dimensions.
func (r REPLModel) View(width, height int) string {
	var parts []string

	if r.outputContent != "" {
		parts = append(parts, r.output.View())
	}

	parts = append(parts, r.input.View())

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// InputValue returns the current text input value (for testing).
func (r REPLModel) InputValue() string {
	return r.input.Value()
}

// HistoryLen returns the number of history entries (for testing).
func (r REPLModel) HistoryLen() int {
	return len(r.history)
}

// HistoryAt returns the history entry at index i (for testing).
func (r REPLModel) HistoryAt(i int) string {
	if i < 0 || i >= len(r.history) {
		return ""
	}
	return r.history[i]
}

// OutputContent returns the accumulated output (for testing).
func (r REPLModel) OutputContent() string {
	return r.outputContent
}

// replRowCap bounds how many rows formatResultSet ever renders. executeREPLQuery
// (app.go) also pushes a LIMIT down into the SQL itself when the statement
// shape makes that safe (a plain SELECT/WITH…SELECT with no LIMIT of its
// own), so a huge table is never even fully fetched into memory; this is the
// backstop that also covers the statement shapes that can't be safely
// wrapped (SHOW/DESCRIBE/PRAGMA/multi-statement) and any write statement
// that happens to return rows.
const replRowCap = 1000

// formatResultSet formats a query ResultSet as plain, padded text — the TUI
// is a terminal, so table results are a fixed-width text grid rather than
// any styled/bordered rendering (lipgloss borders + Width() interact badly
// with variable-width cell content). Every cell value is run through
// formatCellValue, which renders NULL/NaN/Inf as visible text and strips
// control/escape characters so a value can't corrupt the terminal.
func formatResultSet(rs *engine.ResultSet) string {
	if rs == nil || len(rs.Columns) == 0 {
		return "-- OK (no results)"
	}

	totalRows := len(rs.Rows)
	rows := rs.Rows
	truncated := totalRows > replRowCap
	if truncated {
		rows = rows[:replRowCap]
	}

	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = len(sanitizeForTerminal(c.Name, false))
	}
	rowsText := make([][]string, len(rows))
	for ri, row := range rows {
		vals := make([]string, len(rs.Columns))
		for i := range rs.Columns {
			v := ""
			if i < len(row) {
				v = formatCellValue(row[i])
			}
			vals[i] = v
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
		rowsText[ri] = vals
	}

	var b strings.Builder
	writeRow := func(vals []string) {
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = padRight(v, widths[i])
		}
		b.WriteString(strings.Join(parts, "  "))
		b.WriteString("\n")
	}

	colNames := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		colNames[i] = sanitizeForTerminal(c.Name, false)
	}
	writeRow(colNames)

	sepParts := make([]string, len(widths))
	for i, w := range widths {
		sepParts[i] = strings.Repeat("-", w)
	}
	b.WriteString(strings.Join(sepParts, "  "))
	b.WriteString("\n")

	for _, vals := range rowsText {
		writeRow(vals)
	}

	if truncated {
		// "at least" because when executeREPLQuery already pushed a LIMIT
		// down into the SQL, rs.Rows itself was already capped at the
		// database — totalRows-replRowCap is a floor on how much more
		// exists, not necessarily the exact remainder.
		b.WriteString(fmt.Sprintf("… at least %d more row(s) not shown (capped at %d)\n", totalRows-replRowCap, replRowCap))
	}
	b.WriteString(fmt.Sprintf("(%d rows)", totalRows))
	return b.String()
}

// formatCellValue renders one result cell as display text: NULL for a Go
// nil (a SQL NULL), the literal text for a NaN/±Inf float (fmt's default
// %v already does this — spelled out here so it's a documented contract,
// not an accident of formatting), and every other value through
// sanitizeForTerminal so an embedded control/escape character in a string
// column can't corrupt the terminal.
func formatCellValue(v any) string {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) {
			return "NaN"
		}
		if math.IsInf(n, 1) {
			return "+Inf"
		}
		if math.IsInf(n, -1) {
			return "-Inf"
		}
	case float32:
		return formatCellValue(float64(n))
	}
	return sanitizeForTerminal(engine.CellText(v, "NULL"), false)
}

// padRight pads s with spaces to width n (no-op if s is already >= n runes).
func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
