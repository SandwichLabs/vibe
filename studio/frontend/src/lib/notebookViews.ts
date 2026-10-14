// notebookViews — what each notebook sql-cell view needs from a query
// result, and whether a given result actually provides it. Ported from
// schoolwork-workspace's src/notebook/views.js: same four classic views, same rules,
// so the picker's hints, the "why is this greyed out" tooltips, the mismatch
// message, and the auto-fit on first run all tell the same story the prior
// art did. The KINETIC chart views (area … heatmap) follow the same pattern
// and mirror internal/agents' checkSeriesFit / checkTripleFit.
import { typeFamily } from './formatCell'

export type ViewKind = 'table' | 'bar' | 'line' | 'stat' | 'area' | 'columns' | 'rings' | 'radar' | 'sankey' | 'heatmap'

// The four classic views get toolbar buttons; the chart views sit in a menu.
export const CLASSIC_VIEWS: ViewKind[] = ['table', 'bar', 'line', 'stat']
export const CHART_VIEWS: ViewKind[] = ['area', 'columns', 'rings', 'radar', 'sankey', 'heatmap']
export const VIEWS: ViewKind[] = [...CLASSIC_VIEWS, ...CHART_VIEWS]

export const VIEW_LABELS: Record<ViewKind, string> = {
  table: 'TABLE', bar: 'BARS', line: 'LINE', stat: 'STAT',
  area: 'AREA', columns: 'COLUMNS', rings: 'RINGS', radar: 'RADAR', sankey: 'FLOW', heatmap: 'HEATMAP',
}

// The A2UI component type a view synthesizes into when a sql cell's result
// is rendered (#67's one renderer, shared by user cells and agent
// dashboards alike).
export const VIEW_COMPONENT: Record<ViewKind, string> = {
  table: 'table', bar: 'barChart', line: 'lineChart', stat: 'stat',
  area: 'areaChart', columns: 'columnChart', rings: 'ringChart', radar: 'radarChart', sankey: 'sankeyChart', heatmap: 'heatmap',
}

export const VIEW_HINTS: Record<ViewKind, string> = {
  table: 'every column, as rows',
  bar: 'a label column, then a number — one bar per row',
  line: 'an ordered label column, then a number — good for terms or dates',
  stat: 'one number: the first column of the first row',
  area: 'a label column, then one number column per series — smooth areas',
  columns: 'a label column, then one number column per series — stacked columns',
  rings: 'label, value[, max] per row — up to six progress rings',
  radar: 'an axis label column (3+ rows), then one number column per series',
  sankey: 'source, target, then a numeric weight — one ribbon per row',
  heatmap: 'a row label, a column label, then a number — one cell per row',
}

// MAX_RINGS matches the ring chart's (and internal/agents' maxRings) limit.
const MAX_RINGS = 6

