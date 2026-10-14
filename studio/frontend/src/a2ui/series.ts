// Data shaping for the KINETIC charts. Each takes the same columns + rows a
// live "sql" binding returns (inline data uses that exact shape too), so one
// function serves both. Pure: no Vue, no bindings.

export interface TableData {
  columns: string[]
  rows: any[][]
}

interface Series {
  name: string
  values: number[]
}

export interface SeriesData {
  labels: string[]
  series: Series[]
}

// The categorical palette has six roles (--vibe-chart-1..6); series past the
// sixth reuse them in order.
const PALETTE_SIZE = 6

export function seriesClass(i: number): string {
  return `a2ui-series-${i % PALETTE_SIZE}`
}

export function num(v: unknown): number {
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? n : 0
}

function isNumeric(v: unknown): boolean {
  if (typeof v === 'number') return Number.isFinite(v)
  // A HUGEINT/DECIMAL past JS's safe range crosses the wire as a numeric string.
  return typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v))
}

function columnNames(t: TableData): string[] {
  if (t.columns.length) return t.columns
  return t.rows[0] ? t.rows[0].map((_, i) => String(i)) : []
}

// toSeries reads a label column then one numeric column per series: the label
// is the first column whose first non-null value is not a number (column 0 if
// every column is numeric), and every other numeric column is a series.
export function toSeries(t: TableData): SeriesData {
  const cols = columnNames(t)
  const numeric = cols.map((_, i) => {
    const sample = t.rows.find((r) => r[i] !== null && r[i] !== undefined)
    return sample !== undefined && isNumeric(sample[i])
  })
  let labelIdx = numeric.findIndex((n) => !n)
  if (labelIdx < 0) labelIdx = 0
  const series: Series[] = []
  cols.forEach((name, i) => {
    if (i === labelIdx || !numeric[i]) return
    series.push({ name, values: t.rows.map((r) => num(r[i])) })
  })
  return { labels: t.rows.map((r) => String(r[labelIdx] ?? '')), series }
}

export interface Ring {
  label: string
  value: number
  max: number
}

const MAX_RINGS = 6

// toRings reads label, value[, max] per row; max falls back to the
// component's "max" (default 100). At most MAX_RINGS rings.
export function toRings(t: TableData, defaultMax?: number): Ring[] {
  const dflt = defaultMax && defaultMax > 0 ? defaultMax : 100
  return t.rows.slice(0, MAX_RINGS).map((r) => {
    const max = r.length > 2 && num(r[2]) > 0 ? num(r[2]) : dflt
    return { label: String(r[0] ?? ''), value: num(r[1]), max }
  })
}

interface Flow {
  from: number
  to: number
  weight: number
}

export interface FlowData {
  sources: { name: string; total: number }[]
  targets: { name: string; total: number }[]
  flows: Flow[]
}

// toFlows reads source, target, weight per row. Nodes keep first-seen order;
// non-positive weights are dropped (a ribbon can't be negative).
export function toFlows(t: TableData): FlowData {
  const sources: FlowData['sources'] = []
  const targets: FlowData['targets'] = []
  const idx = (list: FlowData['sources'], name: string) => {
    let i = list.findIndex((n) => n.name === name)
    if (i < 0) i = list.push({ name, total: 0 }) - 1
    return i
  }
  const flows: Flow[] = []
  for (const r of t.rows) {
    const weight = num(r[2])
    if (weight <= 0) continue
    const from = idx(sources, String(r[0] ?? ''))
    const to = idx(targets, String(r[1] ?? ''))
    sources[from].total += weight
    targets[to].total += weight
    flows.push({ from, to, weight })
  }
  return { sources, targets, flows }
}

export interface MatrixData {
  rowLabels: string[]
  colLabels: string[]
  // cells[r][c]; null where the result had no row for that pair
  cells: (number | null)[][]
  min: number
  max: number
}

// toMatrix reads row label, column label, value per row (long format, what a
// GROUP BY 1, 2 returns). Labels keep first-seen order; a repeated pair keeps
// its last value.
export function toMatrix(t: TableData): MatrixData {
  const rowLabels: string[] = []
  const colLabels: string[] = []
  const at = new Map<string, number>()
  for (const r of t.rows) {
    const rl = String(r[0] ?? '')
    const cl = String(r[1] ?? '')
    if (!rowLabels.includes(rl)) rowLabels.push(rl)
    if (!colLabels.includes(cl)) colLabels.push(cl)
    at.set(JSON.stringify([rl, cl]), num(r[2]))
  }
  const cells = rowLabels.map((rl) => colLabels.map((cl) => at.get(JSON.stringify([rl, cl])) ?? null))
  const vals = [...at.values()]
  return { rowLabels, colLabels, cells, min: vals.length ? Math.min(...vals) : 0, max: vals.length ? Math.max(...vals) : 0 }
}

// heatLevel quantizes a value into one of four buckets (0 = coolest) for the
// heatmap's class-based colors.
export function heatLevel(v: number, min: number, max: number): number {
  const span = max - min
  if (span <= 0) return 3
  return Math.min(3, Math.floor(((v - min) / span) * 4))
}

export interface XY {
  x: number
  y: number
}

// splinePath draws a Catmull-Rom curve through the points as cubic Béziers.
export function splinePath(pts: XY[]): string {
  if (pts.length === 0) return ''
  const f = (n: number) => n.toFixed(1)
  let d = `M ${f(pts[0].x)} ${f(pts[0].y)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i === 0 ? i : i - 1]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2 < pts.length ? i + 2 : i + 1]
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = p1.y + (p2.y - p0.y) / 6
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = p2.y - (p3.y - p1.y) / 6
    d += ` C ${f(c1x)} ${f(c1y)}, ${f(c2x)} ${f(c2y)}, ${f(p2.x)} ${f(p2.y)}`
  }
  return d
}

// areaPath closes a spline down to the baseline.
export function areaPath(pts: XY[], baselineY: number): string {
  if (pts.length < 2) return ''
  const last = pts[pts.length - 1]
  return `${splinePath(pts)} L ${last.x.toFixed(1)} ${baselineY} L ${pts[0].x.toFixed(1)} ${baselineY} Z`
}

// polar places value v (of max) on axis i of n, starting at 12 o'clock.
export function polar(i: number, n: number, v: number, max: number, cx: number, cy: number, radius: number): XY {
  const angle = (Math.PI * 2 * i) / n - Math.PI / 2
  const r = (Math.max(0, Math.min(v / (max || 1), 1.1)) || 0) * radius
  return { x: cx + r * Math.cos(angle), y: cy + r * Math.sin(angle) }
}
