package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type wizardStep int

const (
	wizardStepType wizardStep = iota
	wizardStepTitle
	wizardStepTemplate
	wizardStepDone
)

var pageTypes = []string{"page", "dashboard", "form", "report"}

var pageTemplates = map[string][]string{
	"page":      {"blank", "markdown"},
	"dashboard": {"blank", "metrics", "charts"},
	"form":      {"blank", "crud"},
	"report":    {"blank", "summary"},
}

// PageWizardModel walks the user through creating a new page.
type PageWizardModel struct {
	step       wizardStep
	pageType   string
	title      string
	template   string
	typeCursor int
	tmplCursor int
	titleInput string
	active     bool
}

// NewPageWizardModel creates a new page wizard.
func NewPageWizardModel() PageWizardModel {
	return PageWizardModel{
		step: wizardStepType,
	}
}

// Start activates the wizard.
func (w *PageWizardModel) Start() {
	w.active = true
	w.step = wizardStepType
	w.pageType = ""
	w.title = ""
	w.template = ""
	w.typeCursor = 0
	w.tmplCursor = 0
	w.titleInput = ""
}

// IsActive returns whether the wizard is currently active.
func (w PageWizardModel) IsActive() bool {
	return w.active
}

// Update handles key messages for the wizard.
func (w PageWizardModel) Update(msg tea.KeyMsg) PageWizardModel {
	if !w.active {
		return w
	}

	switch msg.String() {
	case "esc":
		w.active = false
		return w
	}

	switch w.step {
	case wizardStepType:
		w = w.updateTypeStep(msg)
	case wizardStepTitle:
		w = w.updateTitleStep(msg)
	case wizardStepTemplate:
		w = w.updateTemplateStep(msg)
	}

	return w
}

func (w PageWizardModel) updateTypeStep(msg tea.KeyMsg) PageWizardModel {
	switch msg.String() {
	case "j", "down":
		w.typeCursor++
		if w.typeCursor >= len(pageTypes) {
			w.typeCursor = len(pageTypes) - 1
		}
	case "k", "up":
		w.typeCursor--
		if w.typeCursor < 0 {
			w.typeCursor = 0
		}
	case "enter":
		w.pageType = pageTypes[w.typeCursor]
		w.step = wizardStepTitle
	}
	return w
}

func (w PageWizardModel) updateTitleStep(msg tea.KeyMsg) PageWizardModel {
	switch msg.String() {
	case "enter":
		if w.titleInput != "" {
			w.title = w.titleInput
			w.step = wizardStepTemplate
			w.tmplCursor = 0
		}
	case "backspace":
		if len(w.titleInput) > 0 {
			w.titleInput = w.titleInput[:len(w.titleInput)-1]
		}
	default:
		if len(msg.String()) == 1 {
			w.titleInput += msg.String()
		}
	}
	return w
}

func (w PageWizardModel) updateTemplateStep(msg tea.KeyMsg) PageWizardModel {
	templates := pageTemplates[w.pageType]
	switch msg.String() {
	case "j", "down":
		w.tmplCursor++
		if w.tmplCursor >= len(templates) {
			w.tmplCursor = len(templates) - 1
		}
	case "k", "up":
		w.tmplCursor--
		if w.tmplCursor < 0 {
			w.tmplCursor = 0
		}
	case "enter":
		w.template = templates[w.tmplCursor]
		w.step = wizardStepDone
		w.active = false
	}
	return w
}

// Result returns the wizard result. Only valid after step == wizardStepDone.
func (w PageWizardModel) Result() (pageType, title, template string) {
	return w.pageType, w.title, w.template
}

// View renders the wizard overlay.
func (w PageWizardModel) View(width, height int) string {
	if !w.active {
		return ""
	}

	var b strings.Builder

	title := contentTitleStyle.Render("New Page Wizard")
	b.WriteString(title)
	b.WriteString("\n\n")

	switch w.step {
	case wizardStepType:
		b.WriteString(sectionHeaderStyle.Render("Select page type:"))
		b.WriteString("\n")
		for i, pt := range pageTypes {
			if i == w.typeCursor {
				b.WriteString(selectedItemStyle.Render(fmt.Sprintf("> %s", pt)))
			} else {
				b.WriteString(listItemStyle.Render(pt))
			}
			b.WriteString("\n")
		}

	case wizardStepTitle:
		b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("Page type: %s", w.pageType)))
		b.WriteString("\n\n")
		b.WriteString("Enter page title: ")
		b.WriteString(w.titleInput)
		b.WriteString("_")

	case wizardStepTemplate:
		b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("Page: %s (%s)", w.title, w.pageType)))
		b.WriteString("\n\n")
		b.WriteString(sectionHeaderStyle.Render("Select template:"))
		b.WriteString("\n")
		templates := pageTemplates[w.pageType]
		for i, tmpl := range templates {
			if i == w.tmplCursor {
				b.WriteString(selectedItemStyle.Render(fmt.Sprintf("> %s", tmpl)))
			} else {
				b.WriteString(listItemStyle.Render(tmpl))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Esc to cancel"))

	return b.String()
}
