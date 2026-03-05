package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// editorFinishedMsg is sent when the external editor closes.
type editorFinishedMsg struct {
	pageID  string
	content string
	err     error
}

// openEditorCmd creates a tea.Cmd that suspends the TUI, opens $EDITOR
// with the given content in a temp file, and sends back the edited result.
func openEditorCmd(pageID, content string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "vibe-edit-*.md")
	if err != nil {
		return func() tea.Msg {
			return editorFinishedMsg{pageID: pageID, err: fmt.Errorf("failed to create temp file: %w", err)}
		}
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return func() tea.Msg {
			return editorFinishedMsg{pageID: pageID, err: fmt.Errorf("failed to write temp file: %w", err)}
		}
	}
	tmpFile.Close()

	c := exec.Command(editor, tmpPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		if err != nil {
			return editorFinishedMsg{pageID: pageID, err: fmt.Errorf("editor exited with error: %w", err)}
		}
		data, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return editorFinishedMsg{pageID: pageID, err: fmt.Errorf("failed to read edited file: %w", readErr)}
		}
		return editorFinishedMsg{pageID: pageID, content: string(data)}
	})
}
