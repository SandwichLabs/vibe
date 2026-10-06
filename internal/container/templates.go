package container

// Bundled starter templates live in internal/templates, shared with the
// workspace CLI (`vibe workspace init --template`). This file keeps the legacy
// container's seed path on top of them until the container is removed (#111).

import (
	"context"
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/templates"
)

// ListTemplates returns the names of all registered starter templates that can
// be passed to WithTemplate.
func ListTemplates() []string { return templates.List() }

// TemplateDescription returns a human-readable description for a registered
// starter template (used by the Studio Home quick-start cards). The boolean
// reports whether the name is registered.
func TemplateDescription(name string) (string, bool) { return templates.Description(name) }

// applyTemplate seeds the named starter template into a freshly created
// container: DuckDB user tables + sample data, and a starter notebook in
// SQLCipher. It returns a wrapped error if the template name is unknown.
func applyTemplate(c *Container, name string) error {
	tmpl, ok := templates.Get(name)
	if !ok {
		return fmt.Errorf("unknown template %q: not a registered template", name)
	}

	c.logger.Info("seeding starter template", "template", name)

	ctx := context.Background()
	err := templates.SeedTables(ctx, func(ctx context.Context, q string) error {
		_, err := c.duckDB.db.ExecContext(ctx, q)
		return err
	}, tmpl)
	if err != nil {
		return fmt.Errorf("failed to seed template %q tables: %w", name, err)
	}
	if _, _, err := c.Notebooks().Insert(ctx, tmpl.Book); err != nil {
		return fmt.Errorf("failed to seed template %q notebook: %w", name, err)
	}
	return nil
}
