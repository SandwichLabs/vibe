package html

// Bound-control renderer (plan §1.4).
//
// This file hosts the single control implementation shared by both the {form}
// renderer and the datasheet inline-edit cell: combo (display vs hidden value),
// lookup-from-query, checkbox, date/datetime picker, textarea, with ControlTip
// surfaced as title/help text. Combo/lookup options are resolved server-side by
// executing lookup_sql via the engine, so the raw SQL never reaches the client.

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/sandwich-labs/vibe-app/internal/schema"
)

// OptionResolver resolves combo/lookup options server-side from a stored
// lookup SQL statement so the raw SQL never reaches the client.
type OptionResolver interface {
	ResolveOptions(ctx context.Context, lookupSQL, valueCol, displayCol string) ([]renderer.SelectOption, error)
}

// engineOptionResolver resolves options by executing lookup_sql on the engine.
type engineOptionResolver struct {
	eng *engine.Manager
}

// NewEngineOptionResolver builds an OptionResolver backed by the DuckDB engine.
func NewEngineOptionResolver(eng *engine.Manager) OptionResolver {
	return &engineOptionResolver{eng: eng}
}

// ResolveOptions executes lookup_sql via the engine and maps each row's
// value_col/display_col into a SelectOption. The lookup SQL is run server-side;
// callers receive only the resolved {value,label} pairs.
func (r *engineOptionResolver) ResolveOptions(ctx context.Context, lookupSQL, valueCol, displayCol string) ([]renderer.SelectOption, error) {
	if r.eng == nil {
		return nil, fmt.Errorf("option resolver: engine is not available")
	}
	if strings.TrimSpace(lookupSQL) == "" {
		return nil, nil
	}

	rs, err := r.eng.Execute(ctx, lookupSQL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve lookup options: %w", err)
	}

	valIdx := rs.ColumnIndex(valueCol)
	dispIdx := rs.ColumnIndex(displayCol)
	// Fall back to the first two columns (Access lookup convention: hidden bound
	// column first, display column second) when names are not specified/matched.
	if valIdx < 0 {
		valIdx = 0
	}
	if dispIdx < 0 {
		if len(rs.Columns) > 1 {
			dispIdx = 1
		} else {
			dispIdx = 0
		}
	}

	opts := make([]renderer.SelectOption, 0, rs.RowCount())
	for i := 0; i < rs.RowCount(); i++ {
		opts = append(opts, renderer.SelectOption{
			Value: stringifyValue(rs.Value(i, valIdx)),
			Label: stringifyValue(rs.Value(i, dispIdx)),
		})
	}
	return opts, nil
}

// runQuery executes an arbitrary read query on the engine, used by the
// master-detail subform renderer to auto-filter the child datasheet.
func (r *engineOptionResolver) runQuery(ctx context.Context, sql string, params map[string]any) (*engine.ResultSet, error) {
	if r.eng == nil {
		return nil, fmt.Errorf("subform query: engine is not available")
	}
	rs, err := r.eng.Execute(ctx, sql, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute subform query: %w", err)
	}
	return rs, nil
}

// loadFieldMetaMap returns the field metadata for a table keyed by column name.
func loadFieldMetaMap(store *schema.Store, table string) (map[string]schema.FieldMeta, error) {
	metas, err := store.GetFieldMeta(table)
	if err != nil {
		return nil, fmt.Errorf("failed to load field metadata for %q: %w", table, err)
	}
	out := make(map[string]schema.FieldMeta, len(metas))
	for _, m := range metas {
		out[m.ColumnName] = m
	}
	return out, nil
}

// RenderFormWithOptions renders a FormConfig as an HTML form, resolving any
// combo/lookup options server-side via the provided resolver and routing every
// field through the shared bound-control renderer (renderControl).
func (r *Renderer) RenderFormWithOptions(ctx context.Context, cfg *renderer.FormConfig, resolver OptionResolver) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("form config is required")
	}

	var b strings.Builder

	action := ""
	if cfg.PageID != "" {
		action = fmt.Sprintf("/pages/%s/submit", html.EscapeString(cfg.PageID))
	}

	b.WriteString(fmt.Sprintf(`<form class="vibe-form" method="POST" action="%s">`, html.EscapeString(action)))
	b.WriteString("\n")

	if cfg.FormID != "" {
		b.WriteString(fmt.Sprintf(`  <input type="hidden" name="_form_id" value="%s">`, html.EscapeString(cfg.FormID)))
		b.WriteString("\n")
	}

	for _, field := range cfg.Fields {
		b.WriteString(`  <div class="vibe-form-field">`)
		b.WriteString("\n")

		label := field.Label
		if label == "" {
			label = field.Name
		}
		b.WriteString(fmt.Sprintf(`    <label for="%s">%s</label>`,
			html.EscapeString(field.Name), html.EscapeString(label)))
		b.WriteString("\n")

		control, err := r.renderControl(ctx, field, resolver)
		if err != nil {
			return "", err
		}
		b.WriteString("    ")
		b.WriteString(control)
		b.WriteString("\n")

		b.WriteString("  </div>\n")
	}

	writeFormActions(&b, cfg.Archetype)
	b.WriteString("</form>")

	return b.String(), nil
}

