#!/usr/bin/env node
// Generates THIRD_PARTY_NOTICES.md at the repo root (`task notices`).
//
//   node scripts/gen-notices.mjs            write THIRD_PARTY_NOTICES.md
//   node scripts/gen-notices.mjs --check    regenerate in memory; exit 1 when the file on disk differs
//
// Sources, all deterministic (sorted, no timestamps):
//   - Go: go-licenses v2 over ./cmd/vibe and ./studio for every release platform (linux and darwin,
//     amd64 and arm64), merged. Licence texts are grouped when identical.
//   - npm: every non-dev package in studio/frontend/package-lock.json, text from node_modules
//     (run `npm ci` in studio/frontend first).
//   - DuckDB's bundled components, fonts and icons: committed texts (scripts/notices/, the OFL files
//     beside the fonts).
// Needs network access the first time (go install of the pinned go-licenses) and `npm ci` in studio/frontend.
// It exits non-zero when a licence is not on the allow-list of permissive licences, so a new
// dependency with an unexpected licence fails CI instead of shipping unnoticed.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const GO_LICENSES = 'github.com/google/go-licenses/v2@v2.0.1'
const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const OUT = join(root, 'THIRD_PARTY_NOTICES.md')

// SPDX identifiers that may ship. MPL-2.0 is weak copyleft at file level: allowed because we ship
// unmodified upstream source files only, and the notices say so.
const ALLOWED = new Set([
  'MIT', 'BSD-2-Clause', 'BSD-3-Clause', 'Apache-2.0', 'ISC', 'MPL-2.0', 'OFL-1.1', '0BSD',
  'Unlicense', 'CC0-1.0', 'BlueOak-1.0.0',
])
const PLATFORMS = [['linux', 'amd64'], ['linux', 'arm64'], ['darwin', 'arm64'], ['darwin', 'amd64']]

const problems = []
let goNotices = []
const npmNotices = []
const sh = (cmd, args, opts = {}) => execFileSync(cmd, args, { encoding: 'utf8', maxBuffer: 1 << 28, ...opts })

// ---------------------------------------------------------------- Go
// The CLI and Studio are built with different tags (wails adds desktop,production; Linux adds webkit2_41),
// and tags change which packages, hence modules, get linked. Each target is reported with its release tags.
const TARGETS = (goos) => [
  { pkg: './cmd/vibe', tags: 'duckdb_arrow' },
  { pkg: './studio', tags: `desktop,production,duckdb_arrow${goos === 'linux' ? ',webkit2_41' : ''}` },
]

// The module cache escapes upper-case letters as "!x".
const unescapeMod = (p) => p.replace(/!([a-z])/g, (_, c) => c.toUpperCase())

