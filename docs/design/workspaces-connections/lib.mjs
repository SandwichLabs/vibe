// Shared building blocks for the #106 mockup generator (build.mjs). Docs tooling only.
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const iconsTs = readFileSync(join(here, '../../../studio/frontend/src/icons.ts'), 'utf8')

// Real Studio icons (icons.ts) + a few extra Lucide glyphs this design needs.
export const ICONS = {}
for (const m of iconsTs.matchAll(/^\s{2}(\w+):\s*'([^']*)',?$/gm)) ICONS[m[1]] = m[2]
Object.assign(ICONS, {
  gitBranch: '<line x1="6" x2="6" y1="3" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>',
  folderOpen: '<path d="m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2"/>',
  circle: '<circle cx="12" cy="12" r="9"/>',
  circleDot: '<circle cx="12" cy="12" r="10" fill="currentColor" stroke="none"/>',
  circleAlert: '<circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/>',
  circleCheck: '<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>',
  shieldCheck: '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
  plug: '<path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"/>',
  server: '<rect width="20" height="8" x="2" y="2" rx="2"/><rect width="20" height="8" x="2" y="14" rx="2"/><line x1="6" x2="6.01" y1="6" y2="6"/><line x1="6" x2="6.01" y1="18" y2="18"/>',
  more: '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
  chevUpDown: '<path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/>',
  externalLink: '<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>',
  clock: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
  unlock: '<rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/>',
  pencilLine: '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
  fileCode: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="m10 13-2 2 2 2"/><path d="m14 17 2-2-2-2"/>',
  eyeOff: '<path d="M9.88 9.88a3 3 0 1 0 4.24 4.24"/><path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68"/><path d="M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61"/><line x1="2" x2="22" y1="2" y2="22"/>',
  history: '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
  sheet: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M3 15h18M9 3v18"/>',
})

export const icon = (name, size = 16, extra = '') =>
  `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" ${extra}>${ICONS[name] ?? ''}</svg>`

// Connection type tiles (two-letter, brand-neutral).
export const TYPES = {
  duckdb: { code: 'DK', label: 'DuckDB file' },
  postgres: { code: 'PG', label: 'Postgres' },
  motherduck: { code: 'MD', label: 'MotherDuck' },
  sqlite: { code: 'SQ', label: 'SQLite file' },
  s3: { code: 'S3', label: 'S3 / GCS / R2' },
  mysql: { code: 'MY', label: 'MySQL' },
  ducklake: { code: 'DL', label: 'DuckLake' },
}
export const ctype = (t, cls = '') => `<span class="ctype ${cls}" aria-hidden="true">${TYPES[t].code}</span>`

// Status icon: shape AND colour AND an accessible name, never colour alone.
export const STATUS = {
  off: { icon: 'circle', label: 'Not connected' },
  ok: { icon: 'circleDot', label: 'Connected' },
  err: { icon: 'circleAlert', label: 'Can’t connect' },
  creds: { icon: 'key', label: 'Needs credentials' },
  ext: { icon: 'download', label: 'Extension needed' },
  lock: { icon: 'users', label: 'In use by another process' },
}
export const st = (kind, label) =>
  `<span class="st ${kind}" role="img" aria-label="${label ?? STATUS[kind].label}" title="${label ?? STATUS[kind].label}">${icon(STATUS[kind].icon, 14)}</span>`
export const SCOPE_LABEL = { user: 'All my workspaces', shared: 'Team', local: 'This computer' }
export const scope = (s) => `<span class="scope ${s}" title="${{ user: 'Saved for you, in every workspace', shared: 'Saved in vibe.yaml, shared with the team through git', local: 'Saved on this computer only, for this workspace' }[s]}">${SCOPE_LABEL[s]}</span>`

// Several artboards share one document: suffix ids / radio names so labels and groups stay separate.
let ab = 0
const uniq = (html) => {
  const n = ++ab
  return html
    .replace(/\b(id|for|aria-labelledby|aria-describedby|aria-controls)="([^"]+)"/g, (m, a, v) => `${a}="${v.split(' ').map((x) => x + '-' + n).join(' ')}"`)
    .replace(/\bname="([\w-]+)"/g, (m, v) => `name="${v}-${n}"`)
}

