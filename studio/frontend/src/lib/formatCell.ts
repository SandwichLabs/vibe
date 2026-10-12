// formatCell — the one cell formatter. A value + its DuckDB column
// type + a display context (grid | report) gives display text, a semantic
// kind (for CSS), an alignment, and an optional title (full text for a
// truncated cell). The query grid, the datasheet, A2UI table/stat, and
// notebook SQL cells all call this instead of formatting inline, so a
// DECIMAL(18,4)/BIGINT/DATE/… value reads identically everywhere it appears.
//
// Wire contract this formatter assumes (see internal/engine/normalize.go and
// PLAN-83 §2): a BIGINT/UBIGINT/HUGEINT value is a JS number when it fits
// Number.MAX_SAFE_INTEGER, else an exact decimal string. A DECIMAL is a JS
// number carrying the exact scale-padded text when it round-trips exactly,
// else an exact string — either way JSON preserves the scale as written, but
// `String(15675.3)` in JS drops trailing zeros, so the DECIMAL's declared
// scale must come from the column's `DECIMAL(w,s)` type string, never from
// the number's own JS text. DATE/TIME/TIMESTAMP[TZ]/INTERVAL/UUID/BLOB are
// already DuckDB-formatted strings. NULL is JS null.
//
// #96 seam: CellFormatOptions gains a `format` field ('money' | 'percent' |
// 'integer' | ...) later. The number branch of formatCell below is the only
// place a numeric value becomes text, so #96 adds one switch there — no
// other call site changes.

type CellContext = 'grid' | 'report'

interface CellFormatOptions {
  context?: CellContext // default 'grid'
}

type CellKind = 'null' | 'number' | 'bool' | 'text' | 'nested'

export interface FormattedCell {
  text: string
  kind: CellKind
  align: 'left' | 'right'
  title?: string
}

type TypeFamily = 'integer' | 'decimal' | 'float' | 'boolean' | 'temporal' | 'text' | 'nested'

const TITLE_LEN_THRESHOLD = 40

// typeFamily classifies a DuckDB type name (or, absent one — model-authored
// inline A2UI data has no column type — infers from the JS value's own
// typeof) into the coarse family the display rules below key off of.
export function typeFamily(dbType: string | undefined, value: unknown): TypeFamily {
  const t = (dbType || '').toUpperCase()
  if (t) {
    // Checked first: a LIST/ARRAY element type or a STRUCT field type can
    // itself contain "INT" (INTEGER[], STRUCT("a" INTEGER, ...)) — nested
    // must win before the plain-INT substring check below sees it.
    if (/\[\d*\]$/.test(t) || t.startsWith('STRUCT') || t.startsWith('MAP') || t.startsWith('UNION')) return 'nested'
    // Checked before the generic INT substring test: INTERVAL contains
    // "INT" too.
    if (t.startsWith('DATE') || t.startsWith('TIME') || t.startsWith('INTERVAL')) return 'temporal'
    if (t.startsWith('DECIMAL') || t.startsWith('NUMERIC')) {
      return decimalScale(t) ? 'decimal' : 'integer'
    }
    if (t.includes('INT')) return 'integer' // TINYINT..HUGEINT, UTINYINT..UHUGEINT
    if (t === 'DOUBLE' || t === 'FLOAT' || t === 'REAL') return 'float'
    if (t === 'BOOLEAN' || t === 'BOOL') return 'boolean'
    return 'text'
  }
  if (value === null || value === undefined) return 'text'
  if (typeof value === 'number') return 'float'
  if (typeof value === 'boolean') return 'boolean'
  if (typeof value === 'object') return 'nested'
  return 'text'
}

// isNumericType reports whether a DuckDB type name is one formatCell right-
// aligns as a number (integer/decimal/float families) — the one place
// "is this column numeric" is decided, so a header's alignment, a NULL
// cell's alignment, and lib/notebookViews.ts's shape classification all
// agree with what the cell itself renders as, instead of being re-derived
// ad hoc per call site
// (`['integer','decimal','float'].includes(typeFamily(...))` inlined in a
// view).
export function isNumericType(dbType: string | undefined): boolean {
  const f = typeFamily(dbType, undefined)
  return f === 'integer' || f === 'decimal' || f === 'float'
}

// decimalScale extracts s from a DECIMAL(w,s)/NUMERIC(w,s) type name, or
// null when the type isn't DECIMAL/NUMERIC or has no explicit scale (scale
// 0, e.g. DECIMAL(10) or DECIMAL(10,0), is an integer — also null).
export function decimalScale(dbType: string | undefined): number | null {
  const m = /^(?:DECIMAL|NUMERIC)\((\d+)\s*,\s*(\d+)\)$/i.exec((dbType || '').trim())
  if (!m) return null
  const s = Number(m[2])
  return s > 0 ? s : null
}

// groupDigits inserts the locale's group separator into the integer part of
// a plain decimal string (optionally signed, optionally with a '.' fraction)
// and swaps '.' for the locale's own decimal point. Operates on text so it
// never round-trips a big/exact value through a double.
const localeParts = (() => {
  try {
    const parts = new Intl.NumberFormat(undefined, { useGrouping: true }).formatToParts(1234.5)
    const group = parts.find((p) => p.type === 'group')?.value ?? ','
    const decimal = parts.find((p) => p.type === 'decimal')?.value ?? '.'
    return { group, decimal }
  } catch {
    return { group: ',', decimal: '.' }
  }
})()

