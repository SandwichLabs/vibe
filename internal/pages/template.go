package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// TemplateData is the data context available to Go templates in page content.
type TemplateData struct {
	// Results maps SQL block names to their result sets.
	Results map[string]*TemplateResult
	// Params holds page parameters (from URL query params or frontmatter defaults).
	Params map[string]any
	// Vars holds variables from the executor's variable registry.
	Vars map[string]any
	// Meta holds page metadata from frontmatter.
	Meta map[string]string
}

// TemplateResult wraps a ResultSet with template-friendly accessors.
type TemplateResult struct {
	Columns []Column
	Rows    [][]any
}

// ColNames returns column names as a string slice.
func (tr *TemplateResult) ColNames() []string {
	names := make([]string, len(tr.Columns))
	for i, c := range tr.Columns {
		names[i] = c.Name
	}
	return names
}

// ExpandTemplates processes Go template syntax in TextNodes of an executed page.
// It builds a TemplateData context from SQL block results and page params,
// then executes each TextNode's content as a Go text/template.
func ExpandTemplates(executed *ExecutedPage, params map[string]any) (*ExecutedPage, error) {
	// Build results map from executed SQL blocks.
	results := make(map[string]*TemplateResult)
	for _, en := range executed.Nodes {
		if sqlNode, ok := en.Node.(*SQLBlockNode); ok && en.Result != nil {
			results[sqlNode.Name] = &TemplateResult{
				Columns: en.Result.Columns,
				Rows:    en.Result.Rows,
			}
		}
	}

	// If no results and no template syntax detected, skip processing.
	if len(results) == 0 && !hasTemplateSyntax(executed) {
		return executed, nil
	}

	// Build metadata from frontmatter.
	meta := map[string]string{
		"title": executed.Frontmatter.Title,
		"type":  executed.Frontmatter.Type,
	}
	for k, v := range executed.Frontmatter.Extra {
		meta[k] = v
	}

	data := &TemplateData{
		Results: results,
		Params:  params,
		Vars:    executed.Variables,
		Meta:    meta,
	}
	if data.Vars == nil {
		data.Vars = make(map[string]any)
	}

	// Process each TextNode through the template engine.
	expanded := &ExecutedPage{
		Frontmatter: executed.Frontmatter,
		Variables:   executed.Variables,
		Nodes:       make([]ExecutedNode, len(executed.Nodes)),
	}
	copy(expanded.Nodes, executed.Nodes)

	for i, en := range expanded.Nodes {
		textNode, ok := en.Node.(*TextNode)
		if !ok {
			continue
		}
		if !strings.Contains(textNode.Content, "{{") {
			continue
		}

		output, err := executeTemplate(textNode.Content, data)
		if err != nil {
			return nil, fmt.Errorf("template error in text block: %w", err)
		}

		expanded.Nodes[i] = ExecutedNode{
			Node:         &TextNode{Content: output},
			Result:       en.Result,
			InlineValues: en.InlineValues,
		}
	}

	return expanded, nil
}

// executeTemplate parses and executes a single template string.
func executeTemplate(content string, data *TemplateData) (string, error) {
	tmpl, err := template.New("page").Funcs(templateFuncs()).Parse(content)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}

	return buf.String(), nil
}

// templateFuncs returns the custom function map for page templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// col returns the column name at the given index.
		"col": func(tr *TemplateResult, idx int) string {
			if tr == nil || idx < 0 || idx >= len(tr.Columns) {
				return ""
			}
			return tr.Columns[idx].Name
		},

		// cell returns the value at the given index in a row.
		"cell": func(row []any, idx int) any {
			if idx < 0 || idx >= len(row) {
				return nil
			}
			return row[idx]
		},

		// count returns the number of rows in a result.
		"count": func(tr *TemplateResult) int {
			if tr == nil {
				return 0
			}
			return len(tr.Rows)
		},

		// sum adds up all numeric values in a column.
		"sum": func(tr *TemplateResult, colIdx int) float64 {
			if tr == nil {
				return 0
			}
			var total float64
			for _, row := range tr.Rows {
				if colIdx < 0 || colIdx >= len(row) {
					continue
				}
				total += toFloat64(row[colIdx])
			}
			return total
		},

		// fmt_currency formats a number as $1,234.56.
		"fmt_currency": func(v any) string {
			f := toFloat64(v)
			negative := f < 0
			if negative {
				f = -f
			}
			whole := int64(f)
			cents := int64((f - float64(whole)) * 100 + 0.5)
			if cents >= 100 {
				whole++
				cents -= 100
			}
			s := formatCommas(whole)
			prefix := "$"
			if negative {
				prefix = "-$"
			}
			return fmt.Sprintf("%s%s.%02d", prefix, s, cents)
		},

		// fmt_percent formats a number as a percentage (0.42 → "42.0%").
		"fmt_percent": func(v any) string {
			f := toFloat64(v)
			return fmt.Sprintf("%.1f%%", f*100)
		},

		// fmt_number formats a number with comma separators.
		"fmt_number": func(v any) string {
			f := toFloat64(v)
			return formatCommas(int64(f))
		},

		// json marshals a value to JSON.
		"json": func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprintf("json error: %v", err)
			}
			return string(b)
		},

		// add returns a + b.
		"add": func(a, b int) int { return a + b },

		// sub returns a - b.
		"sub": func(a, b int) int { return a - b },

		// mul returns a * b.
		"mul": func(a, b int) int { return a * b },

		// default returns the fallback if val is empty/nil/zero.
		"default": func(fallback, val any) any {
			if val == nil {
				return fallback
			}
			if s, ok := val.(string); ok && s == "" {
				return fallback
			}
			return val
		},

		// join joins strings with a separator.
		"join": strings.Join,

		// upper/lower for string manipulation.
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
	}
}

// hasTemplateSyntax checks if any TextNode contains {{ }}.
func hasTemplateSyntax(page *ExecutedPage) bool {
	for _, en := range page.Nodes {
		if textNode, ok := en.Node.(*TextNode); ok {
			if strings.Contains(textNode.Content, "{{") {
				return true
			}
		}
	}
	return false
}

// toFloat64 converts common numeric types to float64.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	case string:
		var f float64
		fmt.Sscanf(n, "%f", &f)
		return f
	default:
		return 0
	}
}

// formatCommas adds comma separators to an integer.
func formatCommas(n int64) string {
	if n < 0 {
		return "-" + formatCommas(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}
