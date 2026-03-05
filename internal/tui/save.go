package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SavePromptModel manages the unsaved-changes confirmation dialog.
type SavePromptModel struct {
	active  bool
	message string
}

// NewSavePromptModel creates a new save prompt.
func NewSavePromptModel() SavePromptModel {
	return SavePromptModel{}
}

// Show activates the save prompt with the given message.
func (s *SavePromptModel) Show(message string) {
	s.active = true
	s.message = message
}

// IsActive returns whether the prompt is currently showing.
func (s SavePromptModel) IsActive() bool {
	return s.active
}

// savePromptResultMsg carries the user's response to the save prompt.
type savePromptResultMsg struct {
	save bool
}

// Update handles key messages for the save prompt.
func (s SavePromptModel) Update(msg tea.KeyMsg) (SavePromptModel, tea.Cmd) {
	if !s.active {
		return s, nil
	}

	switch msg.String() {
	case "y", "Y":
		s.active = false
		return s, func() tea.Msg {
			return savePromptResultMsg{save: true}
		}
	case "n", "N", "esc":
		s.active = false
		return s, func() tea.Msg {
			return savePromptResultMsg{save: false}
		}
	}

	return s, nil
}

// View renders the save prompt overlay.
func (s SavePromptModel) View(width, height int) string {
	if !s.active {
		return ""
	}

	var b strings.Builder

	b.WriteString(contentTitleStyle.Render("Unsaved Changes"))
	b.WriteString("\n\n")
	b.WriteString(s.message)
	b.WriteString("\n\n")
	b.WriteString(helpKeyStyle.Render("y"))
	b.WriteString(helpDescStyle.Render(" Save and continue"))
	b.WriteString("\n")
	b.WriteString(helpKeyStyle.Render("n"))
	b.WriteString(helpDescStyle.Render(" Discard changes"))
	b.WriteString("\n")
	b.WriteString(helpKeyStyle.Render("Esc"))
	b.WriteString(helpDescStyle.Render(" Cancel"))

	return b.String()
}
