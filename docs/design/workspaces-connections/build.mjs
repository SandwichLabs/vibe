// Generates the #106 mockup pages.   node docs/design/workspaces-connections/build.mjs
// Docs tooling only. Outputs are plain static HTML + mock.css; open index.html.
import { writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import { icon } from './lib.mjs'
import { pageLauncher, pageNew, pageImport, pageSwitcher } from './pages-a.mjs'
import { pageRail, pageEditor } from './pages-b.mjs'
import { pageStates, pageQuery, pageSettings, pageAgent } from './pages-c.mjs'

const here = dirname(fileURLToPath(import.meta.url))
execFileSync('node', [join(here, 'build-css.mjs')], { stdio: 'inherit' })

const pages = [
  ['01-launcher.html', pageLauncher(), 'Launcher', 'Replaces the unlock screen: recent workspaces, open, new, import, first-run.', '#111'],
  ['02-new-workspace.html', pageNew(), 'New workspace', 'Name, location, git init, starter template. Validation state.', '#111'],
  ['03-import-vibe.html', pageImport(), 'Import .vibe', 'Unlock with a "can\'t be recovered" warning, preview what will be created, result.', '#110 #111'],
  ['04-shell-switcher.html', pageSwitcher(), 'Shell + workspace switcher', 'Vault-style menu in the header, and the restart-to-switch-MotherDuck prompt.', '#111'],
  ['05-connections-rail.html', pageRail(), 'Connections rail', 'Explorer tree, status dots, scope badges, shadowing, context menu, empty states.', '#117'],
  ['06-connection-editor.html', pageEditor(), 'Connection editor', 'Type picker, generated form, write-only credentials, Test, scope, read-only.', '#117'],
  ['07-states.html', pageStates(), 'States', 'Needs credentials, extension consent, opened read-only, conflict banner, read-only datasheet.', '#111 #112 #117'],
  ['08-query-editor-cells.html', pageQuery(), 'Query editor + SQL cells', 'Connection selector in the editor and per notebook cell.', '#117'],
  ['09-settings.html', pageSettings(), 'Settings split', 'Workspace vs App.', '#111 #117'],
  ['10-agent-approval.html', pageAgent(), 'Agent approval card', 'A write to a non-local connection names the connection.', '#116 #117'],
]
for (const [f, html] of pages) writeFileSync(join(here, f), html)

const cards = pages.map(([f, , t, d, issues], i) => `
  <a class="card gcard" href="${f}" data-keep-theme>
    <div class="thumb" aria-hidden="true"><iframe data-src="${f}" src="${f}" tabindex="-1" title="${t}" loading="lazy" style="transform:scale(0.3)"></iframe></div>
    <div class="gt"><span class="n">${String(i + 1).padStart(2, '0')} &middot; ${issues}</span><span class="h">${t}</span><span class="d">${d}</span></div>
  </a>`).join('')

writeFileSync(join(here, 'index.html'), `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Workspaces and connections - Vibe Studio design mockups</title>
<link rel="stylesheet" href="mock.css"><script src="mock.js"></script>
<style>.gcard .thumb iframe{transform:scale(var(--s,0.3))} .intro{max-width:760px;padding:24px 20px 8px} .intro h1{font-size:34px;margin:0 0 8px} .intro p{color:var(--sh-graphite);font-size:15px;line-height:1.6;margin:0 0 8px}</style>
<script>
// scale the thumbnail iframes to their card width
addEventListener('DOMContentLoaded',function(){function f(){document.querySelectorAll('.gcard .thumb').forEach(function(t){t.querySelector('iframe').style.transform='scale('+(t.clientWidth/1280)+')'})};f();addEventListener('resize',f)})
</script></head>
<body class="sh">
<header class="doc-bar"><span class="ttl">Vibe Studio &middot; workspaces and connections</span><span class="tag">Design pass #106 &middot; epic #105</span><span class="sp"></span>
<a href="SPEC.md">Read the spec (SPEC.md)</a>
<button class="tbtn" id="theme-toggle" type="button" aria-label="Toggle light and dark theme">${icon('moon', 14)}<span id="theme-label">Light</span></button></header>
<div style="max-width:1280px;margin:0 auto">
<div class="intro"><h1>Workspaces and connections</h1>
<p>Static, high-fidelity mockups of the new Studio surfaces, built from the real SchoolHouse stylesheets and the <code>--vibe-*</code> theme roles. Use the Light/Dark button to flip every page. These are documents, not shipped code.</p>
<p>Each page shows several states. The copy on them is the proposed final wording; the spec lists the Go bindings, events and reusable components for each.</p></div>
<div class="gallery" style="padding-bottom:48px">${cards}</div></div>
</body></html>`)
console.log('wrote', pages.length, 'pages + index.html')