function goEntries() {
  const bin = mkdtempSync(join(tmpdir(), 'gl-'))
  try {
    sh('go', ['install', GO_LICENSES], { env: { ...process.env, GOBIN: bin }, cwd: root, stdio: ['ignore', 'pipe', 'inherit'] })
    const modcache = sh('go', ['env', 'GOMODCACHE'], { cwd: root }).trim()
    const self = sh('go', ['list', '-m'], { cwd: root }).trim()
    const isSelf = (m) => m === self || m.startsWith(`${self}/`)
    const tpl = join(bin, 'tpl.txt')
    writeFileSync(tpl, '{{range .}}{{.Name}}\t{{.Version}}\t{{.LicenseName}}\t{{.LicensePath}}\n{{end}}')
    const seen = new Map()
    const notices = new Map()
    for (const [goos, goarch] of PLATFORMS) {
      for (const { pkg: target, tags } of TARGETS(goos)) {
        const env = { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '1', GOFLAGS: `-tags=${tags}` }
        const out = sh(join(bin, 'go-licenses'), ['report', target, '--template', tpl], { cwd: root, env, stdio: ['ignore', 'pipe', 'ignore'] })
        const listed = new Set()
        for (const line of out.split('\n')) {
          const [pkg, , license, path] = line.split('\t')
          if (!pkg) continue
          if (!path || !path.startsWith(modcache)) {
            if (!isSelf(pkg)) problems.push(`Go ${pkg} (${goos}/${goarch}): no licence file found (${path || 'none'})`)
            continue
          }
          const rel = path.slice(modcache.length + 1)
          const at = rel.indexOf('@')
          const slash = rel.indexOf('/', at)
          const module = unescapeMod(rel.slice(0, at))
          const version = rel.slice(at + 1, slash)
          if (isSelf(module)) continue
          listed.add(module)
          const key = `${module}@${version}\t${rel.slice(slash + 1)}`
          if (!seen.has(key)) seen.set(key, { name: module, version, license, text: readFileSync(path, 'utf8') })
          if (!license || !ALLOWED.has(license)) problems.push(`Go ${module}@${version}: licence "${license || 'unknown'}" is not on the allow-list`)
          // Apache-2.0 section 4(d): reproduce the NOTICE file of each module that has one.
          const mdir = join(modcache, rel.slice(0, slash))
          if (!notices.has(`${module}@${version}`) && existsSync(mdir)) {
            for (const f of readdirSync(mdir).filter((n) => /^notice/i.test(n)).sort()) {
              notices.set(`${module}@${version}\t${f}`, { name: module, version, license: 'NOTICE', text: readFileSync(join(mdir, f), 'utf8') })
            }
            notices.set(`${module}@${version}`, null)
          }
        }
        // Cross-check: every module go itself says is linked must have been reported.
        const deps = sh('go', ['list', '-deps', '-f', '{{if .Module}}{{.Module.Path}}{{end}}', target], { cwd: root, env })
        for (const m of new Set(deps.split('\n').filter(Boolean))) {
          if (!isSelf(m) && !listed.has(m)) problems.push(`Go module ${m} is linked into ${target} (${goos}/${goarch}) but go-licenses did not report it`)
        }
      }
    }
    for (const [k, v] of [...notices]) if (v === null) notices.delete(k)
    goNotices = [...notices.values()]
    return [...seen.values()]
  } finally {
    rmSync(bin, { recursive: true, force: true })
  }
}

// ---------------------------------------------------------------- npm
// A package.json licence may be an SPDX expression: "(MPL-2.0 OR Apache-2.0)" is satisfied by electing
// the first allowed alternative, preferring one that is not MPL-2.0. Returns it, or '' when none is allowed.
function electLicense(expr) {
  const alts = expr.replace(/[()]/g, '').split(/\s+OR\s+/).map((a) => a.trim()).sort((a, b) => (a === 'MPL-2.0') - (b === 'MPL-2.0'))
  for (const alt of alts) {
    if (alt.split(/\s+AND\s+/).every((p) => ALLOWED.has(p.trim()))) return alt
  }
  return ''
}

const BUNDLED_BUILD_TOOLS = [
  ['tailwindcss', 'preflight CSS is part of the bundled stylesheet'],
  ['vite', 'module-preload helper is part of the bundled JavaScript'],
]
const elected = (expr) => typeof expr === 'string' && electLicense(expr) !== ''

