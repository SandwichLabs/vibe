// Guard rail for the thin-views rule (#54 slice 9b, F10 / O9): a view should
// wire composables and render, not hold the logic.
//   - no .vue file under src/ may exceed MAX_LINES lines in total;
//   - only the files named in SCRIPT_ALLOW may have more than MAX_SCRIPT_LINES
//     lines between <script> and </script>, and each may not grow past its cap
//     (shrink the cap when the file shrinks).
// Usage: node scripts/check-vue-size.mjs   (run from studio/frontend)
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const MAX_LINES = 600
const MAX_SCRIPT_LINES = 150

// path (relative to studio/frontend) -> [cap, reason]
const SCRIPT_ALLOW = {}

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'wailsjs') continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

// scriptLines counts the lines strictly inside every <script> block.
function scriptLines(text) {
  let n = 0
  let inside = false
  for (const line of text.split('\n')) {
    if (!inside && /^<script\b/.test(line)) { inside = true; continue }
    if (inside && /^<\/script>/.test(line)) { inside = false; continue }
    if (inside) n++
  }
  return n
}

const root = process.cwd()
const failures = []
const seenAllowed = new Set()
const report = []
for (const file of walk(join(root, 'src'))) {
  const rel = relative(root, file)
  const text = readFileSync(file, 'utf8')
  const total = text.split('\n').length - (text.endsWith('\n') ? 1 : 0)
  const script = scriptLines(text)
  if (total > MAX_LINES) failures.push(`${rel}: ${total} lines (limit ${MAX_LINES}): split it into components and composables`)
  if (script > MAX_SCRIPT_LINES) {
    const allow = SCRIPT_ALLOW[rel]
    if (!allow) failures.push(`${rel}: ${script} script lines (limit ${MAX_SCRIPT_LINES}): move the logic into a composable, or name it in SCRIPT_ALLOW with a reason`)
    else {
      seenAllowed.add(rel)
      if (script > allow[0]) failures.push(`${rel}: ${script} script lines, past its allowance of ${allow[0]}: move logic out rather than raise it`)
      report.push(`${rel}: ${script}/${allow[0]} script lines (${allow[1]})`)
    }
  }
}
for (const rel of Object.keys(SCRIPT_ALLOW)) {
  if (!seenAllowed.has(rel)) failures.push(`${rel}: is in SCRIPT_ALLOW but is now within ${MAX_SCRIPT_LINES} script lines (or gone): remove it from the list`)
}
if (Object.keys(SCRIPT_ALLOW).length > 3) failures.push('SCRIPT_ALLOW names more than 3 views')

if (failures.length) {
  console.error('check-vue-size failed:\n  ' + failures.join('\n  '))
  process.exit(1)
}
for (const r of report) console.log(r)
console.log('check-vue-size: ok')
