package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestREPLModel_Input(t *testing.T) {
	r := NewREPLModel()
	r.input.Focus()
	assert.Equal(t, "", r.InputValue())

	// Type characters via textinput.
	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	assert.Equal(t, "S", r.InputValue())

	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	assert.Equal(t, "SQ", r.InputValue())

	// Backspace.
	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "S", r.InputValue())
}

func TestREPLModel_Enter(t *testing.T) {
	r := NewREPLModel()
	r.input.Focus()
	r.input.SetValue("SELECT 1")

	r, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "", r.InputValue())
	assert.Equal(t, 1, r.HistoryLen())
	assert.Equal(t, "SELECT 1", r.HistoryAt(0))
	assert.NotNil(t, cmd)
}

func TestREPLModel_History(t *testing.T) {
	r := NewREPLModel()
	r.input.Focus()
	r.history = []string{"SELECT 1", "SELECT 2"}

	// Navigate up through history.
	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, "SELECT 2", r.InputValue())

	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, "SELECT 1", r.InputValue())

	// Navigate back down.
	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "SELECT 2", r.InputValue())

	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "", r.InputValue())
}

func TestREPLModel_View(t *testing.T) {
	r := NewREPLModel()
	view := r.View(80, 6)
	assert.Contains(t, view, "sql>")
}

func TestREPLModel_AppendOutput(t *testing.T) {
	r := NewREPLModel()
	r.AppendOutput("result 1")
	assert.Equal(t, "result 1", r.OutputContent())

	r.AppendOutput("result 2")
	assert.Contains(t, r.OutputContent(), "result 1")
	assert.Contains(t, r.OutputContent(), "result 2")
}

func TestREPLModel_FocusBlur(t *testing.T) {
	r := NewREPLModel()
	assert.False(t, r.Focused())

	r.Focus()
	assert.True(t, r.Focused())

	r.Blur()
	assert.False(t, r.Focused())
}
