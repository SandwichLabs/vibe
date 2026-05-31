package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewApp(t *testing.T) {
	app := NewApp(nil, nil, nil)
	require.NotNil(t, app)
	assert.Equal(t, focusSidebar, app.focus)
	assert.False(t, app.quitting)
	assert.False(t, app.showHelp)
	assert.False(t, app.unsaved)
	// Verify bubbles components initialized.
	assert.NotEmpty(t, app.keys.Quit.Keys())
	assert.NotNil(t, app.helpView)
}

func TestApp_Init(t *testing.T) {
	app := NewApp(nil, nil, nil)
	cmd := app.Init()
	assert.Nil(t, cmd)
}

func TestApp_CycleFocus(t *testing.T) {
	app := NewApp(nil, nil, nil)
	assert.Equal(t, focusSidebar, app.focus)

	app.setFocus(focusContent)
	assert.Equal(t, focusContent, app.focus)

	app.setFocus(focusREPL)
	assert.Equal(t, focusREPL, app.focus)
	assert.True(t, app.repl.Focused())

	app.setFocus(focusSidebar)
	assert.Equal(t, focusSidebar, app.focus)
	assert.False(t, app.repl.Focused())
}

func TestApp_ToggleREPL(t *testing.T) {
	app := NewApp(nil, nil, nil)
	assert.Equal(t, focusSidebar, app.focus)

	app.toggleREPL()
	assert.Equal(t, focusREPL, app.focus)

	app.toggleREPL()
	assert.Equal(t, focusContent, app.focus)
}

func TestApp_HelpToggle(t *testing.T) {
	app := NewApp(nil, nil, nil)
	assert.False(t, app.showHelp)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app, _ = model.(*App)
	assert.True(t, app.showHelp)

	// Any key should dismiss help.
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

func TestApp_TableDataMsg(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.tableBrowser.SetLoading("test_table", 5)

	model, _ := app.Update(tableDataMsg{
		tableName: "test_table",
		columns:   nil,
		rows:      nil,
		err:       nil,
	})
	app, _ = model.(*App)
	assert.False(t, app.tableBrowser.loading)
}

func TestApp_NewKeyOpensWizard(t *testing.T) {
	app := NewApp(nil, nil, nil)
	assert.False(t, app.pageWizard.IsActive())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	app, _ = model.(*App)
	assert.True(t, app.pageWizard.IsActive())

	// View should show wizard overlay.
	app.width = 80
	app.height = 24
	view := app.View()
	assert.Contains(t, view, "New Page Wizard")
}

func TestApp_WizardEscCancels(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.pageWizard.Start()
	assert.True(t, app.pageWizard.IsActive())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEscape})
	app, _ = model.(*App)
	assert.False(t, app.pageWizard.IsActive())
}

func TestApp_WelcomeContent(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.width = 100
	app.height = 40
	app.propagateDimensions()
	view := app.View()
	assert.Contains(t, view, "Welcome")
}

func TestApp_EditorFinishedMsg(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.width = 100
	app.height = 40

	// Simulate editor returning content.
	model, _ := app.Update(editorFinishedMsg{
		pageID:  "test-page-1",
		content: "# Edited Content\n\nHello world.",
	})
	app, _ = model.(*App)
	assert.Equal(t, viewPage, app.activeView)
	assert.Equal(t, focusContent, app.focus)
}

func TestApp_EditorFinishedMsgError(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// Error should not crash.
	model, _ := app.Update(editorFinishedMsg{
		pageID: "test-page-1",
		err:    fmt.Errorf("editor crashed"),
	})
	app, _ = model.(*App)
	// Should still be on whatever view was before, not crash.
	assert.NotNil(t, app)
}

func TestApp_SetFocusBlursREPL(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.setFocus(focusREPL)
	assert.True(t, app.repl.Focused())

	app.setFocus(focusSidebar)
	assert.False(t, app.repl.Focused())
}
