// schemaDraft — the data side of the Schema Builder (parity W3): the friendly
// field-type vocabulary (the Access-style ID/Text/Link/Pick words the agent and
// the diagram speak, mapped to real DuckDB types), parsing of the agent's
// propose_schema drafts, and the diagram geometry (card auto-layout + arrow
// anchor points) so all of it is unit-testable outside the view.

export interface DraftColumn {
  name: string
  type: string
  primary_key?: boolean
  not_null?: boolean
}

export interface DraftTable {
  name: string
  columns: DraftColumn[]
}

export interface DraftRelationship {
  child_table: string
  child_column: string
  parent_table: string
  parent_column: string
  optional?: boolean
}

export interface SchemaDraft {
  tables: DraftTable[]
  relationships: DraftRelationship[]
  note?: string
}

// Friendly vocabulary -> DuckDB type. Keys are lowercase; unknown values pass
// through untouched so the agent may also use raw SQL types.
const FRIENDLY_TO_SQL: Record<string, string> = {
  id: 'BIGINT',
  text: 'VARCHAR',
  'long text': 'VARCHAR',
  number: 'DOUBLE',
  'whole number': 'BIGINT',
  money: 'DECIMAL(12,2)',
  date: 'DATE',
  time: 'TIMESTAMP',
  'yes/no': 'BOOLEAN',
  link: 'BIGINT',
  pick: 'VARCHAR',
}

export function toSQLType(friendly: string): string {
  return FRIENDLY_TO_SQL[friendly.trim().toLowerCase()] ?? friendly.trim()
}

// toFriendlyType maps a DuckDB column type back to the diagram vocabulary.
// pk/fk override the raw type — an ID is an ID whatever its storage type.
export function toFriendlyType(sqlType: string, opts: { pk?: boolean; fk?: boolean } = {}): string {
  if (opts.pk) return 'ID'
  if (opts.fk) return 'Link'
  const t = (sqlType || '').toUpperCase()
  if (t.startsWith('VARCHAR') || t === 'TEXT' || t.startsWith('STRING')) return 'Text'
  if (t === 'DATE') return 'Date'
  if (t.startsWith('TIMESTAMP') || t === 'TIME' || t === 'DATETIME') return 'Time'
  if (t === 'BOOLEAN' || t === 'BOOL') return 'Yes/No'
  if (t.startsWith('DECIMAL') || t.startsWith('NUMERIC')) return 'Money'
  if (['BIGINT', 'INTEGER', 'INT', 'SMALLINT', 'TINYINT', 'HUGEINT', 'UBIGINT', 'UINTEGER'].includes(t)) return 'Number'
  if (['DOUBLE', 'FLOAT', 'REAL', 'FLOAT4', 'FLOAT8'].includes(t)) return 'Number'
  return sqlType
}

// parseDraft parses a propose_schema tool input (the JSON the agent sent) into
// a normalized SchemaDraft. Returns null when the payload isn't usable.
export function parseDraft(inputJSON: string): SchemaDraft | null {
  let raw: any
  try {
    raw = JSON.parse(inputJSON)
  } catch {
    return null
  }
  if (!raw || !Array.isArray(raw.tables) || raw.tables.length === 0) return null
  const tables: DraftTable[] = []
  for (const t of raw.tables) {
    const name = String(t?.name ?? '').trim()
    if (!name) continue
    const columns: DraftColumn[] = []
    for (const c of t?.columns ?? []) {
      const cname = String(c?.name ?? '').trim()
      if (!cname) continue
      const type = String(c?.type ?? 'Text').trim() || 'Text'
      // The ID vocabulary word implies the primary key even when the model
      // forgets the explicit flag.
      const pk = Boolean(c?.primary_key) || type.toLowerCase() === 'id'
      columns.push({ name: cname, type, primary_key: pk, not_null: Boolean(c?.not_null) || pk })
    }
    if (columns.length === 0) continue
    tables.push({ name, columns })
  }
  if (tables.length === 0) return null
  const relationships: DraftRelationship[] = []
  for (const r of raw.relationships ?? []) {
    const rel: DraftRelationship = {
      child_table: String(r?.child_table ?? '').trim(),
      child_column: String(r?.child_column ?? '').trim(),
      parent_table: String(r?.parent_table ?? '').trim(),
      parent_column: String(r?.parent_column ?? '').trim() || 'id',
      optional: Boolean(r?.optional),
    }
    if (rel.child_table && rel.child_column && rel.parent_table) relationships.push(rel)
  }
  return { tables, relationships, note: typeof raw.note === 'string' ? raw.note : '' }
}

// ---- Diagram geometry ----

export interface Pos { x: number; y: number }
export interface Rect { x: number; y: number; w: number; h: number }

// Card metrics mirror the shipped .tbl CSS (168px wide; ink header + 24px rows).
export const CARD_W = 168
export const CARD_TITLE_H = 28
export const CARD_ROW_H = 24

export function cardHeight(rowCount: number): number {
  return CARD_TITLE_H + rowCount * CARD_ROW_H
}

// Column x positions / staggered starting y values echo the mockup layout.
const COL_X = [24, 248, 472]
const COL_START_Y = [56, 88, 72]
const GAP_Y = 28

// autoLayout assigns a position to every unplaced card, flowing into the
// shortest column, while leaving already-placed (possibly user-dragged) cards
// where they are.
export function autoLayout(
  order: string[],
  heightOf: (name: string) => number,
  placed: Record<string, Pos>,
): Record<string, Pos> {
  const pos: Record<string, Pos> = { ...placed }
  const bottoms = [...COL_START_Y]
  for (const name of Object.keys(placed)) {
    const p = placed[name]
    let ci = 0
    let best = Infinity
    COL_X.forEach((cx, i) => {
      const d = Math.abs(cx - p.x)
      if (d < best) { best = d; ci = i }
    })
    bottoms[ci] = Math.max(bottoms[ci], p.y + heightOf(name) + GAP_Y)
  }
  for (const name of order) {
    if (pos[name]) continue
    let ci = 0
    bottoms.forEach((b, i) => { if (b < bottoms[ci]) ci = i })
    pos[name] = { x: COL_X[ci], y: bottoms[ci] }
    bottoms[ci] += heightOf(name) + GAP_Y
  }
  return pos
}

// edgePoint returns where the segment from rect-center toward `toward` crosses
// the rect boundary — the anchor for an FK arrow so heads sit on card edges.
export function edgePoint(r: Rect, toward: Pos): Pos {
  const cx = r.x + r.w / 2
  const cy = r.y + r.h / 2
  const dx = toward.x - cx
  const dy = toward.y - cy
  if (dx === 0 && dy === 0) return { x: cx, y: cy }
  const sx = dx !== 0 ? r.w / 2 / Math.abs(dx) : Infinity
  const sy = dy !== 0 ? r.h / 2 / Math.abs(dy) : Infinity
  const s = Math.min(sx, sy)
  return { x: cx + dx * s, y: cy + dy * s }
}
