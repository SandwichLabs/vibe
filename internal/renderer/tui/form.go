package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

var (
	formLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")).
			Width(20)

	formInputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	formRequiredStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))
)

// renderForm renders form fields as a display layout in the terminal.
func renderForm(cfg *renderer.FormConfig, maxWidth int) string {
	var b strings.Builder

	inputWidth := maxWidth - 24 // label width + padding
	if inputWidth < 15 {
		inputWidth = 15
	}

	for _, field := range cfg.Fields {
		label := field.Label
		if label == "" {
			label = field.Name
		}

		required := ""
		if field.Required {
			required = formRequiredStyle.Render(" *")
		}

		labelRendered := formLabelStyle.Render(label + required)

		var inputDisplay string
		switch field.Type {
		case "select":
			if len(field.Options) > 0 {
				opts := make([]string, 0, len(field.Options))
				for _, opt := range field.Options {
					opts = append(opts, opt.Label)
				}
				inputDisplay = fmt.Sprintf("[%s]", strings.Join(opts, " | "))
			} else {
				inputDisplay = "[select...]"
			}
		case "checkbox":
			inputDisplay = "[ ]"
		case "textarea":
			inputDisplay = "[multi-line text]"
		default:
			placeholder := fmt.Sprintf("[%s]", field.Type)
			if field.Default != nil {
				placeholder = fmt.Sprintf("%v", field.Default)
			}
			inputDisplay = placeholder
		}

		input := formInputStyle.Width(inputWidth).Render(inputDisplay)

		b.WriteString(labelRendered)
		b.WriteString(input)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	submitStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("39")).
		Padding(0, 2)
	b.WriteString(submitStyle.Render(" Submit "))

	return b.String()
}
