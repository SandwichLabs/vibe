// SPIKE #257 - throwaway. Raw and gzip(9) bytes per emitted file of a `npm run build`, spike chunks flagged,
// plus the total of assets/*.js with and without them (the lazy imports make the split exact).
//   node spike/console/bundle-size.mjs studio/frontend/dist
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const dist = process.argv[2] ?? 'studio/frontend/dist'
const dir = join(dist, 'assets')
const spike = /^(ghostty-web|xterm|addon-fit|addon-webgl|ConsoleSpikeView|ghostty-vt|__vite-browser-external)/
const rows = readdirSync(dir).filter((f) => statSync(join(dir, f)).isFile()).map((f) => {
  const buf = readFileSync(join(dir, f))
  return { f, raw: buf.length, gz: gzipSync(buf, { level: 9 }).length, spike: spike.test(f) }
})
const fmt = (n) => String(n).padStart(9)
console.log('file'.padEnd(52), '      raw', '  gzip(9)', ' spike')
for (const r of rows.filter((r) => r.spike || /\.(js)$/.test(r.f) && r.raw > 100000)) console.log(r.f.padEnd(52), fmt(r.raw), fmt(r.gz), r.spike ? '  yes' : '')
const sum = (rs) => ({ raw: rs.reduce((a, r) => a + r.raw, 0), gz: rs.reduce((a, r) => a + r.gz, 0) })
const js = rows.filter((r) => r.f.endsWith('.js'))
const t = sum(js); const s = sum(js.filter((r) => r.spike)); const b = sum(js.filter((r) => !r.spike))
console.log('\nassets/*.js total       ', fmt(t.raw), fmt(t.gz))
console.log('  spike chunks (js)     ', fmt(s.raw), fmt(s.gz))
console.log('  baseline (no spike)   ', fmt(b.raw), fmt(b.gz))
const wasm = rows.find((r) => r.f.endsWith('.wasm'))
if (wasm) console.log('ghostty-vt.wasm asset   ', fmt(wasm.raw), fmt(wasm.gz))
const gh = readFileSync(join(dir, rows.find((r) => r.f.startsWith('ghostty-web-')).f), 'utf8')
const m = gh.match(/data:application\/wasm;base64,[A-Za-z0-9+/=]+/)
console.log('\nghostty-web chunk embeds the wasm as a base64 data URL:', m ? `yes (${m[0].length} chars)` : 'no')