// RenderButton renders a {button:} command-button (plan §5, unit P5.5).
//
// The button carries a data-macro-id (and data-action) hook attribute the Vue
// GUI runtime binds to invoke RunMacro(action) when pressed. There is NO inline
// JS (no onclick=), keeping it consistent with the other bound controls. The
// visible label is HTML-escaped.
func (r *Renderer) RenderButton(cfg *renderer.ButtonConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("button config is required")
	}
	if strings.TrimSpace(cfg.Label) == "" {
		return "", fmt.Errorf("button config requires a label")
	}
	if strings.TrimSpace(cfg.Action) == "" {
		return "", fmt.Errorf("button config requires an action (macro target)")
	}

	action := html.EscapeString(cfg.Action)
	return fmt.Sprintf(
		`<button type="button" class="vibe-button" data-macro-id="%s" data-action="%s">%s</button>`,
		action, action, html.EscapeString(cfg.Label),
	), nil
}

// RenderEditCell renders a single bound control for a datasheet inline-edit
// cell, reusing the SAME control implementation as the form renderer so there is
// no duplicate control logic.
func (r *Renderer) RenderEditCell(ctx context.Context, field renderer.FormField, resolver OptionResolver) (string, error) {
	return r.renderControl(ctx, field, resolver)
}

// renderControl is the single shared bound-control implementation. It dispatches
// on the Access-style control_type (falling back to the legacy field Type) and
// emits the appropriate HTML control, surfacing the ControlTip as a title
// attribute. Combo/lookup options are resolved server-side via the resolver.
func (r *Renderer) renderControl(ctx context.Context, field renderer.FormField, resolver OptionResolver) (string, error) {
	name := html.EscapeString(field.Name)
	titleAttr := ""
	if field.ControlTip != "" {
		titleAttr = fmt.Sprintf(` title="%s"`, html.EscapeString(field.ControlTip))
	}
	requiredAttr := ""
	if field.Required {
		requiredAttr = " required"
	}
	defaultVal := ""
	if field.Default != nil {
		defaultVal = stringifyValue(field.Default)
	}

	controlType := field.ControlType
	if controlType == "" {
		controlType = field.Type
	}

	switch controlType {
	case "combo", "lookup", "select":
		options := field.Options
		if field.LookupSQL != "" && resolver != nil {
			resolved, err := resolver.ResolveOptions(ctx, field.LookupSQL, field.ValueCol, field.DisplayCol)
			if err != nil {
				return "", err
			}
			options = resolved
		}
		return renderComboControl(name, titleAttr, requiredAttr, defaultVal, options), nil

	case "checkbox":
		checked := ""
		if defaultVal == "true" || defaultVal == "1" {
			checked = " checked"
		}
		return fmt.Sprintf(`<input type="checkbox" id="%s" name="%s"%s%s%s>`,
			name, name, checked, requiredAttr, titleAttr), nil

	case "textarea":
		return fmt.Sprintf(`<textarea id="%s" name="%s"%s%s>%s</textarea>`,
			name, name, requiredAttr, titleAttr, html.EscapeString(defaultVal)), nil

	default:
		inputType := mapControlType(controlType)
		return fmt.Sprintf(`<input type="%s" id="%s" name="%s" value="%s"%s%s>`,
			inputType, name, name, html.EscapeString(defaultVal), requiredAttr, titleAttr), nil
	}
}

// renderComboControl emits a <select> that shows the display_col text but
// submits the value_col id (the Access "show 'Visa', store 2" pattern). The
// option whose value matches the bound record's stored value is marked selected.
func renderComboControl(name, titleAttr, requiredAttr, defaultVal string, options []renderer.SelectOption) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<select id="%s" name="%s"%s%s>`, name, name, requiredAttr, titleAttr))
	b.WriteString(`<option value="">-- Select --</option>`)
	for _, opt := range options {
		selected := ""
		if opt.Value == defaultVal {
			selected = " selected"
		}
		b.WriteString(fmt.Sprintf(`<option value="%s"%s>%s</option>`,
			html.EscapeString(opt.Value), selected, html.EscapeString(opt.Label)))
	}
	b.WriteString(`</select>`)
	return b.String()
}

// mapControlType maps a bound-control type (or legacy field type) to an HTML
// input type.
func mapControlType(controlType string) string {
	switch controlType {
	case "number", "currency":
		return "number"
	case "date":
		return "date"
	case "datetime":
		return "datetime-local"
	case "email":
		return "email"
	case "url":
		return "url"
	default:
		return "text"
	}
}

// stringifyValue renders a scanned value as the string used for option values
// and defaults, formatting integers without a trailing decimal.
func stringifyValue(v any) string {
	if v == nil {
		return ""
	}
	switch n := v.(type) {
	case int64:
		return fmt.Sprintf("%d", n)
	case int32:
		return fmt.Sprintf("%d", n)
	case int:
		return fmt.Sprintf("%d", n)
	default:
		return fmt.Sprintf("%v", v)
	}
}
