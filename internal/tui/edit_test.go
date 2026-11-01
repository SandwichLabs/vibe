package tui

import (
	"os"
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestREPL_CtrlOAsksToEditTheInput(t *testing.T) {
	r := NewREPLModel()
	r.Focus()
	r.input.SetValue("select 1")
	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	require.NotNil(t, cmd)
	assert.Equal(t, replEditMsg{initial: "select 1"}, cmd())
}

func TestREPL_BackslashEEditsTheLastQuery(t *testing.T) {
	r := NewREPLModel()
	r.Focus()
	r.history = []string{"select 1", "select\n  2"}
	r.input.SetValue(`\e`)
	r, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, replEditMsg{initial: "select\n  2"}, cmd())
	assert.Empty(t, r.InputValue())
	assert.Equal(t, 2, r.HistoryLen(), `\e itself is not history`)
}

// scratchWith writes text where the editor would have saved it.
func scratchWith(t *testing.T, text string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "query-*.sql")
	require.NoError(t, err)
	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestFinishEdit_RunsTheEditedQuery(t *testing.T) {
	app, _ := newRealDuckDBApp(t)
	model, cmd := app.Update(editorDoneMsg{path: scratchWith(t, "SELECT 40 + 2 AS answer\n"), before: ""})
	app, ok := model.(*App)
	require.True(t, ok)
	require.NotNil(t, cmd)
	assert.Equal(t, "SELECT 40 + 2 AS answer", app.repl.HistoryAt(0))
	model, _ = app.Update(cmd())
	app, ok = model.(*App)
	require.True(t, ok)
	assert.Contains(t, app.repl.OutputContent(), "(from editor)")
	assert.Contains(t, app.repl.OutputContent(), "42")
}

func TestFinishEdit_UnchangedOrCancelledRunsNothing(t *testing.T) {
	app, _ := newRealDuckDBApp(t)
	_, cmd := app.Update(editorDoneMsg{path: scratchWith(t, "select 1"), before: "select 1"})
	assert.Nil(t, cmd, "an unchanged buffer runs nothing")
	_, cmd = app.Update(editorDoneMsg{path: scratchWith(t, "drop table t"), err: &exec.ExitError{}})
	assert.Nil(t, cmd, "a non-zero editor exit (:cq) runs nothing")
	assert.Equal(t, "edit cancelled", app.status)
	assert.Zero(t, app.repl.HistoryLen())
}

func TestOpenEditor_SuspendsForTheEditor(t *testing.T) {
	root := t.TempDir()
	app := NewApp(nil, nil, nil, WithEditorRoot(root), withEditorArgv("true"))
	_, cmd := app.Update(replEditMsg{initial: "select 1"})
	require.NotNil(t, cmd, "an ExecProcess command hands the terminal to the editor")
	entries, err := os.ReadDir(root + "/.assay-local/edit")
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the scratch buffer lives under the workspace")
}
