package main

// Starter-template bindings for the New workspace dialog. The registry lives in
// internal/templates; CreateWorkspace seeds the chosen one.

import (
	"github.com/SandwichLabs/vibe/internal/templates"
)

// TemplateInfo is a bundled starter template (name + one plain sentence).
type TemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListTemplates returns the bundled starter templates for the New workspace
// dialog's "Start from" cards.
func (a *App) ListTemplates() ([]TemplateInfo, error) {
	names := templates.List()
	out := make([]TemplateInfo, 0, len(names))
	for _, name := range names {
		desc, _ := templates.Description(name)
		out = append(out, TemplateInfo{Name: name, Description: desc})
	}
	return out, nil
}
