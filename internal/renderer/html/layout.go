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
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,400;9..144,500;9..144,600;9..144,700&family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
  <style>
    /* SchoolHouse design system — tokens mirrored from the Studio frontend
       (studio/frontend/src/schoolhouse/colors_and_type.css). */
    :root {
      --sh-ink:        #1F2330;
      --sh-paper:      #FAF7F0;
      --sh-paper-2:    #F2EDDF;
      --sh-surface:    #FFFFFF;
      --sh-graphite:   #5C6275;
      --sh-chalk:      #A8A89C;
      --sh-teal:       #2F8F87;
      --sh-teal-lo:    #236962;
      --sh-marigold:   #F2A93B;
      --sh-coral:      #E27262;
      --sh-error:      #B23B2E;

      --sh-font-display: 'Fraunces', Georgia, 'Times New Roman', serif;
      --sh-font-body:    'Inter', system-ui, -apple-system, sans-serif;
      --sh-font-mono:    'JetBrains Mono', 'SF Mono', Menlo, monospace;

      --sh-fs-xs: 12px;
      --sh-fs-sm: 14px;
      --sh-fs-base: 16px;
      --sh-fs-lg: 18px;
      --sh-fs-xl: 22px;
      --sh-fs-2xl: 28px;
      --sh-fs-3xl: 36px;

      --sh-track-display:   -0.015em;
      --sh-track-mono-caps:  0.06em;

      --sh-radius-sm: 2px;
      --sh-radius:    4px;

      --sh-border-thin: 1.5px solid var(--sh-ink);
      --sh-border:      2px solid var(--sh-ink);

      --sh-shadow-sm:   2px 2px 0 0 rgba(31,35,48,0.85);
      --sh-shadow:      3px 3px 0 0 rgba(31,35,48,0.85);
      --sh-shadow-lg:   5px 5px 0 0 rgba(31,35,48,0.85);
      --sh-shadow-teal: 3px 3px 0 0 var(--sh-teal);
    }

    * { box-sizing: border-box; margin: 0; padding: 0; }
    html, body { height: 100%; }
    body {
      font-family: var(--sh-font-body);
      font-size: var(--sh-fs-base);
      line-height: 1.5;
      color: var(--sh-ink);
      background: var(--sh-paper);
      display: flex;
      min-height: 100vh;
      -webkit-font-smoothing: antialiased;
      -moz-osx-font-smoothing: grayscale;
    }

    /* Sidebar — warm paper rail with mono labels, teal active marker */
    .vibe-sidebar {
      width: 240px;
      background: var(--sh-paper-2);
      color: var(--sh-ink);
      padding: 0;
      border-right: var(--sh-border);
      flex-shrink: 0;
      position: sticky;
      top: 0;
      height: 100vh;
      overflow-y: auto;
    }
    .vibe-sidebar-header {
      padding: 18px 20px;
      border-bottom: var(--sh-border);
      background: var(--sh-surface);
    }
    .vibe-sidebar h2 {
      font-family: var(--sh-font-display);
      font-size: var(--sh-fs-xl);
      font-weight: 600;
      color: var(--sh-ink);
      margin: 0;
      letter-spacing: var(--sh-track-display);
      border-bottom: 3px solid var(--sh-teal);
      display: inline-block;
      padding-bottom: 1px;
    }
    .vibe-sidebar-nav { padding: 8px 0; }
    .vibe-sidebar a {
      display: block;
      padding: 9px 20px;
      text-decoration: none;
      color: var(--sh-ink);
      font-family: var(--sh-font-body);
      font-weight: 500;
      font-size: var(--sh-fs-sm);
      border-left: 4px solid transparent;
      transition: background 80ms, color 80ms;
    }
    .vibe-sidebar a:hover { background: rgba(31, 35, 48, 0.04); }
    .vibe-sidebar a.active {
      background: var(--sh-surface);
      color: var(--sh-ink);
      border-left-color: var(--sh-teal);
      font-weight: 600;
    }

    /* Main */
    .vibe-main {
      flex: 1;
      padding: 32px 40px;
      max-width: 1280px;
      min-width: 0;
    }
    .vibe-main h1 {
      font-family: var(--sh-font-display);
      font-size: var(--sh-fs-3xl);
      font-weight: 600;
      line-height: 1.1;
      letter-spacing: var(--sh-track-display);
      margin-bottom: 24px;
    }
    .vibe-main h2 {
      font-family: var(--sh-font-display);
      font-size: var(--sh-fs-2xl);
      font-weight: 600;
      line-height: 1.1;
      letter-spacing: var(--sh-track-display);
      margin: 32px 0 12px;
    }
    .vibe-main h3 {
      font-family: var(--sh-font-display);
      font-size: var(--sh-fs-xl);
      font-weight: 600;
      line-height: 1.15;
      margin: 24px 0 12px;
    }
    .vibe-main p { margin-bottom: 16px; line-height: 1.6; }
    .vibe-main code {
      font-family: var(--sh-font-mono);
      font-size: 0.92em;
      background: var(--sh-paper-2);
      padding: 1px 6px;
      border-radius: var(--sh-radius-sm);
      border: var(--sh-border-thin);
    }
    .vibe-main pre {
      font-family: var(--sh-font-mono);
      font-size: var(--sh-fs-sm);
      background: var(--sh-ink);
      color: var(--sh-paper);
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      padding: 16px;
      overflow-x: auto;
      margin: 16px 0;
    }
    .vibe-main pre code {
      background: transparent;
      border: none;
      padding: 0;
      color: inherit;
    }

    /* Tables */
    .vibe-table-container { overflow-x: auto; margin: 16px 0; }
    .vibe-table {
      width: 100%;
      border-collapse: collapse;
      font-size: var(--sh-fs-sm);
      background: var(--sh-surface);
    }
    .vibe-table th, .vibe-table td {
      padding: 8px 12px;
      border: var(--sh-border-thin);
      text-align: left;
    }
    .vibe-table thead th {
      position: sticky;
      top: 0;
      background: var(--sh-paper-2);
      font-family: var(--sh-font-mono);
      font-weight: 600;
      text-transform: uppercase;
      font-size: var(--sh-fs-xs);
      letter-spacing: var(--sh-track-mono-caps);
    }
    .vibe-table tbody tr:nth-child(even) { background: var(--sh-paper-2); }
    .vibe-table tbody tr:hover { background: rgba(47, 143, 135, 0.08); }
    .vibe-sortable { cursor: pointer; user-select: none; }
    .vibe-sortable:hover { background: var(--sh-paper); }
    .vibe-table-search {
      padding: 10px 14px;
      margin-bottom: 8px;
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      background: var(--sh-surface);
      width: 100%;
      max-width: 320px;
      font-family: var(--sh-font-body);
      font-size: var(--sh-fs-sm);
      outline: none;
    }
    .vibe-table-search:focus { box-shadow: var(--sh-shadow-teal); }

    /* Charts */
    .vibe-chart-container {
      margin: 16px 0;
      padding: 16px;
      background: var(--sh-surface);
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      box-shadow: var(--sh-shadow);
    }
    .vibe-chart-title {
      font-family: var(--sh-font-display);
      font-weight: 600;
      font-size: var(--sh-fs-lg);
      letter-spacing: var(--sh-track-display);
      margin-bottom: 8px;
    }

    /* Metrics */
    .vibe-metric {
      display: inline-block;
      padding: 16px 24px;
      background: var(--sh-surface);
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      box-shadow: var(--sh-shadow);
      text-align: left;
      margin: 8px;
      min-width: 160px;
    }
    .vibe-metric-value {
      font-family: var(--sh-font-display);
      font-size: var(--sh-fs-3xl);
      font-weight: 600;
      line-height: 1.1;
      letter-spacing: var(--sh-track-display);
      color: var(--sh-ink);
    }
    .vibe-metric-label {
      font-family: var(--sh-font-mono);
      font-size: var(--sh-fs-xs);
      letter-spacing: var(--sh-track-mono-caps);
      text-transform: uppercase;
      color: var(--sh-graphite);
      margin-top: 6px;
    }

    /* Forms */
    .vibe-form { max-width: 600px; margin: 16px 0; }
    .vibe-form-field { margin-bottom: 16px; }
    .vibe-form-field label {
      display: block;
      font-family: var(--sh-font-mono);
      font-weight: 600;
      font-size: var(--sh-fs-xs);
      text-transform: uppercase;
      letter-spacing: var(--sh-track-mono-caps);
      color: var(--sh-graphite);
      margin-bottom: 6px;
    }
    .vibe-form-field input,
    .vibe-form-field select,
    .vibe-form-field textarea {
      width: 100%;
      padding: 10px 14px;
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      background: var(--sh-surface);
      font-family: var(--sh-font-body);
      font-size: var(--sh-fs-sm);
      outline: none;
    }
    .vibe-form-field input:focus,
    .vibe-form-field select:focus,
    .vibe-form-field textarea:focus { box-shadow: var(--sh-shadow-teal); }
    .vibe-form-field input[type="checkbox"] { width: auto; accent-color: var(--sh-teal); }
    .vibe-form-actions { margin-top: 16px; }

    /* Buttons */
    .vibe-btn {
      padding: 10px 18px;
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      background: var(--sh-surface);
      color: var(--sh-ink);
      box-shadow: var(--sh-shadow);
      cursor: pointer;
      font-family: var(--sh-font-body);
      font-size: var(--sh-fs-sm);
      font-weight: 600;
      letter-spacing: 0.01em;
      transition: transform 120ms, box-shadow 120ms;
    }
    .vibe-btn:hover { transform: translate(-1px, -1px); box-shadow: var(--sh-shadow-lg); }
    .vibe-btn:active { transform: translate(1px, 1px); box-shadow: var(--sh-shadow-sm); }
    .vibe-btn-primary { background: var(--sh-teal); color: var(--sh-paper); }

    /* Detail records */
    .vibe-detail { margin: 16px 0; }
    .vibe-detail dl {
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 12px 20px;
      padding: 20px;
      background: var(--sh-surface);
      border: var(--sh-border);
      border-radius: var(--sh-radius-sm);
      box-shadow: var(--sh-shadow);
      margin-bottom: 16px;
    }
    .vibe-detail dt {
      font-family: var(--sh-font-mono);
      font-weight: 600;
      font-size: var(--sh-fs-xs);
      text-transform: uppercase;
      letter-spacing: var(--sh-track-mono-caps);
      color: var(--sh-graphite);
    }
    .vibe-detail dd { margin: 0; }

    /* Grid layout */
    .vibe-row { display: grid; gap: 16px; margin: 16px 0; }
    .vibe-col { min-width: 0; }
  </style>
</head>
<body>
  {{if .Sidebar}}
  <nav class="vibe-sidebar">
    <div class="vibe-sidebar-header">
      <h2>Vibe</h2>
    </div>
    <div class="vibe-sidebar-nav">
    {{range .Sidebar}}
    <a href="/pages/{{.ID}}"{{if .Active}} class="active"{{end}}>{{.Title}}</a>
    {{end}}
    </div>
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
