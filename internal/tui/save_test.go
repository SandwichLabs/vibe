package tui

import (
	"context"
	"database/sql"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingContainer is a minimal ContainerAccess mock that counts Save()
// calls, for asserting the TUI's write-persistence behavior (ctrl+s,
// save-on-quit) without a real .vibe file.
type countingContainer struct {
	saveCalls int
	saveErr   error
}

func (c *countingContainer) SQLiteDB() *sql.DB { return nil }
func (c *countingContainer) DuckDBDB() *sql.DB { return nil }
func (c *countingContainer) Save() error {
	c.saveCalls++
	return c.saveErr
}
func (c *countingContainer) Close() error { return nil }

// stubEngine returns a canned result for every query — the TUI tests below
// only care about wrote-tracking (driven directly via replResultMsg.wrote,
// not real execution/classification — see unsaved_test.go for tests that
// exercise the real agents.IsReadOnlyStatement classifier end to end).
type stubEngine struct{}

func (stubEngine) Execute(_ context.Context, _ string, _ map[string]any) (*engine.ResultSet, error) {
	return &engine.ResultSet{}, nil
}

func TestApp_REPLWriteMarksUnsaved(t *testing.T) {
	app := NewApp(nil, stubEngine{}, nil)
	assert.False(t, app.unsaved)

	model, cmd := app.Update(replResultMsg{output: "ok", wrote: true})
	app, _ = model.(*App)
	assert.Nil(t, cmd)
	assert.True(t, app.unsaved, "a write result must mark the session unsaved")
}

func TestApp_CtrlSSavesFromREPL(t *testing.T) {
	c := &countingContainer{}
	app := NewApp(c, stubEngine{}, nil)
	app.setMode(modeREPL)
	app.unsaved = true

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	app, _ = model.(*App)
	require.NotNil(t, cmd, "ctrl+s must save even while the REPL owns the keyboard")

	msg := cmd()
	model, cmd2 := app.Update(msg)
	app, _ = model.(*App)
	assert.Equal(t, 1, c.saveCalls)
	assert.False(t, app.unsaved)
	assert.False(t, app.quitting)
	assert.Nil(t, cmd2)
}

func TestApp_QuitWithUnsavedWritesAutoSaves(t *testing.T) {
	c := &countingContainer{}
	app := NewApp(c, stubEngine{}, nil)
	app.unsaved = true

	cmd := app.quit()
	require.NotNil(t, cmd)
	msg := cmd()
	saveMsg, ok := msg.(saveResultMsg)
	require.True(t, ok)
	assert.True(t, saveMsg.thenQuit)

	model, quitCmd := app.Update(saveMsg)
	app, _ = model.(*App)
	assert.Equal(t, 1, c.saveCalls, "quitting with unsaved writes must save before exiting")
	assert.True(t, app.quitting)
	require.NotNil(t, quitCmd)
	assert.IsType(t, tea.QuitMsg{}, quitCmd())
}

func TestApp_QuitWithoutUnsavedWritesSkipsSave(t *testing.T) {
	c := &countingContainer{}
	app := NewApp(c, stubEngine{}, nil)
	app.unsaved = false

	cmd := app.quit()
	require.NotNil(t, cmd)
	assert.True(t, app.quitting)
	assert.IsType(t, tea.QuitMsg{}, cmd())
	assert.Equal(t, 0, c.saveCalls, "a clean quit with nothing unsaved must not save")
}

func TestApp_QuestionMarkInREPLTypesNotHelp(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setMode(modeREPL)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app, _ = model.(*App)
	assert.False(t, app.showHelp, "'?' must type into the REPL input, not open help")
	assert.Equal(t, "?", app.repl.InputValue())
}

func TestApp_QuestionMarkOutsideREPLOpensHelp(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setMode(modeTables)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app, _ = model.(*App)
	assert.True(t, app.showHelp)
}

func TestApp_ModeBarHidesNumbersWhileREPLCapturesInput(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.width = 80
	app.setMode(modeREPL)
	bar := app.renderModeBar()
	assert.NotContains(t, bar, "1:REPL")
	assert.NotContains(t, bar, "2:Tables")
	assert.Contains(t, bar, "REPL")
	assert.Contains(t, bar, "Tables")

	app.setMode(modeTables)
	bar = app.renderModeBar()
	assert.Contains(t, bar, "1:REPL")
	assert.Contains(t, bar, "2:Tables")
}

func TestApp_StatusBarShowsUnsavedIndicator(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.width = 80
	app.height = 24
	app.unsaved = true
	assert.Contains(t, app.View(), "[unsaved]")

	app.unsaved = false
	assert.NotContains(t, app.View(), "[unsaved]")
}
