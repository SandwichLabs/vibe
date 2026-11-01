package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/SandwichLabs/assay/internal/editor"
	tea "github.com/charmbracelet/bubbletea"
)

// replEditMsg asks the app to open initial in the person's editor (ctrl+o, or
// `\e` typed at the prompt as in psql).
type replEditMsg struct {
	initial string
}

// editorDoneMsg reports that the editor exited.
type editorDoneMsg struct {
	path   string
	before string
	err    error
}

// editCommand is the REPL's psql-style meta-command for "edit in $EDITOR".
const editCommand = `\e`

// WithEditorRoot sets the workspace root for editor scratch buffers, so they
// sit under the workspace (.assay-local/edit/) where an editor's language
// server finds assay.yaml and starts `assay lsp`.
func WithEditorRoot(root string) Option { return func(a *App) { a.editRoot = root } }

// openEditor suspends the TUI and runs the editor on a scratch .sql file
// holding initial. When the editor exits the TUI resumes and editorDoneMsg
// carries the result.
func (a *App) openEditor(initial string) tea.Cmd {
	resolve := a.editorArgv
	if resolve == nil {
		resolve = func() ([]string, error) { return editor.Resolve(os.Getenv) }
	}
	argv, err := resolve()
	if err == nil {
		err = editor.Check(argv)
	}
	if err != nil {
		a.status = err.Error()
		return nil
	}
	path, err := editor.NewScratch(a.editRoot, initial)
	if err != nil {
		a.status = err.Error()
		return nil
	}
	cmd := editor.Command(argv, path, a.editRoot)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{path: path, before: initial, err: err}
	})
}

// finishEdit runs the edited query, as psql's \e does, when the buffer changed
// and is not empty. An unchanged buffer or a cancelled editor (":cq") leaves
// the prompt as it was.
func (a *App) finishEdit(msg editorDoneMsg) tea.Cmd {
	text, ok, err := editor.Finish(msg.path, msg.err)
	if err != nil {
		a.status = err.Error()
		return nil
	}
	if !ok || strings.TrimSpace(text) == "" || text == msg.before {
		a.status = "edit cancelled"
		return nil
	}
	query := strings.TrimSpace(text)
	a.repl.recordEdited(query)
	a.repl.AppendOutput(fmt.Sprintf("%s(from editor)\n%s", a.repl.input.Prompt, query))
	return a.executeREPLQuery(query)
}
