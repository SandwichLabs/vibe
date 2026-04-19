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
  <link href="https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600;700&family=IBM+Plex+Sans:wght@400;500;600;700&display=swap" rel="stylesheet">
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
  <style>
    :root {
      --sl-signal-yellow:    #FFD700;
      --sl-signal-yellow-lo: #E6C200;
      --sl-absolute-black:   #000000;
      --sl-warm-white:       #FAFAF5;
      --sl-warm-gray:        #E8E5DD;
      --sl-warm-gray-2:      #F5F0E8;
      --sl-paper:            #FFFBEA;
      --sl-surface:          #FFFFFF;
      --sl-muted-brown:      #8B7355;
      --sl-muted:            #777777;
      --sl-error:            #CC0000;

      --sl-font-display:     'Space Grotesk', system-ui, sans-serif;
      --sl-font-body:        'Space Grotesk', system-ui, sans-serif;
      --sl-font-mono:        'IBM Plex Mono', 'Courier New', monospace;

      --sl-fs-xs: 11px;
      --sl-fs-sm: 13px;
      --sl-fs-base: 15px;
      --sl-fs-lg: 18px;
      --sl-fs-xl: 22px;
      --sl-fs-2xl: 30px;
      --sl-fs-3xl: 44px;

      --sl-tracking-tight: -0.02em;
      --sl-tracking-mono:  0.05em;

      --sl-border-thin:  2px solid var(--sl-absolute-black);
      --sl-border-mid:   3px solid var(--sl-absolute-black);

      --sl-shadow:       4px 4px 0 0 var(--sl-absolute-black);
      --sl-shadow-lg:    6px 6px 0 0 var(--sl-absolute-black);
      --sl-shadow-sm:    2px 2px 0 0 var(--sl-absolute-black);
      --sl-shadow-yellow: 4px 4px 0 0 var(--sl-signal-yellow);
    }

    * { box-sizing: border-box; margin: 0; padding: 0; border-radius: 0; }
    html, body { height: 100%; }
    body {
      font-family: var(--sl-font-body);
      font-size: var(--sl-fs-base);
      line-height: 1.45;
      color: var(--sl-absolute-black);
      background: var(--sl-warm-white);
      display: flex;
      min-height: 100vh;
      -webkit-font-smoothing: antialiased;
    }

    /* Sidebar — black bar with yellow top accent, mono labels */
    .vibe-sidebar {
      width: 240px;
      background: var(--sl-absolute-black);
      color: #fff;
      padding: 0;
      border-right: var(--sl-border-mid);
      flex-shrink: 0;
      position: sticky;
      top: 0;
      height: 100vh;
      overflow-y: auto;
    }
    .vibe-sidebar-header {
      padding: 16px 20px;
      border-bottom: 3px solid var(--sl-signal-yellow);
    }
    .vibe-sidebar h2 {
      font-family: var(--sl-font-display);
      font-size: var(--sl-fs-lg);
      font-weight: 700;
      color: var(--sl-signal-yellow);
      margin: 0;
      letter-spacing: var(--sl-tracking-tight);
    }
    .vibe-sidebar-nav { padding: 8px 0; }
    .vibe-sidebar a {
      display: block;
      padding: 10px 20px;
      text-decoration: none;
      color: rgba(255, 255, 255, 0.75);
      font-family: var(--sl-font-display);
      font-weight: 500;
      font-size: var(--sl-fs-sm);
      border-left: 4px solid transparent;
      transition: background 80ms, color 80ms;
    }
    .vibe-sidebar a:hover {
      background: rgba(255, 255, 255, 0.08);
      color: #fff;
    }
    .vibe-sidebar a.active {
      background: var(--sl-signal-yellow);
      color: var(--sl-absolute-black);
      border-left-color: var(--sl-absolute-black);
      font-weight: 700;
    }

    /* Main */
    .vibe-main {
      flex: 1;
      padding: 32px 40px;
      max-width: 1280px;
      min-width: 0;
    }
    .vibe-main h1 {
      font-family: var(--sl-font-display);
      font-size: var(--sl-fs-3xl);
      font-weight: 700;
      line-height: 1.15;
      letter-spacing: var(--sl-tracking-tight);
      text-transform: uppercase;
      margin-bottom: 24px;
    }
    .vibe-main h2 {
      font-family: var(--sl-font-display);
      font-size: var(--sl-fs-2xl);
      font-weight: 700;
      margin: 32px 0 12px;
    }
    .vibe-main h3 {
      font-family: var(--sl-font-display);
      font-size: var(--sl-fs-xl);
      font-weight: 700;
      margin: 24px 0 12px;
    }
    .vibe-main p { margin-bottom: 16px; line-height: 1.65; }
    .vibe-main code {
      font-family: var(--sl-font-mono);
      font-weight: 600;
      font-size: 0.9em;
      background: var(--sl-signal-yellow);
      padding: 0.05em 0.4em;
      border: var(--sl-border-thin);
    }
    .vibe-main pre {
      font-family: var(--sl-font-mono);
      font-size: var(--sl-fs-sm);
      background: var(--sl-warm-gray-2);
      border: var(--sl-border-mid);
      padding: 16px;
      overflow-x: auto;
      margin: 16px 0;
    }
    .vibe-main pre code {
      background: transparent;
      border: none;
      padding: 0;
    }

    /* Tables */
    .vibe-table-container { overflow-x: auto; margin: 16px 0; }
    .vibe-table {
      width: 100%;
      border-collapse: collapse;
      font-size: var(--sl-fs-sm);
      background: var(--sl-surface);
    }
    .vibe-table th, .vibe-table td {
      padding: 8px 12px;
      border: var(--sl-border-thin);
      text-align: left;
    }
    .vibe-table thead th {
      position: sticky;
      top: 0;
      background: var(--sl-signal-yellow);
      font-family: var(--sl-font-mono);
      font-weight: 700;
      text-transform: uppercase;
      font-size: var(--sl-fs-xs);
      letter-spacing: var(--sl-tracking-mono);
    }
    .vibe-table tbody tr:nth-child(even) { background: var(--sl-warm-gray); }
    .vibe-sortable { cursor: pointer; user-select: none; }
    .vibe-sortable:hover { background: var(--sl-signal-yellow-lo); }
    .vibe-table-search {
      padding: 8px 12px;
      margin-bottom: 8px;
      border: var(--sl-border-mid);
      background: var(--sl-surface);
      width: 100%;
      max-width: 320px;
      font-family: var(--sl-font-body);
      font-size: var(--sl-fs-sm);
      outline: none;
    }
    .vibe-table-search:focus { box-shadow: var(--sl-shadow-yellow); }

    /* Charts */
    .vibe-chart-container {
      margin: 16px 0;
      padding: 16px;
      background: var(--sl-surface);
      border: var(--sl-border-mid);
      box-shadow: var(--sl-shadow);
    }
    .vibe-chart-title {
      font-family: var(--sl-font-display);
      font-weight: 700;
      font-size: var(--sl-fs-lg);
      margin-bottom: 8px;
    }

    /* Metrics */
    .vibe-metric {
      display: inline-block;
      padding: 16px 24px;
      background: var(--sl-surface);
      border: var(--sl-border-mid);
      box-shadow: var(--sl-shadow);
      text-align: left;
      margin: 8px;
      min-width: 160px;
    }
    .vibe-metric-value {
      font-family: var(--sl-font-display);
      font-size: var(--sl-fs-3xl);
      font-weight: 700;
      line-height: 1.02;
      letter-spacing: var(--sl-tracking-tight);
      color: var(--sl-absolute-black);
    }
    .vibe-metric-label {
      font-family: var(--sl-font-mono);
      font-size: var(--sl-fs-xs);
      letter-spacing: var(--sl-tracking-mono);
      text-transform: uppercase;
      color: var(--sl-muted-brown);
      margin-top: 6px;
    }

    /* Forms */
    .vibe-form { max-width: 600px; margin: 16px 0; }
    .vibe-form-field { margin-bottom: 16px; }
    .vibe-form-field label {
      display: block;
      font-family: var(--sl-font-mono);
      font-weight: 700;
      font-size: var(--sl-fs-xs);
      text-transform: uppercase;
      letter-spacing: var(--sl-tracking-mono);
      margin-bottom: 6px;
    }
    .vibe-form-field input,
    .vibe-form-field select,
    .vibe-form-field textarea {
      width: 100%;
      padding: 10px 14px;
      border: var(--sl-border-mid);
      background: var(--sl-surface);
      font-family: var(--sl-font-body);
      font-size: var(--sl-fs-sm);
      outline: none;
    }
    .vibe-form-field input:focus,
    .vibe-form-field select:focus,
    .vibe-form-field textarea:focus { box-shadow: var(--sl-shadow-yellow); }
    .vibe-form-field input[type="checkbox"] { width: auto; }
    .vibe-form-actions { margin-top: 16px; }

    /* Buttons */
    .vibe-btn {
      padding: 10px 20px;
      border: var(--sl-border-mid);
      background: var(--sl-surface);
      color: var(--sl-absolute-black);
      box-shadow: var(--sl-shadow);
      cursor: pointer;
      font-family: var(--sl-font-display);
      font-size: var(--sl-fs-sm);
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.02em;
      transition: transform 80ms, box-shadow 80ms;
    }
    .vibe-btn:hover { transform: translate(-2px, -2px); box-shadow: var(--sl-shadow-lg); }
    .vibe-btn:active { transform: translate(2px, 2px); box-shadow: var(--sl-shadow-sm); }
    .vibe-btn-primary { background: var(--sl-signal-yellow); color: var(--sl-absolute-black); }

    /* Detail records */
    .vibe-detail { margin: 16px 0; }
    .vibe-detail dl {
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 12px 20px;
      padding: 20px;
      background: var(--sl-surface);
      border: var(--sl-border-mid);
      box-shadow: var(--sl-shadow);
      margin-bottom: 16px;
    }
    .vibe-detail dt {
      font-family: var(--sl-font-mono);
      font-weight: 700;
      font-size: var(--sl-fs-xs);
      text-transform: uppercase;
      letter-spacing: var(--sl-tracking-mono);
      color: var(--sl-muted-brown);
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
