// Fails on an unused export, type or file under src/{lib,composables,components,a2ui}.
// knip must see all of src (views import from these directories), so it runs over
// everything and this script keeps only the findings in the four directories.
import { spawnSync } from 'node:child_process'

const scope = /^src\/(lib|composables|components|a2ui)\//
const run = spawnSync('npx', ['knip', '--include', 'exports,types,files', '--reporter', 'json', '--no-exit-code'], {
  encoding: 'utf8',
  maxBuffer: 64 * 1024 * 1024,
})
if (run.status !== 0 && !run.stdout) {
  console.error(run.stderr)
  process.exit(2)
}
const report = JSON.parse(run.stdout)
const found = []
for (const f of report.files ?? []) if (scope.test(f)) found.push(`unused file: ${f}`)
for (const issue of report.issues ?? []) {
  if (!scope.test(issue.file)) continue
  // knip 6 reports unused files per issue; knip 5 used a top-level report.files list.
  if ((issue.files ?? []).length) found.push(`unused file: ${issue.file}`)
  for (const kind of ['exports', 'types']) {
    for (const e of issue[kind] ?? []) found.push(`unused ${kind.slice(0, -1)}: ${issue.file}:${e.line ?? '?'} ${e.name}`)
  }
}
if (found.length) {
  console.error(found.join('\n'))
  console.error(`\n${found.length} unused export(s) in lib/composables/components/a2ui: remove the export or use it.`)
  process.exit(1)
}
console.log('check-exports: ok')