// ---- page chrome ---------------------------------------------------------
export function page({ file, num, title, blurb, artboards, extraHead = '' }) {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${num} ${title} - Vibe Studio design mockups</title>
<link rel="stylesheet" href="mock.css">
<script src="mock.js"></script>
${extraHead}
</head>
<body class="sh">
<header class="doc-bar">
  <a href="index.html" data-keep-theme>&larr; All screens</a>
  <span class="ttl">${num} &middot; ${title}</span>
  <span class="tag">Epic #105 &middot; Design pass #106 &middot; static mockup, not shipped code</span>
  <span class="sp"></span>
  <button class="tbtn" id="theme-toggle" type="button" aria-label="Toggle light and dark theme">${icon('moon', 14)}<span id="theme-label">Light</span></button>
</header>
<div class="stage">
${artboards.map(uniq).join('\n')}
</div>
</body>
</html>
`
}

export const artboard = (label, note, inner, cls = '') => `
<section class="artboard">
  <div class="ab-label"><b>${label}</b><span class="note-t">${note ?? ''}</span></div>
  <div class="frame ${cls}">
${inner}
  </div>
</section>`

// ---- shell ---------------------------------------------------------------
export const NAV = [
  { id: 'home', label: 'Home', icon: 'home' },
  { id: 'notebooks', label: 'Notebooks', icon: 'layers' },
  { id: 'queries', label: 'Queries', icon: 'terminal' },
  { id: 'skills', label: 'Skills', icon: 'bookOpen' },
  { id: 'agent', label: 'Agent', icon: 'bot' },
  { id: 'import', label: 'Import', icon: 'download' },
  { id: 'settings', label: 'Settings', icon: 'settings' },
]

export function topbar({ ws = 'Acme Analytics', branch = 'main', switcherOpen = false, pill = 'Agent asks before changes', pillDot = 'ok', agent = false } = {}) {
  return `
<div class="topbar">
  <div class="brand"><span class="mark mark-only" aria-hidden="true">V</span></div>
  <button class="ws-switch" type="button" aria-haspopup="menu" aria-expanded="${switcherOpen}" aria-label="Workspace: ${ws}. Switch workspace">
    ${icon('folderOpen')}<span class="ws-name">${ws}</span>${branch ? `<span class="ws-branch">${icon('gitBranch')}${branch}</span>` : ''}${icon('chevUpDown')}
  </button>
  <div class="topright" style="gap:10px;">
    <span class="pill"><span class="dot ${pillDot}"></span>${pill}</span>
    <button class="btn btn-inverted" type="button" aria-expanded="${agent}">${icon('bot')} Agent</button>
    <button class="icon-btn" type="button" style="color:var(--vibe-chrome-fg);border-color:var(--vibe-chrome-fg);width:32px;height:32px;" aria-label="Switch to dark theme">${icon('moon')}</button>
  </div>
</div>`
}

// tree node: depth, kind etc. -------------------------------------------------
export function tn({ focus = false, d = 0, chev = null, ico = null, tile = null, label, meta = '', metaCls = '', sel = false, conn = false, tail = '', level, expanded, title = '' }) {
  const pad = 4 + d * 14
  const chevHtml = chev ? icon(chev === 'open' ? 'chevronDown' : 'chevronRight', 14, 'class="chev"') : `<span class="chev none"></span>`
  const aria = `role="treeitem" aria-level="${level ?? d + 1}"${chev ? ` aria-expanded="${chev === 'open'}"` : ''}${sel ? ' aria-selected="true"' : ''}`
  return `<div class="tn${conn ? ' conn' : ''}${sel ? ' sel' : ''}${focus ? ' kf' : ''}" ${aria} style="padding-left:${pad}px"${title ? ` title="${title}"` : ''}>${chevHtml}${tile ?? (ico ? icon(ico, 14, 'class="ico"') : '')}<span class="nm">${label}</span>${meta ? `<span class="meta ${metaCls}">${meta}</span>` : ''}${tail ? `<span class="rowtail">${tail}</span>` : ''}</div>`
}

export const connRow = ({ name, type, scopeName, status, chev, sel, ro = false, shadow = false, statusLabel }) =>
  tn({
    d: 0, chev, tile: ctype(type), label: name, conn: true, sel,
    tail: `${ro ? icon('lock', 12, 'class="ro" aria-label="Read-only" role="img"') : ''}${shadow ? icon('layers', 12, 'class="ro" role="img" aria-label="Overrides a shared connection"') : ''}${scope(scopeName)}${st(status, statusLabel)}`,
    title: name,
  })

export function sidebarWithRail({ active = 'home', tree, treeAttrs = '', footer = true, branchLine = '2 changed files', railHead = true }) {
  return `
<aside class="sidebar with-rail">
  <nav class="navblock" aria-label="Workspace">
    <div class="side-section">Workspace</div>
    ${NAV.map((n) => `<a class="side-link${n.id === active ? ' active' : ''}" href="#" ${n.id === active ? 'aria-current="page"' : ''}>${icon(n.icon, 18)}<span>${n.label}</span></a>`).join('\n    ')}
  </nav>
  <div class="conn-rail" role="region" aria-label="Connections">
    <div class="rail-head">
      <div class="side-section" id="rail-h">Connections</div>
      <button class="icon-btn rail-toggle" type="button" aria-label="Show connections" aria-expanded="false" title="Connections">${icon('plug')}</button>
      <button class="icon-btn keep" type="button" aria-label="New connection" title="New connection">${icon('plus')}</button>
      <button class="icon-btn" type="button" aria-label="Refresh all connections" title="Refresh">${icon('refresh')}</button>
    </div>
    <div class="rail-filter"><label class="sr" style="position:absolute;left:-9999px" for="rf">Filter tables</label>${icon('search')}<input id="rf" type="text" placeholder="Filter tables and columns" aria-label="Filter tables and columns"></div>
    <div class="tree" role="tree" aria-labelledby="rail-h" ${treeAttrs}>
${tree}
    </div>
  </div>
  ${footer ? `<div class="nav-pill" style="margin:10px 12px 12px;flex:none;">
    <span class="label"><span class="dot"></span>Saved as you go</span>
    <span class="url">~/work/<span class="acc">acme-analytics</span></span>
    <span class="git">${icon('gitBranch', 12)} main &middot; ${branchLine}</span>
  </div>` : ''}
</aside>`
}

export function shell({ active = 'home', tree, main, banner = '', menu = '', topOpts = {}, treeAttrs = '', peek = '', overlay = '', branchLine }) {
  return `
<div class="app has-rail" ${peek ? `data-peek="${peek}"` : ''}>
${topbar(topOpts)}
${sidebarWithRail({ active, tree, treeAttrs, branchLine })}
<main>${banner}${main}</main>
${menu}
${overlay}
</div>`
}

// ---- the default rail tree, used on several pages -------------------------
export function treeFull({ sel = '', focus = '', status = {}, drop = [] } = {}) {
  const S = (n, d) => status[n] ?? d
  const f = (n) => (focus === n ? ' kf' : '')
  const rows = [
    [connRow({ name: 'local', type: 'duckdb', scopeName: 'shared', status: S('local', 'ok'), chev: 'open', sel: sel === 'local' }), 'local'],
    // Only `main` exists, so the schema level is hidden: tables sit right under the connection.
    [tn({ d: 1, ico: 'tableIcon', label: 'customers', meta: '12,480' }), 'local'],
    [tn({ d: 1, ico: 'tableIcon', label: 'orders', meta: '48,213', sel: sel === 'orders', focus: focus === 'orders' }), 'local'],
    [tn({ d: 1, ico: 'eye', label: 'v_monthly_revenue', meta: 'view' }), 'local'],
    [tn({ d: 1, ico: 'tableIcon', label: 'products', meta: '1,206' }), 'local'],
    [connRow({ name: 'prod_pg', type: 'postgres', scopeName: 'shared', status: S('prod_pg', 'ok'), chev: 'closed', ro: true, sel: sel === 'prod_pg', statusLabel: status.prod_pg_label }), 'prod_pg'],
    [connRow({ name: 'warehouse', type: 'postgres', scopeName: 'local', status: S('warehouse', 'creds'), chev: 'closed', shadow: true, statusLabel: 'Needs credentials: PG_WAREHOUSE_PASSWORD is not set' }), 'warehouse'],
    [connRow({ name: 'md', type: 'motherduck', scopeName: 'user', status: S('md', 'creds'), chev: 'closed', ro: true }), 'md'],
    [connRow({ name: 'lake', type: 's3', scopeName: 'shared', status: S('lake', 'ext'), chev: null, ro: true }), 'lake'],
    [connRow({ name: 'staging_pg', type: 'postgres', scopeName: 'local', status: S('staging_pg', 'err'), chev: 'closed', ro: true }), 'staging_pg'],
    [connRow({ name: 'ops_db', type: 'duckdb', scopeName: 'shared', status: S('ops_db', 'lock'), chev: null, statusLabel: 'In use by vibe mcp serve (pid 4182)' }), 'ops_db'],
    [connRow({ name: 'legacy', type: 'sqlite', scopeName: 'local', status: S('legacy', 'off'), chev: 'closed', ro: true }), 'legacy'],
  ]
  return rows.filter(([, k]) => !drop.includes(k)).map(([h]) => h).join('\n')
}
export const TREE_FULL = treeFull()

export const datasheet = ({ cols, rows, ro = false }) => `
<table class="brutal-table" style="background:var(--sh-surface);">
  <thead><tr>${cols.map((c) => `<th class="${c.num ? 'cell-num' : ''}">${c.n}${c.pk ? ' <span style="color:var(--sh-warn)">PK</span>' : ''}</th>`).join('')}</tr></thead>
  <tbody>${rows.map((r) => `<tr>${r.map((v, i) => `<td class="${cols[i].num ? 'cell-num' : ''}${v === null ? ' cell-null' : ''}"${ro ? ' aria-readonly="true"' : ''}>${v === null ? 'NULL' : v}</td>`).join('')}</tr>`).join('')}</tbody>
</table>`

export const ORDERS = {
  cols: [{ n: 'id', num: true, pk: true }, { n: 'customer_id', num: true }, { n: 'status' }, { n: 'ordered_at' }, { n: 'total', num: true }],
  rows: [
    ['10482', '2217', 'pending', '2026-10-05 14:12', '184.50'],
    ['10481', '1904', 'shipped', '2026-10-05 11:40', '62.00'],
    ['10480', '2217', 'shipped', '2026-10-04 16:03', '1,240.75'],
    ['10479', '3310', 'cancelled', '2026-10-04 09:51', '38.20'],
    ['10478', '1120', 'shipped', '2026-10-03 17:25', '412.00'],
    ['10477', '2988', 'pending', '2026-10-03 13:08', null],
    ['10476', '1904', 'shipped', '2026-10-02 10:30', '96.10'],
    ['10475', '1556', 'shipped', '2026-10-02 08:44', '27.95'],
  ],
}
