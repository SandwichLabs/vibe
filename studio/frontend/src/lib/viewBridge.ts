// The protocol between the host and a custom artifact view's Worker (#160): what
// the host accepts from the Worker, how much, and the fixed words it answers
// with. Nothing here touches the DOM or a binding. The Worker is untrusted (an
// allowed view is still code someone else or the agent wrote), so every field is
// shape-checked and size-capped before anything acts on it, and an error that
// goes back to it is one of the fixed texts below: never a path, an SQL error or
// the policy's reason.

/** Largest SVG the host will show (bytes). */
export const MAX_SVG_BYTES = 2 * 1024 * 1024
/** Largest A2UI page a view may hand back (bytes of JSON; Go caps it again). */
export const MAX_PAGE_BYTES = 1024 * 1024
/** Queries a view may have running at once. */
export const MAX_IN_FLIGHT = 4
/** Queries a view may start in any rolling minute. */
export const MAX_RUNS_PER_MINUTE = 60
/** Messages of any kind the host will take in one second before it stops the view. */
export const MAX_MESSAGES_PER_SECOND = 100
/** Payload bytes (SVG, page and text lengths) the host will take from a Worker in one second. */
export const MAX_BYTES_PER_SECOND = 16 * 1024 * 1024
/** A Worker must be ready this long after it started, whatever it says it is loading. */
export const READY_DEADLINE_MS = 15_000
/** The library files a Worker may announce it is loading, once each (the build's dist/assay-view/lib). */
export const LIB_FILES: readonly string[] = [
  'd3-array', 'd3-color', 'd3-format', 'd3-geo', 'd3-interpolate', 'd3-path', 'd3-scale', 'd3-shape', 'd3-time', 'd3-time-format',
  'vega', 'vega-interpreter', 'vega-lite',
]

/** The longest text a view's own failure message may show. */
const MAX_FAILURE_TEXT = 300
const MAX_PARAM_KEYS = 32
const MAX_PARAM_JSON = 8 * 1024
const MAX_TEXT_VALUE = 1000
const NAME = /^[A-Za-z][A-Za-z0-9_]{0,63}$/

/** Every error a query can answer a view with. */
export type RunErrorCode = 'bad' | 'busy' | 'rate' | 'failed'
export const RUN_ERROR_TEXT: Record<RunErrorCode, string> = {
  bad: 'That request was not valid.',
  busy: 'Too many queries are running at once.',
  rate: 'Too many queries in the last minute.',
  failed: 'The query could not be run.',
}

export type RunParams = Record<string, string | number | boolean | null>

/** What a view's Worker may send. */
export type WorkerMessage =
  | { t: 'hb' }
  | { t: 'loading'; file: string }
  | { t: 'ready' }
  | { t: 'loadFailed'; text: string }
  | { t: 'run'; id: number; query: string; params: RunParams }
  | { t: 'drawn'; id: number; svg: string }
  | { t: 'drawn'; id: number; a2ui: string }
  | { t: 'drawFailed'; id: number; text: string }

export type Verdict = { ok: true; msg: WorkerMessage } | { ok: false; code: RunErrorCode | 'ignore'; id?: number }

const isId = (v: unknown): v is number => Number.isInteger(v) && (v as number) > 0 && (v as number) < 2 ** 31
const isPlain = (v: unknown): v is Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return false
  const proto = Object.getPrototypeOf(v)
  return proto === Object.prototype || proto === null
}
const onlyKeys = (o: Record<string, unknown>, keys: string[]) => Object.keys(o).every((k) => keys.includes(k))

/** A query's parameters: a flat object of names to text, numbers, booleans or null. */
export function checkParams(v: unknown): RunParams | null {
  if (!isPlain(v)) return null
  const keys = Object.keys(v)
  if (keys.length > MAX_PARAM_KEYS) return null
  const out: RunParams = {}
  for (const k of keys) {
    const x = v[k]
    if (!NAME.test(k)) return null
    if (typeof x === 'string') {
      if ([...x].length > MAX_TEXT_VALUE) return null
    } else if (typeof x === 'number') {
      if (!Number.isFinite(x)) return null
    } else if (typeof x !== 'boolean' && x !== null) {
      return null
    }
    out[k] = x as string | number | boolean | null
  }
  return JSON.stringify(out).length > MAX_PARAM_JSON ? null : out
}

function checkRun(m: Record<string, unknown>): Verdict {
  const id = isId(m.id) ? m.id : undefined
  // Exactly these fields: a `sql` (or any other) field is a refused request.
  if (id === undefined || !onlyKeys(m, ['t', 'id', 'query', 'params']) || typeof m.query !== 'string' || !NAME.test(m.query)) {
    return { ok: false, code: id === undefined ? 'ignore' : 'bad', id }
  }
  const params = checkParams(m.params)
  if (!params) return { ok: false, code: 'bad', id }
  return { ok: true, msg: { t: 'run', id, query: m.query, params } }
}