function isNum(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

// isNumericFamily reports whether a DuckDB type name (as sent alongside
// query results) is one formatCell would right-align as a number.
function isNumericFamily(dbType: string): boolean {
  const f = typeFamily(dbType, undefined)
  return f === 'integer' || f === 'decimal' || f === 'float'
}

export interface Shape {
  cols: string[]
  numeric: string[]
  label: string[]
  rowCount: number
  firstDistinct: boolean
}

/**
 * Describe a result set (column-array + row-array, RunA2UIQuery's shape):
 * which columns hold numbers, which hold labels.
 *
 * When `types` (the DuckDB type name per column) is given, a column is
 * classified by its declared type rather than by sampling a row value. This
 * closes the "mixed column" edge case the original value-sampling approach
 * had: a HUGEINT/DECIMAL column whose first non-null value happens to be
 * out of JS's safe integer range crosses the wire as a string (per
 * lib/formatCell's wire contract), which `isNum` would misjudge as a label
 * column even though it is exact, chartable numeric data. Falls back to
 * sampling — the original behaviour — for any column with no type at hand
 * (model-authored inline A2UI data has none), so this stays backward
 * compatible with every caller that doesn't have `types` yet.
 */
export function describeShape(columns: string[], rows: any[][], types?: string[]): Shape {
  const cols = columns.length ? columns : (rows[0] ? rows[0].map((_, i) => String(i)) : [])
  const numeric: string[] = []
  const label: string[] = []
  const colIndex = new Map(cols.map((c, i) => [c, i]))
  for (const c of cols) {
    const i = colIndex.get(c)!
    const dbType = types?.[i]
    if (dbType) {
      ;(isNumericFamily(dbType) ? numeric : label).push(c)
      continue
    }
    const sample = rows.find((r) => r[i] !== null && r[i] !== undefined)
    if (sample === undefined) { label.push(c); continue }
    ;(isNum(sample[i]) ? numeric : label).push(c)
  }
  const firstIdx = 0
  const seen = new Set(rows.map((r) => String(r[firstIdx])))
  return {
    cols,
    numeric,
    label,
    rowCount: rows.length,
    firstDistinct: rows.length > 0 && seen.size === rows.length,
  }
}

interface ViewFit {
  ok: boolean
  why?: string
}

/** Can `view` render this shape? */
export function viewFit(view: ViewKind, shape: Shape): ViewFit {
  const { cols, numeric, rowCount } = shape
  if (cols.length === 0) return { ok: false, why: 'the query returned no columns' }

  switch (view) {
    case 'table':
      return { ok: true }
    case 'bar':
    case 'line':
    case 'area':
    case 'columns':
    case 'rings':
    case 'radar': {
      if (numeric.length === 0) {
        return { ok: false, why: `needs a numeric column — this query returns only text (${cols.join(', ')})` }
      }
      if (cols.length < 2) {
        return { ok: false, why: 'needs two columns: something to label each point, then the number' }
      }
      if (view === 'radar' && rowCount < 3) return { ok: false, why: 'needs at least three rows, one per axis' }
      if (view === 'rings' && rowCount > MAX_RINGS) {
        return { ok: false, why: `draws at most ${MAX_RINGS} rings, but this query returns ${rowCount} rows — filter or LIMIT it` }
      }
      return { ok: true }
    }
    case 'sankey':
    case 'heatmap': {
      if (cols.length < 3 || !numeric.includes(cols[2])) {
        const want = view === 'sankey' ? 'source, target, then a numeric weight' : 'a row label, a column label, then a numeric value'
        return { ok: false, why: `needs three columns: ${want}` }
      }
      return { ok: true }
    }
    case 'stat': {
      if (rowCount === 0) return { ok: false, why: 'needs one row — this query returned none' }
      if (rowCount > 1) {
        return { ok: false, why: `shows a single number, but this query returns ${rowCount} rows — aggregate it, or add LIMIT 1` }
      }
      return { ok: true }
    }
    default:
      return { ok: true }
  }
}

const ORDERED_HINTS = ['date', 'day', 'week', 'month', 'year', 'term', 'window', 'period', 'time', 'quarter', 'season']

function looksOrdered(name: string): boolean {
  const n = String(name).toLowerCase()
  return ORDERED_HINTS.some((h) => n.includes(h))
}

/**
 * The view that suits a result best, used only for cells whose view a
 * person has not pinned. Deliberately conservative: prefers TABLE whenever
 * a chart would be a guess, because a wrong chart is worse than an honest
 * table.
 */
export function bestView(shape: Shape): ViewKind {
  const { cols, numeric, rowCount, firstDistinct } = shape
  if (rowCount === 1 && cols.length === 1 && numeric.length === 1) return 'stat'

  const CHARTABLE_ROWS = 25
  if (cols.length === 2 && numeric.includes(cols[1]) && rowCount > 1
      && rowCount <= CHARTABLE_ROWS && firstDistinct) {
    return looksOrdered(cols[0]) ? 'line' : 'bar'
  }
  return 'table'
}
