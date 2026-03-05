package tui

import (
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
func (r *REPLModel) AppendOutput(text string) {
	if r.outputContent != "" {
		r.outputContent += "\n"
	}
	r.outputContent += text
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
