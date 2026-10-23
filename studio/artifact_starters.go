package main

import (
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// The starters New artifact offers (Library plan L1/L6). They are Studio's
// examples, not the format, so they live here and not in internal/artifacts.
// Every kind shares one manifest body (two date parameters, one query), so the
// kinds read alike; only the `view:` line, and for a page the files, differ.
// None of them names a network address, and the scripted ones start undecided
// like any other: they open as source with an Allow button.

// Starter kinds, as NewArtifact's kind argument spells them.
const (
	starterA2UI       = "a2ui"
	starterHTML       = "html"
	starterHTMLScript = "html-script"
	starterScript     = "script"
)

// starterKinds is the list a refusal names, in picker order.
var starterKinds = []string{starterA2UI, starterHTML, starterHTMLScript, starterScript}

const starterManifestHead = `version: 1
title: "%s"
description: A starting point. Edit the query and the view.
`

const starterManifestBody = `parameters:
  - name: from
    type: date
    default: today-30d
  - name: to
    type: date
    default: today
queries:
  - name: span
    sql: SELECT ($to::DATE - $from::DATE) + 1 AS days
`

// starterManifestFor is the manifest text of a kind: the head, an optional
// view line, then the shared body.
func starterManifestFor(name, kind string) string {
	view := ""
	switch kind {
	case starterHTML, starterHTMLScript:
		view = "view: " + artifacts.ViewHTML + "\n"
	case starterScript:
		view = "view: " + artifacts.ViewScript + "\n"
	}
	return fmt.Sprintf(starterManifestHead, name) + view + starterManifestBody
}

const starterView = `{
  "title": "Starter",
  "components": [
    {"id": "controls", "type": "container", "layout": "row"},
    {"id": "dates", "type": "dateRange", "parent": "controls", "start": "from", "end": "to", "label": "Dates"},
    {"id": "days", "type": "stat", "label": "Days in range", "query": "span"}
  ]
}
`

const starterPageEntry = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Starter</title>
<link rel="stylesheet" href="assets/style.css">
</head>
<body>
<main>
<h1>Starter page</h1>
<p class="muted">From {{ param "from" }} to {{ param "to" }}. Edit the query in artifact.yaml and this page in html/index.html.tmpl.</p>
{{ $span := query "span" }}
<table>
<thead><tr><th>Days in range</th></tr></thead>
<tbody>{{ range $span.Rows }}<tr><td id="days">{{ number (index . 0) }}</td></tr>{{ end }}</tbody>
</table>
</main>
%s</body>
</html>
`

const starterPageScriptTag = `<script src="app.js"></script>
`

// starterPageCSS uses the person's theme roles, which only the page window
// links (_vibe/theme.css); a static page shows in a sandboxed frame without
// them, so each role has a system-colour fallback. No resource is loaded.
const starterPageCSS = `:root {
  color-scheme: light dark;
}
body {
  margin: 0;
  background: var(--vibe-bg, Canvas);
  color: var(--vibe-fg, CanvasText);
  font-family: system-ui, sans-serif;
}
main {
  max-width: 40rem;
  margin: 0 auto;
  padding: 2rem 1rem;
}
h1 {
  margin: 0 0 0.5rem;
  font-size: 1.5rem;
}
.muted {
  color: var(--vibe-fg-muted, GrayText);
}
table {
  width: 100%;
  border-collapse: collapse;
  background: var(--vibe-surface, Canvas);
}
th,
td {
  padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--vibe-border, GrayText);
  text-align: left;
}
`

// starterPageJS asks for the same query again from the page. It shows how
// vibe.run reaches a named query; the page works without it.
const starterPageJS = `// Runs the "span" query again from the page and updates the cell.
// vibe.run only reaches the named queries in artifact.yaml.
async function refresh() {
  const params = await vibe.params()
  const res = await vibe.run('span', { from: params.from.value, to: params.to.value })
  const cell = document.getElementById('days')
  if (cell && res.rows.length > 0) {
    cell.textContent = String(res.rows[0][0])
  }
}
refresh().catch(() => {})
`

// starterScriptJS draws a one-bar SVG from the "span" query.
const starterScriptJS = `// draw() is called by Vibe with the current parameters and the space available.
// It returns an SVG string. vibe.run only reaches the named queries in artifact.yaml.
function draw(ctx) {
  return vibe.run('span', { from: ctx.params.from, to: ctx.params.to }).then((res) => {
    const days = Number(res.rows.length > 0 ? res.rows[0][0] : 0)
    const w = Math.max(200, ctx.width)
    const h = 120
    const bar = Math.min(w - 40, Math.max(2, days * 4))
    return {
      svg: '<svg xmlns="http://www.w3.org/2000/svg" width="' + w + '" height="' + h + '">' +
        '<rect x="20" y="30" width="' + bar + '" height="30" fill="' + vibe.theme['chart.1'] + '"/>' +
        '<text x="20" y="90" font-family="sans-serif" font-size="14" fill="' + vibe.theme.fg + '">' +
        days + ' days in range</text></svg>',
    }
  })
}
`

// starter is one kind's files: the manifest text and the view file bytes (a2ui
// document or view.js), and for a page its file set.
type starter struct {
	manifest string
	view     string
	files    []artifacts.PageFile
}

// starterFor builds a kind's starter, or refuses a kind it does not know with
// the list of kinds. An empty kind is the live view starter.
func starterFor(name, kind string) (*starter, error) {
	if kind == "" {
		kind = starterA2UI
	}
	s := &starter{manifest: starterManifestFor(name, kind)}
	switch kind {
	case starterA2UI:
		s.view = starterView
	case starterHTML:
		s.files = pageStarterFiles("")
	case starterHTMLScript:
		s.files = pageStarterFiles(starterPageScriptTag)
		s.files = append(s.files, artifacts.PageFile{Path: "app.js", Data: []byte(starterPageJS)})
	case starterScript:
		s.view = starterScriptJS
	default:
		return nil, fmt.Errorf("unknown artifact kind %.40q: use one of %s", kind, strings.Join(starterKinds, ", "))
	}
	return s, nil
}

// pageStarterFiles is the static page's file set; scriptTag, when set, is
// placed before </body>.
func pageStarterFiles(scriptTag string) []artifacts.PageFile {
	return []artifacts.PageFile{
		{Path: artifacts.DefaultEntry, Data: []byte(fmt.Sprintf(starterPageEntry, scriptTag))},
		{Path: "assets/style.css", Data: []byte(starterPageCSS)},
	}
}
