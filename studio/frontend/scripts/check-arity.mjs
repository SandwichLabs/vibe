// Compares each method's parameter count in the wails-generated
// src/wailsjs/go/main/App.d.ts with AppApi in src/env.d.ts, so a Go signature
// change that env.d.ts missed fails CI. Run after `wails build` regenerates the
// generated file. Usage: node scripts/check-arity.mjs [generated App.d.ts] [env.d.ts]
import { readFileSync } from 'node:fs'

const genPath = process.argv[2] ?? 'src/wailsjs/go/main/App.d.ts'
const envPath = process.argv[3] ?? 'src/env.d.ts'

function params(s) {
  let depth = 0
  let cur = ''
  const out = []
  for (const ch of s) {
    if ('([{<'.includes(ch)) depth++
    if (')]}>'.includes(ch)) depth--
    if (ch === ',' && depth === 0) {
      out.push(cur.trim())
      cur = ''
    } else cur += ch
  }
  if (cur.trim()) out.push(cur.trim())
  return out
}

function sigs(text, rx) {
  const res = new Map()
  for (const m of text.matchAll(rx)) {
    let depth = 1
    let j = m.index + m[0].length
    while (depth && j < text.length) {
      if (text[j] === '(') depth++
      if (text[j] === ')') depth--
      j++
    }
    res.set(m[1], params(text.slice(m.index + m[0].length, j - 1)).length)
  }
  return res
}

const gen = sigs(readFileSync(genPath, 'utf8'), /^export function (\w+)\(/gm)
const env = readFileSync(envPath, 'utf8')
const start = env.indexOf('interface AppApi {')
if (start < 0) throw new Error(`no AppApi interface in ${envPath}`)
const end = env.indexOf('\n}\n', start)
const api = sigs(env.slice(start, end), /^ {2}(\w+)\(/gm)

let bad = 0
for (const k of [...new Set([...gen.keys(), ...api.keys()])].sort()) {
  if (!gen.has(k) || !api.has(k)) {
    console.log(`${k}: in generated=${gen.has(k)}, in AppApi=${api.has(k)}`)
    bad++
  } else if (gen.get(k) !== api.get(k)) {
    console.log(`${k}: generated takes ${gen.get(k)} parameter(s), AppApi declares ${api.get(k)}`)
    bad++
  }
}
if (gen.size === 0) {
  console.log(`no methods found in ${genPath}`)
  bad++
}
console.log(`check-arity: ${bad ? 'FAILED' : 'ok'} (${gen.size} generated, ${api.size} declared)`)
process.exit(bad ? 1 : 0)