export function groupDigits(text: string): string {
  const neg = text.startsWith('-')
  const body = neg ? text.slice(1) : text
  const dot = body.indexOf('.')
  const intPart = dot >= 0 ? body.slice(0, dot) : body
  const fracPart = dot >= 0 ? body.slice(dot + 1) : ''
  if (!/^\d+$/.test(intPart)) return text // not a plain decimal string, leave as-is
  let grouped = ''
  for (let i = 0; i < intPart.length; i++) {
    const posFromEnd = intPart.length - i
    grouped += intPart[i]
    if (posFromEnd > 1 && posFromEnd % 3 === 1) grouped += localeParts.group
  }
  return (neg ? '-' : '') + grouped + (fracPart ? localeParts.decimal + fracPart : '')
}

// numberFormatter — the shared ≤2-decimal-place, trailing-zeros-trimmed
// formatter for 'float' family values (moved here from a2ui/query.ts).
const numberFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2, minimumFractionDigits: 0 })

// formatNumber formats a finite number with the shared numberFormatter. Used
// directly by chart components that only ever display a plain JS number
// (bar chart values, line chart axis/tooltip labels) — moved here from
// a2ui/query.ts, same behaviour.
export function formatNumber(v: number): string {
  if (!Number.isFinite(v)) return '—'
  return numberFormatter.format(v)
}

function newlineMark(s: string): string {
  return s.replace(/\r\n|\n/g, '↵').replace(/\t/g, '→')
}

// formatCell turns one wire value into display text, kind, alignment and an
// optional title, given the column's DuckDB type (undefined for
// model-authored inline A2UI data) and a context.
export function formatCell(value: unknown, dbType?: string, opts?: CellFormatOptions): FormattedCell {
  const context = opts?.context ?? 'grid'

  if (value === null || value === undefined) {
    // Aligned by the COLUMN's family, not hard-coded right — a NULL in a
    // text/temporal/nested column sits left,
    // matching the non-null cells around it; a NULL with no dbType at all
    // (model-authored inline A2UI data with no other clue) defaults left,
    // same as ordinary text.
    return { text: context === 'report' ? '—' : 'NULL', kind: 'null', align: isNumericType(dbType) ? 'right' : 'left' }
  }

  const family = typeFamily(dbType, value)

  if (family === 'boolean') {
    return { text: String(value), kind: 'bool', align: 'left' }
  }

  if (family === 'integer') {
    const text = String(value)
    const shown = context === 'report' ? groupDigits(text) : text
    return { text: shown, kind: 'number', align: 'right' }
  }

  if (family === 'decimal') {
    const scale = decimalScale(dbType) ?? 0
    const text = typeof value === 'number' ? value.toFixed(scale) : String(value)
    const shown = groupDigits(text)
    return { text: shown, kind: 'number', align: 'right' }
  }

  if (family === 'float') {
    const n = typeof value === 'number' ? value : Number(value)
    if (context === 'report') {
      return { text: formatNumber(n), kind: 'number', align: 'right' }
    }
    const text = Number.isFinite(n) ? String(n) : String(value)
    return { text: groupDigits(text), kind: 'number', align: 'right' }
  }

  if (family === 'nested') {
    const text = JSON.stringify(value)
    const title = text.length > TITLE_LEN_THRESHOLD ? text : undefined
    return { text, kind: 'nested', align: 'left', title }
  }

  // temporal / text (and the "no type, not a number/bool/object" fallback,
  // which also lands here as plain text — e.g. a big BIGINT/DECIMAL string
  // with no column type at all).
  const raw = String(value)
  const display = newlineMark(raw)
  const title = raw.length > TITLE_LEN_THRESHOLD || /\r\n|\n/.test(raw) ? raw : undefined
  return { text: display, kind: 'text', align: 'left', title }
}

// cellEditText seeds the datasheet's inline editor: the raw, ungrouped,
// locale-free text DuckDB's CAST will accept back. Grouping and the
// report/grid number formatting above are display-only and must never reach
// an editor.
//
// A nested (LIST/ARRAY/STRUCT/MAP) value seeds with the exact same
// JSON.stringify text formatCell displays — not `String(value)`, which
// stringifies an object/array to
// "[object Object]"/comma-joined text that disagrees with what the user
// just clicked and that DuckDB can't CAST back. DuckDB's own `::type` cast
// accepts JSON-shaped list/struct literals fine (`'[1,null,3]'::INTEGER[]`),
// so "editor seed == display minus grouping" holds for every family, and
// the untouched-cell guard in TableBrowserView.vue (which also calls
// cellEditText) still compares like for like.
export function cellEditText(value: unknown, dbType?: string): string {
  if (value === null || value === undefined) return ''
  const scale = decimalScale(dbType)
  if (scale !== null) {
    return typeof value === 'number' ? value.toFixed(scale) : String(value)
  }
  if (typeFamily(dbType, value) === 'nested') return JSON.stringify(value)
  return String(value)
}

// cellClass returns the CSS classes a formatted cell should render with
// (theme/bridge.css: .cell-num / .cell-null / .cell-text / .cell-temporal).
// dbType is optional and only used to add tabular-nums for temporal values
// (dates/times/timestamps/intervals), which are text-kind but still read
// better column-aligned.
//
// `.cell-num` (right-align + tabular-nums) is driven by `fc.align`, not
// `fc.kind === 'number'` — that was the only reader `FormattedCell.align`
// had, and it ignored the field, so a NULL
// cell (`kind: 'null'`) in a numeric column stayed left-aligned under
// right-aligned numbers. formatCell's NULL branch now sets `align` from the
// column's family too, so this one rule keeps every kind consistent.
export function cellClass(fc: FormattedCell, dbType?: string): string[] {
  const classes: string[] = []
  if (fc.align === 'right') classes.push('cell-num')
  if (fc.kind === 'null') classes.push('cell-null')
  if (fc.kind === 'text' || fc.kind === 'nested') classes.push('cell-text')
  if (dbType && typeFamily(dbType, undefined) === 'temporal') classes.push('cell-temporal')
  return classes
}