function npmEntries() {
  const fe = join(root, 'studio/frontend')
  const lock = JSON.parse(readFileSync(join(fe, 'package-lock.json'), 'utf8'))
  const out = []
  for (const [path, meta] of Object.entries(lock.packages)) {
    if (!path || meta.dev) continue
    const dir = join(fe, path)
    if (!existsSync(dir)) {
      problems.push(`npm ${path}: not installed (run npm ci in studio/frontend)`)
      continue
    }
    const pkg = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
    const declared = typeof pkg.license === 'string' ? pkg.license : pkg.license?.type || meta.license || ''
    const elected = electLicense(declared)
    if (!elected) problems.push(`npm ${pkg.name}@${pkg.version}: licence "${declared || 'unknown'}" is not on the allow-list`)
    const files = readdirSync(dir).filter((f) => /^(licen[cs]e|copying)/i.test(f)).sort()
    let text = files.map((f) => readFileSync(join(dir, f), 'utf8').trim()).join('\n\n----\n\n')
    if (!text) {
      const who = typeof pkg.author === 'string' ? pkg.author : pkg.author?.name || 'the package authors'
      text = `The package ships no licence file. package.json declares the ${declared} licence (author: ${who}).`
    }
    out.push({ name: pkg.name, version: pkg.version, license: elected && elected !== declared ? `${elected} (elected from ${declared})` : declared, text })
    for (const f of readdirSync(dir).filter((n) => /^notice/i.test(n)).sort()) {
      npmNotices.push({ name: pkg.name, version: pkg.version, license: 'NOTICE', text: readFileSync(join(dir, f), 'utf8') })
    }
  }
  // Build tools whose code is copied into dist: Tailwind's preflight CSS and Vite's module-preload helper.
  // They are dev dependencies, so the lock walk above skips them; list them explicitly.
  for (const [name, note] of BUNDLED_BUILD_TOOLS) {
    const dir = join(fe, 'node_modules', name)
    const file = readdirSync(dir).find((f) => /^licen[cs]e/i.test(f))
    const pkg = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
    if (!file || !elected(pkg.license)) problems.push(`npm ${name}: licence missing or not allowed`)
    out.push({ name: `${pkg.name} (${note})`, version: pkg.version, license: pkg.license, text: readFileSync(join(dir, file), 'utf8') })
  }
  return out
}

// ---------------------------------------------------------------- rendering
const fence = (text) => {
  if (text.includes('```')) problems.push('a licence text contains a code fence; extend gen-notices.mjs')
  return '```text\n' + text.trim().replace(/[ \t]+$/gm, '') + '\n```\n'
}
const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0)

// renderGroups lists modules under one licence text each (identical texts share a block).
function renderGroups(entries) {
  const groups = new Map()
  for (const e of entries) {
    const norm = e.text.replace(/\s+/g, ' ').trim()
    const key = `${e.license}\0${createHash('sha256').update(norm).digest('hex')}`
    if (!groups.has(key)) groups.set(key, { license: e.license, text: e.text, members: [] })
    groups.get(key).members.push(e)
  }
  const list = [...groups.values()]
  for (const g of list) g.members.sort((a, b) => cmp(a.name, b.name) || cmp(a.version, b.version))
  list.sort((a, b) => cmp(a.license, b.license) || cmp(a.members[0].name, b.members[0].name))
  return list
    .map((g) => {
      const names = g.members.map((m) => `- ${m.name} ${m.version}`).join('\n')
      return `#### ${g.license}\n\n${names}\n\n${fence(g.text)}`
    })
    .join('\n')
}

// renderNotices prints each distinct NOTICE text once, naming the modules that carry it.
function renderNotices(entries) {
  if (!entries.length) return '(none)\n'
  const groups = new Map()
  for (const e of entries) {
    const t = e.text.trim()
    if (!groups.has(t)) groups.set(t, [])
    groups.get(t).push(`${e.name} ${e.version}`)
  }
  return [...groups]
    .map(([t, who]) => [[...new Set(who)].sort(), t])
    .sort((a, b) => cmp(a[0][0], b[0][0]))
    .map(([who, t]) => `#### ${who.join(', ')}\n\n${fence(t)}`)
    .join('\n')
}

function summary(entries) {
  const counts = new Map()
  for (const e of entries) counts.set(e.license, (counts.get(e.license) || 0) + 1)
  return [...counts].sort((a, b) => cmp(a[0], b[0])).map(([l, n]) => `${l} ${n}`).join(', ')
}

function fontBlocks() {
  const dir = join(root, 'studio/frontend/src/assets/fonts')
  const files = readdirSync(dir).filter((f) => /^OFL-.+\.txt$/.test(f)).sort()
  if (files.length !== 3) problems.push(`expected 3 OFL-*.txt beside the fonts, found ${files.length}`)
  return files.map((f) => `#### ${f.slice(4, -4)} (OFL-1.1)\n\n${fence(readFileSync(join(dir, f), 'utf8'))}`).join('\n')
}

