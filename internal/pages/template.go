package pages

import (
	"bytes"
	"text/template"
)

// TemplateData is the data available to Go templates in text nodes.
type TemplateData struct {
	Vars    map[string]any
	Results map[string]*ResultSet
}

// ExpandTemplates processes Go template expressions in text nodes of an executed page.
// Variables are available as {{.Vars.name}} and result sets as {{.Results.name}}.
func ExpandTemplates(executed *ExecutedPage, results map[string]*ResultSet) error {
	if results == nil {
		results = make(map[string]*ResultSet)
	}

	// Collect results from executed nodes.
	for _, en := range executed.Nodes {
		if en.Result != nil {
			if sqlNode, ok := en.Node.(*SQLBlockNode); ok {
				results[sqlNode.Name] = en.Result
			}
		}
	}

	data := &TemplateData{
		Vars:    executed.Variables,
		Results: results,
	}
	if data.Vars == nil {
		data.Vars = make(map[string]any)
	}

	for i, en := range executed.Nodes {
		textNode, ok := en.Node.(*TextNode)
		if !ok {
			continue
		}

		tmpl, err := template.New("page").Parse(textNode.Content)
		if err != nil {
			// If template parsing fails, leave the content as-is.
			continue
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			// If template execution fails, leave the content as-is.
			continue
		}

		executed.Nodes[i].Node = &TextNode{Content: buf.String()}
	}

	return nil
}
