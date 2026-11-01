// Copies the pinned libraries a custom artifact view may load (#160) from
// node_modules into dist/assay-view/lib/, where Studio's Go binary embeds them and
// serves them under /assay-view/lib/. Run after `vite build` (npm run build).
//
// The versions are the exact ones in package.json (no ranges), so the bytes are
// reproducible from package-lock.json. vega-interpreter ships only as an ES
// module, so it is bundled (with its one dependency, vega-util) into a classic
// script that exposes `vegaInterpreter.expressionInterpreter`; that is what lets
// vega run without eval (the view's CSP has no 'unsafe-eval').
// Usage: node scripts/vendor-view-libs.mjs   (run from studio/frontend)
import { copyFileSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { build } from 'vite'

const OUT = join('dist', 'assay-view', 'lib')

// [package, file inside it, name served as /assay-view/lib/<name>.js]
const COPY = [
  ['d3-array', 'dist/d3-array.min.js'],
  ['d3-color', 'dist/d3-color.min.js'],
  ['d3-format', 'dist/d3-format.min.js'],
  ['d3-geo', 'dist/d3-geo.min.js'],
  ['d3-interpolate', 'dist/d3-interpolate.min.js'],
  ['d3-path', 'dist/d3-path.min.js'],
  ['d3-scale', 'dist/d3-scale.min.js'],
  ['d3-shape', 'dist/d3-shape.min.js'],
  ['d3-time', 'dist/d3-time.min.js'],
  ['d3-time-format', 'dist/d3-time-format.min.js'],
  ['vega', 'build/vega.min.js'],
  ['vega-lite', 'build/vega-lite.min.js'],
]

rmSync(OUT, { recursive: true, force: true })
mkdirSync(OUT, { recursive: true })
for (const [pkg, file] of COPY) {
  const src = join('node_modules', pkg, file)
  copyFileSync(src, join(OUT, `${pkg}.js`))
}

// The interpreter, bundled into one classic script.
const tmp = join('dist', '.assay-view-interpreter')
await build({
  configFile: false,
  logLevel: 'warn',
  build: {
    outDir: tmp,
    emptyOutDir: true,
    minify: true,
    lib: {
      entry: join('node_modules', 'vega-interpreter', 'build', 'vega-interpreter.js'),
      name: 'vegaInterpreter',
      formats: ['iife'],
      fileName: () => 'vega-interpreter.js',
    },
  },
})
copyFileSync(join(tmp, 'vega-interpreter.js'), join(OUT, 'vega-interpreter.js'))
rmSync(tmp, { recursive: true, force: true })

for (const f of readdirSync(OUT).sort()) {
  const size = statSync(join(OUT, f)).size
  if (size < 1000) throw new Error(`vendored ${f} is only ${size} bytes`)
  if (/\b(import|export)\s+[{*\w]/.test(readFileSync(join(OUT, f), 'utf8').slice(0, 2000))) {
    throw new Error(`${f} looks like an ES module, not a classic script`)
  }
}
console.log(`vendored ${readdirSync(OUT).length} view libraries into ${OUT}`)
