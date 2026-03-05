package html

import (
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// RenderForm renders a FormConfig as an HTML form with labeled inputs.
func (r *Renderer) RenderForm(cfg *renderer.FormConfig) (string, error) {
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
		escapedLabel := html.EscapeString(label)
		escapedName := html.EscapeString(field.Name)

		requiredAttr := ""
		if field.Required {
			requiredAttr = " required"
		}

		b.WriteString(fmt.Sprintf(`    <label for="%s">%s</label>`, escapedName, escapedLabel))
		b.WriteString("\n")

		defaultVal := ""
		if field.Default != nil {
			defaultVal = fmt.Sprintf("%v", field.Default)
		}

		switch field.Type {
		case "select":
			renderSelectField(&b, escapedName, requiredAttr, defaultVal, field.Options)
		case "checkbox":
			checked := ""
			if defaultVal == "true" || defaultVal == "1" {
				checked = " checked"
			}
			b.WriteString(fmt.Sprintf(`    <input type="checkbox" id="%s" name="%s"%s%s>`,
				escapedName, escapedName, checked, requiredAttr))
			b.WriteString("\n")
		case "textarea":
			b.WriteString(fmt.Sprintf(`    <textarea id="%s" name="%s"%s>%s</textarea>`,
				escapedName, escapedName, requiredAttr, html.EscapeString(defaultVal)))
			b.WriteString("\n")
		default:
			inputType := mapFieldType(field.Type)
			b.WriteString(fmt.Sprintf(`    <input type="%s" id="%s" name="%s" value="%s"%s>`,
				inputType, escapedName, escapedName, html.EscapeString(defaultVal), requiredAttr))
			b.WriteString("\n")
		}

		b.WriteString("  </div>\n")
	}

	b.WriteString(`  <div class="vibe-form-actions">`)
	b.WriteString("\n")
	b.WriteString(`    <button type="submit" class="vibe-btn vibe-btn-primary">Submit</button>`)
	b.WriteString("\n")
	b.WriteString("  </div>\n")
	b.WriteString("</form>")

	return b.String(), nil
}

// renderSelectField writes a <select> element with options.
func renderSelectField(b *strings.Builder, name, requiredAttr, defaultVal string, options []renderer.SelectOption) {
	b.WriteString(fmt.Sprintf(`    <select id="%s" name="%s"%s>`, name, name, requiredAttr))
	b.WriteString("\n")
	b.WriteString(`      <option value="">-- Select --</option>`)
	b.WriteString("\n")
	for _, opt := range options {
		selected := ""
		if opt.Value == defaultVal {
			selected = " selected"
		}
		b.WriteString(fmt.Sprintf(`      <option value="%s"%s>%s</option>`,
			html.EscapeString(opt.Value), selected, html.EscapeString(opt.Label)))
		b.WriteString("\n")
	}
	b.WriteString("    </select>\n")
}

// mapFieldType maps vibe field types to HTML input types.
func mapFieldType(fieldType string) string {
	switch fieldType {
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
