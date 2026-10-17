// Pivot, the pure part (#162, D6): a LONG-FORM result (one row per row-key x
// column-key, already aggregated by the query) laid out as a crosstab grid.
// Compute stays in the engine: nothing here sums, counts or totals. Headers
// keep the order of first appearance (which the query's ORDER BY controls);
// NULL keys read "(blank)" and ROLLUP rows are laid out as returned, like any
// other row. Two rows for one cell are an error that names the cell. Every
// string produced is plain text for interpolation.
import { formatCell, isNumericType } from '../lib/formatCell'

export interface PivotSpec { rows: string[]; columns: string; value: string }

interface PivotCell {
  text: string
  title?: string
  numeric: boolean
  /** Index of the result row this cell was laid out from (what a drill reads). */
  src: number
}

interface PivotGrid {
  /** The row-key column names, in order. */
  rowColumns: string[]
  /** Column headers in order of first appearance. */
  colKeys: string[]
  rows: { keys: string[]; cells: (PivotCell | null)[] }[]
}

export type PivotResult = { ok: true; grid: PivotGrid } | { ok: false; error: string }

const BLANK = '(blank)'
const EMPTY = '(empty)'
const MAX_NAME = 80

const clip = (s: string) => (s.length > MAX_NAME ? s.slice(0, MAX_NAME) + '…' : s)

/** A key as header text: NULL is "(blank)", '' is "(empty)", anything else as written. */
export function keyText(v: unknown): string {
  if (v === null || v === undefined) return BLANK
  if (v === '') return EMPTY
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

// A key's identity keeps NULL, '' and the text "(blank)" apart.
function keyId(v: unknown): string {
  if (v === null || v === undefined) return 'n'
  return (typeof v) + ':' + (typeof v === 'object' ? JSON.stringify(v) : String(v))
}

export function buildPivot(columns: string[], types: string[], data: any[][], spec: PivotSpec): PivotResult {
  const idx = (name: string) => columns.indexOf(name)
  const need = [...spec.rows, spec.columns, spec.value]
  const missing = need.filter((n) => idx(n) < 0)
  if (missing.length) {
    return { ok: false, error: `Pivot column ${missing.map((m) => `"${clip(m)}"`).join(', ')} is not in the query's result. Columns: ${columns.slice(0, 20).map(clip).join(', ')}.` }
  }
  const rowIdx = spec.rows.map(idx)
  const colI = idx(spec.columns)
  const valI = idx(spec.value)
  const numeric = isNumericType(types[valI])

  const colOrder = new Map<string, number>()
  const colKeys: string[] = []
  const rowOrder = new Map<string, number>()
  const rows: PivotGrid['rows'] = []
  const filled = new Set<string>()

  for (const r of data) {
    const cid = keyId(r[colI])
    if (!colOrder.has(cid)) { colOrder.set(cid, colKeys.length); colKeys.push(keyText(r[colI])) }
    const rid = JSON.stringify(rowIdx.map((i) => keyId(r[i])))
    if (!rowOrder.has(rid)) { rowOrder.set(rid, rows.length); rows.push({ keys: rowIdx.map((i) => keyText(r[i])), cells: [] }) }
  }

  for (let src = 0; src < data.length; src++) {
    const r = data[src]
    const ri = rowOrder.get(JSON.stringify(rowIdx.map((i) => keyId(r[i]))))!
    const ci = colOrder.get(keyId(r[colI]))!
    const cell = `${ri}/${ci}`
    if (filled.has(cell)) {
      const where = [...rows[ri].keys, colKeys[ci]].map(clip).join(' / ')
      const by = [...spec.rows, spec.columns].map((n) => `"${clip(n)}"`).join(', ')
      return { ok: false, error: `Two rows of the result fill the pivot cell (${where}). Group the query by ${by} so each cell has one row.` }
    }
    filled.add(cell)
    const f = formatCell(r[valI], types[valI], { context: 'report' })
    rows[ri].cells[ci] = { text: f.text, title: f.title, numeric: numeric || f.kind === 'number', src }
  }
  for (const row of rows) {
    for (let c = 0; c < colKeys.length; c++) row.cells[c] ??= null
  }
  return { ok: true, grid: { rowColumns: spec.rows, colKeys, rows } }
}
