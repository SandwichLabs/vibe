package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewApp(t *testing.T) {
	app := NewApp(nil, nil, nil)
	require.NotNil(t, app)
	assert.Equal(t, modeREPL, app.mode)
	assert.False(t, app.quitting)
	assert.False(t, app.showHelp)
	assert.NotEmpty(t, app.keys.Quit.Keys())
	assert.NotNil(t, app.helpView)
}

func TestApp_Init(t *testing.T) {
	app := NewApp(nil, nil, nil)
	cmd := app.Init()
	assert.Nil(t, cmd)
}

func TestApp_SwitchModeByNumber(t *testing.T) {
	app := NewApp(nil, nil, nil)
	assert.Equal(t, modeREPL, app.mode)

	// The REPL owns single-key text input while focused, so numeric keys
	// only switch modes from a non-text-capturing view.
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app, _ = model.(*App)
	assert.Equal(t, modeREPL, app.mode, "REPL owns keystrokes; '2' types into the query input instead of switching")

	app.setMode(modeTables)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	app, _ = model.(*App)
	assert.Equal(t, modeNotebooks, app.mode)

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	app, _ = model.(*App)
	assert.Equal(t, modeREPL, app.mode)
}

func TestApp_CycleMode(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setMode(modeTables) // leave the REPL so tab isn't captured as text

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app, _ = model.(*App)
	assert.Equal(t, modeNotebooks, app.mode)

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app, _ = model.(*App)
	assert.Equal(t, modeREPL, app.mode)
}

func TestApp_HelpToggle(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setMode(modeTables)
	assert.False(t, app.showHelp)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app, _ = model.(*App)
	assert.True(t, app.showHelp)

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	app, _ = model.(*App)
	assert.False(t, app.showHelp)
}

func TestApp_WindowSizeMsg(t *testing.T) {
	app := NewApp(nil, nil, nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app, _ = model.(*App)
	assert.Equal(t, 120, app.width)
	assert.Equal(t, 40, app.height)
}

func TestApp_ViewQuit(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.quitting = true
	view := app.View()
	assert.Contains(t, view, "Goodbye!")
}

func TestApp_ViewHelp(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.showHelp = true
	app.width = 80
	app.height = 24
	view := app.View()
	assert.Contains(t, view, "Key Bindings")
}

func TestApp_QuitFromNonREPLView(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setMode(modeTables)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	app, _ = model.(*App)
	assert.True(t, app.quitting)
	assert.NotNil(t, cmd)
}

func TestApp_QOnREPLTypesNotQuits(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setMode(modeREPL)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	app, _ = model.(*App)
	assert.False(t, app.quitting, "'q' must type into the REPL input, not quit")
	assert.Equal(t, "q", app.repl.InputValue())
}

// TestNewApp_REPLIsFocusedAtStartup is a regression guard: NewApp starts in
// modeREPL by setting the field directly rather than going through setMode
// (which is the only other place that calls repl.Focus()). bubbles/textinput
// silently drops every keystroke while unfocused, so a fresh app that
// forgets to focus the REPL looks fine but is un-typeable until the user
// tabs away and back — caught live via a pty-driven TUI smoke test.
func TestNewApp_REPLIsFocusedAtStartup(t *testing.T) {
	app := NewApp(nil, nil, nil)
	assert.True(t, app.repl.Focused())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	app, _ = model.(*App)
	assert.Equal(t, "x", app.repl.InputValue(), "a fresh app's REPL must accept keystrokes immediately")
}

func TestApp_ViewShowsModeBar(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.width = 80
	app.height = 24
	view := app.View()
	assert.Contains(t, view, "REPL")
	assert.Contains(t, view, "Tables")
	assert.Contains(t, view, "Notebooks")
}