function failureText(v: unknown): string {
  return typeof v === 'string' ? v.slice(0, MAX_FAILURE_TEXT) : 'The view failed.'
}

function checkDrawn(m: Record<string, unknown>, id: number): Verdict {
  if ('svg' in m && !('a2ui' in m) && onlyKeys(m, ['t', 'id', 'svg'])) {
    return typeof m.svg === 'string' ? { ok: true, msg: { t: 'drawn', id, svg: m.svg } } : { ok: false, code: 'ignore' }
  }
  // The page arrives as JSON text (the Worker stringified it), so the host sees its length before anything parses it.
  if ('a2ui' in m && !('svg' in m) && onlyKeys(m, ['t', 'id', 'a2ui'])) {
    return typeof m.a2ui === 'string' ? { ok: true, msg: { t: 'drawn', id, a2ui: m.a2ui } } : { ok: false, code: 'ignore' }
  }
  return { ok: false, code: 'ignore' }
}

/**
 * checkMessage shape-checks one message from the Worker. Only the types in
 * WorkerMessage get through; everything else (an unknown type, a wrong shape, a
 * `run` that names SQL) is refused: a `run` is answered with an error, anything
 * else is ignored. Size caps on a drawing are applied where it is shown.
 */
export function checkMessage(data: unknown): Verdict {
  if (!isPlain(data) || typeof data.t !== 'string') return { ok: false, code: 'ignore' }
  switch (data.t) {
    case 'loading':
      return onlyKeys(data, ['t', 'file']) && typeof data.file === 'string' && LIB_FILES.includes(data.file)
        ? { ok: true, msg: { t: 'loading', file: data.file } }
        : { ok: false, code: 'ignore' }
    case 'hb':
    case 'ready':
      return onlyKeys(data, ['t']) ? { ok: true, msg: { t: data.t } } : { ok: false, code: 'ignore' }
    case 'loadFailed':
      return { ok: true, msg: { t: 'loadFailed', text: failureText(data.text) } }
    case 'run':
      return checkRun(data)
    case 'drawn':
      return isId(data.id) ? checkDrawn(data, data.id) : { ok: false, code: 'ignore' }
    case 'drawFailed':
      return isId(data.id) ? { ok: true, msg: { t: 'drawFailed', id: data.id, text: failureText(data.text) } } : { ok: false, code: 'ignore' }
    default:
      return { ok: false, code: 'ignore' }
  }
}

/** A rolling count of calls in a window (a minute, a second). */
export class RateWindow {
  private stamps: number[] = []
  constructor(private readonly limit: number, private readonly windowMs: number) {}

  /** Records one call at `now`; false when it would be over the limit (it is not recorded). */
  take(now: number): boolean {
    const from = now - this.windowMs
    while (this.stamps.length && this.stamps[0] <= from) this.stamps.shift()
    if (this.stamps.length >= this.limit) return false
    this.stamps.push(now)
    return true
  }
}

/** What a message costs against the byte budget: the length of what it carries. */
export function messageBytes(m: WorkerMessage): number {
  switch (m.t) {
    case 'drawn': return 'svg' in m ? m.svg.length : m.a2ui.length
    case 'loadFailed':
    case 'drawFailed': return m.text.length
    case 'run': return JSON.stringify(m.params).length + m.query.length
    default: return 0
  }
}

/** A rolling sum of bytes in a window. */
export class ByteWindow {
  private items: Array<[number, number]> = []
  private sum = 0
  constructor(private readonly limit: number, private readonly windowMs: number) {}

  /** Adds `n` bytes at `now`; false when the window is now over its limit. */
  add(now: number, n: number): boolean {
    const from = now - this.windowMs
    while (this.items.length && this.items[0][0] <= from) this.sum -= this.items.shift()![1]
    this.items.push([now, n])
    this.sum += n
    return this.sum <= this.limit
  }
}

/** The theme roles handed to a view: plain strings, hex colours only. */
export function viewTheme(colors: Record<string, string>, type: string): Record<string, unknown> {
  const hex = (k: string) => (/^#[0-9A-Fa-f]{3,8}$/.test(colors[k] ?? '') ? colors[k] : undefined)
  const out: Record<string, unknown> = { type: type === 'dark' ? 'dark' : 'light' }
  for (const k of ['fg', 'fg.muted', 'bg', 'bg.alt', 'surface', 'border', 'accent']) {
    const v = hex(k)
    if (v) out[k] = v
  }
  const chart: string[] = []
  for (let i = 1; i <= 6; i++) {
    const v = hex(`chart.${i}`)
    if (v) {
      out[`chart.${i}`] = v
      chart.push(v)
    }
  }
  out.chart = chart
  return out
}
