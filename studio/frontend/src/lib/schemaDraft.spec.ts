// Unit tests for the Schema Builder's data layer (parity W3): friendly-type
// vocabulary mapping, propose_schema draft parsing, and diagram geometry.
import { describe, it, expect } from 'vitest'
import {
  toSQLType, toFriendlyType, parseDraft, autoLayout, cardHeight, edgePoint,
  CARD_W, CARD_TITLE_H, CARD_ROW_H,
} from './schemaDraft'

describe('toSQLType (friendly → DuckDB)', () => {
  it('maps the friendly vocabulary', () => {
    expect(toSQLType('ID')).toBe('BIGINT')
    expect(toSQLType('Text')).toBe('VARCHAR')
    expect(toSQLType('Long text')).toBe('VARCHAR')
    expect(toSQLType('Number')).toBe('DOUBLE')
    expect(toSQLType('Whole number')).toBe('BIGINT')
    expect(toSQLType('Money')).toBe('DECIMAL(12,2)')
    expect(toSQLType('Date')).toBe('DATE')
    expect(toSQLType('Yes/No')).toBe('BOOLEAN')
    expect(toSQLType('Link')).toBe('BIGINT')
    expect(toSQLType('Pick')).toBe('VARCHAR')
  })

  it('is case-insensitive and passes raw SQL types through', () => {
    expect(toSQLType('yes/no')).toBe('BOOLEAN')
    expect(toSQLType('VARCHAR')).toBe('VARCHAR')
    expect(toSQLType('DECIMAL(10,2)')).toBe('DECIMAL(10,2)')
  })
})

describe('toFriendlyType (DuckDB → friendly)', () => {
  it('pk and fk win over the storage type', () => {
    expect(toFriendlyType('BIGINT', { pk: true })).toBe('ID')
    expect(toFriendlyType('BIGINT', { fk: true })).toBe('Link')
  })

  it('maps common DuckDB types', () => {
    expect(toFriendlyType('VARCHAR')).toBe('Text')
    expect(toFriendlyType('DATE')).toBe('Date')
    expect(toFriendlyType('TIMESTAMP')).toBe('Time')
    expect(toFriendlyType('BOOLEAN')).toBe('Yes/No')
    expect(toFriendlyType('DECIMAL(12,2)')).toBe('Money')
    expect(toFriendlyType('BIGINT')).toBe('Number')
    expect(toFriendlyType('DOUBLE')).toBe('Number')
  })

  it('passes unknown types through', () => {
    expect(toFriendlyType('BLOB')).toBe('BLOB')
  })
})

describe('parseDraft', () => {
  const good = JSON.stringify({
    tables: [
      { name: 'customers', columns: [{ name: 'id', type: 'ID' }, { name: 'name', type: 'Text', not_null: true }] },
      { name: 'service_calls', columns: [{ name: 'id', type: 'ID', primary_key: true }, { name: 'customer_id', type: 'Link' }] },
    ],
    relationships: [
      { child_table: 'service_calls', child_column: 'customer_id', parent_table: 'customers', parent_column: 'id' },
    ],
    note: 'HVAC basics',
  })

  it('parses tables, relationships and the note', () => {
    const d = parseDraft(good)!
    expect(d.tables).toHaveLength(2)
    expect(d.relationships).toHaveLength(1)
    expect(d.note).toBe('HVAC basics')
  })

  it('the ID type implies primary_key + not_null even without flags', () => {
    const d = parseDraft(good)!
    const id = d.tables[0].columns[0]
    expect(id.primary_key).toBe(true)
    expect(id.not_null).toBe(true)
  })

  it('defaults a missing parent_column to id', () => {
    const d = parseDraft(JSON.stringify({
      tables: [{ name: 't', columns: [{ name: 'id', type: 'ID' }] }],
      relationships: [{ child_table: 't', child_column: 'x', parent_table: 'p' }],
    }))!
    expect(d.relationships[0].parent_column).toBe('id')
  })

  it('rejects garbage, empty drafts, and tables without columns', () => {
    expect(parseDraft('not json')).toBeNull()
    expect(parseDraft('{"tables":[]}')).toBeNull()
    expect(parseDraft('{"tables":[{"name":"t","columns":[]}]}')).toBeNull()
  })
})

describe('diagram geometry', () => {
  it('cardHeight follows the .tbl CSS metrics', () => {
    expect(cardHeight(3)).toBe(CARD_TITLE_H + 3 * CARD_ROW_H)
  })

  it('autoLayout places every card without overlap in a column flow', () => {
    const names = ['a', 'b', 'c', 'd', 'e']
    const pos = autoLayout(names, () => cardHeight(3), {})
    expect(Object.keys(pos)).toHaveLength(5)
    // No two cards share a position.
    const seen = new Set(Object.values(pos).map((p) => `${p.x},${p.y}`))
    expect(seen.size).toBe(5)
  })

  it('autoLayout leaves already-placed (dragged) cards where they are', () => {
    const placed = { a: { x: 500, y: 300 } }
    const pos = autoLayout(['a', 'b'], () => cardHeight(2), placed)
    expect(pos.a).toEqual({ x: 500, y: 300 })
    expect(pos.b).toBeDefined()
  })

  it('edgePoint anchors on the rect boundary toward the target', () => {
    const r = { x: 0, y: 0, w: CARD_W, h: 100 }
    // Target straight right: anchor on the right edge, vertically centered.
    const p = edgePoint(r, { x: 400, y: 50 })
    expect(p.x).toBe(CARD_W)
    expect(p.y).toBe(50)
    // Target straight below: anchor on the bottom edge.
    const q = edgePoint(r, { x: CARD_W / 2, y: 300 })
    expect(q.y).toBe(100)
  })
})
