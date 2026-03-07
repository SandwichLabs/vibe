package html

import (
	"bytes"
	"fmt"
	"html/template"
)

// SidebarItem represents a navigation item in the sidebar.
type SidebarItem struct {
	ID     string
	Title  string
	Active bool
}

const layoutTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Vibe</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
  <style>
    :root {
      --vibe-bg: #ffffff;
      --vibe-fg: #1a1a2e;
      --vibe-primary: #4A90D9;
      --vibe-border: #e0e0e0;
      --vibe-sidebar-bg: #f5f5f5;
      --vibe-metric-bg: #f9f9f9;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      color: var(--vibe-fg);
      background: var(--vibe-bg);
      display: flex;
      min-height: 100vh;
    }
    .vibe-sidebar {
      width: 240px;
      background: var(--vibe-sidebar-bg);
      border-right: 1px solid var(--vibe-border);
      padding: 1rem;
    }
    .vibe-sidebar h2 { font-size: 1rem; margin-bottom: 1rem; color: var(--vibe-primary); }
    .vibe-sidebar a {
      display: block;
      padding: 0.5rem;
      text-decoration: none;
      color: var(--vibe-fg);
      border-radius: 4px;
      margin-bottom: 0.25rem;
    }
    .vibe-sidebar a:hover, .vibe-sidebar a.active {
      background: var(--vibe-primary);
      color: white;
    }
    .vibe-main {
      flex: 1;
      padding: 2rem;
      max-width: 1200px;
    }
    .vibe-main h1 { margin-bottom: 1.5rem; }
    .vibe-table-container { overflow-x: auto; margin: 1rem 0; }
    .vibe-table {
      width: 100%;
      border-collapse: collapse;
    }
    .vibe-table th, .vibe-table td {
      padding: 0.5rem 0.75rem;
      border: 1px solid var(--vibe-border);
      text-align: left;
    }
    .vibe-table thead th {
      position: sticky;
      top: 0;
      background: var(--vibe-sidebar-bg);
      font-weight: 600;
    }
    .vibe-sortable { cursor: pointer; }
    .vibe-table-search {
      padding: 0.5rem;
      margin-bottom: 0.5rem;
      border: 1px solid var(--vibe-border);
      border-radius: 4px;
      width: 100%;
      max-width: 300px;
    }
    .vibe-chart-container { margin: 1rem 0; }
    .vibe-chart-title { margin-bottom: 0.5rem; }
    .vibe-metric {
      display: inline-block;
      padding: 1rem 1.5rem;
      background: var(--vibe-metric-bg);
      border: 1px solid var(--vibe-border);
      border-radius: 8px;
      text-align: center;
      margin: 0.5rem;
    }
    .vibe-metric-value { font-size: 2rem; font-weight: 700; color: var(--vibe-primary); }
    .vibe-metric-label { font-size: 0.875rem; color: #666; margin-top: 0.25rem; }
    .vibe-form { max-width: 600px; margin: 1rem 0; }
    .vibe-form-field { margin-bottom: 1rem; }
    .vibe-form-field label {
      display: block;
      font-weight: 600;
      margin-bottom: 0.25rem;
    }
    .vibe-form-field input, .vibe-form-field select, .vibe-form-field textarea {
      width: 100%;
      padding: 0.5rem;
      border: 1px solid var(--vibe-border);
      border-radius: 4px;
      font-size: 1rem;
    }
    .vibe-form-field input[type="checkbox"] { width: auto; }
    .vibe-form-actions { margin-top: 1rem; }
    .vibe-btn {
      padding: 0.5rem 1.5rem;
      border: none;
      border-radius: 4px;
      cursor: pointer;
      font-size: 1rem;
    }
    .vibe-btn-primary {
      background: var(--vibe-primary);
      color: white;
    }
    .vibe-btn-primary:hover { opacity: 0.9; }
    .vibe-detail { margin: 1rem 0; }
    .vibe-detail dl {
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 0.5rem 1rem;
      padding: 1rem;
      border: 1px solid var(--vibe-border);
      border-radius: 8px;
      margin-bottom: 1rem;
    }
    .vibe-detail dt { font-weight: 600; }
    .vibe-detail dd { margin: 0; }
    .vibe-row { display: grid; margin: 1rem 0; }
    .vibe-col { min-width: 0; }
  </style>
</head>
<body>
  {{if .Sidebar}}
  <nav class="vibe-sidebar">
    <h2>Vibe</h2>
    {{range .Sidebar}}
    <a href="/pages/{{.ID}}"{{if .Active}} class="active"{{end}}>{{.Title}}</a>
    {{end}}
  </nav>
  {{end}}
  <main class="vibe-main">
    <h1>{{.Title}}</h1>
    {{.Content}}
  </main>
</body>
</html>`

// RenderLayout wraps content in the full page HTML layout.
func (r *Renderer) RenderLayout(title string, sidebar []SidebarItem, content string) (string, error) {
	tmpl, err := template.New("layout").Parse(layoutTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse layout template: %w", err)
	}

	data := struct {
		Title   string
		Sidebar []SidebarItem
		Content template.HTML
	}{
		Title:   title,
		Sidebar: sidebar,
		Content: template.HTML(content), //nolint:gosec // content is already escaped by individual renderers
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute layout template: %w", err)
	}

	return buf.String(), nil
}
