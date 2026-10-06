package main

// P6.7 — Studio Home template picker bindings.
//
// These bridge the bundled-template registry (internal/container, P6.6) to the
// Studio Home/Dashboard quick-start template cards (mockup #2): ListTemplates
// surfaces the registered starter templates (name + human-readable description)
// and CreateFromTemplate creates a fresh encrypted .vibe seeded from one of them.

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/queries"
)

// TemplateInfo is the quick-start template descriptor surfaced to the Studio
// Home/Dashboard cards (name + human-readable description).
type TemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListTemplates returns the bundled starter templates available as quick-start
// cards on the Studio Home/Dashboard.
func (a *App) ListTemplates() ([]TemplateInfo, error) {
	names := container.ListTemplates()
	templates := make([]TemplateInfo, 0, len(names))
	for _, name := range names {
		desc, _ := container.TemplateDescription(name)
		templates = append(templates, TemplateInfo{Name: name, Description: desc})
	}
	return templates, nil
}

// CreateFromTemplate creates a new encrypted .vibe seeded from a bundled
// template and opens it as the active file. An unknown template name yields an
// actionable error listing the available names.
func (a *App) CreateFromTemplate(path, passphrase, template string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := container.TemplateDescription(template); !ok {
		return fmt.Errorf("unknown template %q; available templates: %v", template, container.ListTemplates())
	}

	a.closeCurrentContainerLocked()

	c, err := container.Create(path,
		container.WithPassphrase(passphrase),
		container.WithLogger(a.logger),
		container.WithTemplate(template),
		container.WithLockApp("Vibe Studio"),
	)
	if err != nil {
		return fmt.Errorf("failed to create file from template %q: %w", template, err)
	}

	a.container = c
	a.engine = engine.NewManager(c.Session(), engine.WithLogger(a.logger))
	a.registry = queries.NewRegistry(c.SQLiteDB(), a.logger)
	a.filePath = path
	a.beginFileSession()
	notebooks.MigrateOnOpen(a.container.SQLiteDB(), a.container.IsReadOnly(), a.logger)
	// Mirror the OpenFile/CreateFile lifecycle: announce the open. emitEvent is
	// a no-op when no emit sink is wired (unit tests); production installs the
	// Wails runtime emitter in startup.
	a.emitEvent("file:opened")
	return nil
}