function build() {
  const go = goEntries()
  const npm = npmEntries()
  const duckdb = readFileSync(join(root, 'scripts/notices/duckdb-third-party.md'), 'utf8').trim().replace(/^# .*\n/, '')
  const lucide = readFileSync(join(root, 'scripts/notices/lucide-LICENSE.txt'), 'utf8')
  const hasMPL = [...go, ...npm].some((e) => e.license === 'MPL-2.0')
  return `# Third-party notices

Vibe is licensed under the Apache License 2.0 (see LICENSE and NOTICE). It includes and links the
third-party software below, each under its own licence. This file is generated by \`task notices\`
(scripts/gen-notices.mjs); do not edit it by hand.

Contents: [Go modules](#go-modules) · [Frontend packages](#frontend-packages) · [DuckDB and its bundled components](#duckdb-and-its-bundled-components) · [Fonts](#fonts) · [Icons](#icons) · [Not redistributed](#not-redistributed)

## Go modules

Every module linked into the \`vibe\` CLI or Vibe Studio on linux and macOS (amd64 and arm64), found with go-licenses v2.
${go.length} module/licence-file pairs: ${summary(go)}.
${hasMPL ? '\nMPL-2.0 applies at file level: the covered files are unmodified upstream sources, and their source is available from the module path named below.\n' : ''}
${renderGroups(go)}
## Frontend packages

The production dependencies in studio/frontend/package-lock.json (build-time tools are not listed). Only some end up in the
bundle; the list is the conservative superset. ${npm.length} packages: ${summary(npm)}.
Where a package offers a choice of licences (dompurify: MPL-2.0 OR Apache-2.0) Vibe elects the permissive alternative named below.

${renderGroups(npm)}
## NOTICE files

Apache-2.0 section 4(d) asks that the NOTICE file of an Apache-licensed work be reproduced. These are the NOTICE files
shipped by the Go modules and npm packages above.

${renderNotices([...goNotices, ...npmNotices])}
## DuckDB and its bundled components

DuckDB is MIT licensed (Copyright Stichting DuckDB Foundation). The database engine is linked statically, together
with the components below; each applies to its own code.

${duckdb}

## Fonts

Fraunces, Inter and JetBrains Mono are bundled so the app renders the same offline. All three are licensed under the
SIL Open Font License 1.1. They are subsets (latin and latin-ext) of the upstream fonts, otherwise unmodified, and are
not sold on their own.

${fontBlocks()}
## Icons

Studio's icons (src/icons.ts) are Lucide-style SVG paths; some derive from Lucide (ISC) and Feather (MIT).

${fence(lucide)}
## Not redistributed

- The DuckDB \`motherduck\` extension is proprietary software of MotherDuck. Vibe does not ship it. It is downloaded at
  run time, only after the person agrees in the extension consent dialog, and is governed by MotherDuck's terms.
- Every other DuckDB extension (postgres, mysql, httpfs, azure, ducklake, iceberg, delta, and so on) is likewise
  downloaded at run time on consent, from DuckDB's extension repository. None is bundled in the archives.
- WebKitGTK (LGPL-2.1, Linux) and WebKit (macOS) are system libraries that Studio links dynamically. They are not
  redistributed with Vibe.
`
}

const text = build()
if (problems.length) {
  console.error(`gen-notices: ${problems.length} problem(s):\n  - ${[...new Set(problems)].sort().join('\n  - ')}`)
  process.exit(1)
}
if (process.argv.includes('--check')) {
  const disk = existsSync(OUT) ? readFileSync(OUT, 'utf8') : ''
  if (disk !== text) {
    console.error('gen-notices: THIRD_PARTY_NOTICES.md is stale. Run `task notices` and commit the result.')
    process.exit(1)
  }
  console.log('THIRD_PARTY_NOTICES.md is up to date')
} else {
  writeFileSync(OUT, text)
  console.log(`wrote ${OUT} (${text.length} bytes)`)
}
