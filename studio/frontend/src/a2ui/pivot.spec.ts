import { describe, it, expect } from 'vitest'
import { buildPivot, keyText } from './pivot'

const cols = ['region', 'month', 'revenue']
const types = ['VARCHAR', 'VARCHAR', 'BIGINT']
const spec = { rows: ['region'], columns: 'month', value: 'revenue' }

function grid(rows: any[][], s = spec, c = cols, t = types) {
  const r = buildPivot(c, t, rows, s)
  if (!r.ok) throw new Error(r.error)
  return r.grid
}

describe('buildPivot', () => {
  it('lays a long-form result out in order of first appearance', () => {
    const g = grid([['West', 'Feb', 5], ['East', 'Jan', 1], ['West', 'Jan', 2], ['East', 'Feb', 3]])
    expect(g.colKeys).toEqual(['Feb', 'Jan'])
    expect(g.rows.map((r) => r.keys)).toEqual([['West'], ['East']])
    expect(g.rows[0].cells.map((c) => c?.text)).toEqual(['5', '2'])
    expect(g.rows[1].cells.map((c) => c?.text)).toEqual(['3', '1'])
    expect(g.rows[1].cells[0]?.src).toBe(3) // the result row a drill reads
  })

  it('leaves a missing cell empty (ragged input)', () => {
    const g = grid([['East', 'Jan', 1], ['West', 'Feb', 2]])
    expect(g.rows[0].cells.map((c) => c?.text ?? null)).toEqual(['1', null])
    expect(g.rows[1].cells.map((c) => c?.text ?? null)).toEqual([null, '2'])
  })

  it('refuses two rows for one cell, naming it and the columns to group by', () => {
    const r = buildPivot(cols, types, [['East', 'Jan', 1], ['East', 'Jan', 2]], spec)
    expect(r.ok).toBe(false)
    if (!r.ok) {
      expect(r.error).toContain('East / Jan')
      expect(r.error).toContain('Group the query by "region", "month"')
    }
  })

  it('shows NULL keys as (blank), keeps them apart from empty text, and takes ROLLUP rows as returned', () => {
    const g = grid([
      ['East', 'Jan', 1], ['East', null, 3], [null, 'Jan', 4], [null, null, 9], ['', 'Jan', 7], ['(blank)', 'Jan', 8],
    ])
    expect(g.colKeys).toEqual(['Jan', '(blank)'])
    expect(g.rows.map((r) => r.keys[0])).toEqual(['East', '(blank)', '(empty)', '(blank)'])
    // the NULL row and the text "(blank)" row are two rows, not one
    expect(g.rows).toHaveLength(4)
    expect(g.rows[1].cells.map((c) => c?.text)).toEqual(['4', '9'])
    expect(keyText(null)).toBe('(blank)')
    expect(keyText('')).toBe('(empty)')
  })

  it('takes several row-key columns', () => {
    const g = grid([['US', 'West', 'Jan', 1], ['US', 'East', 'Jan', 2]], { rows: ['country', 'region'], columns: 'month', value: 'revenue' },
      ['country', 'region', 'month', 'revenue'], ['VARCHAR', 'VARCHAR', 'VARCHAR', 'BIGINT'])
    expect(g.rowColumns).toEqual(['country', 'region'])
    expect(g.rows.map((r) => r.keys)).toEqual([['US', 'West'], ['US', 'East']])
  })

  it('names a column that is not in the result', () => {
    const r = buildPivot(cols, types, [], { rows: ['region'], columns: 'quarter', value: 'revenue' })
    expect(r.ok).toBe(false)
    if (!r.ok) expect(r.error).toContain('"quarter"')
  })

  it('keeps markup and long text as plain strings', () => {
    const long = 'x'.repeat(500)
    const g = grid([['<img src=x onerror=alert(1)>', long, 1]])
    expect(g.rows[0].keys[0]).toBe('<img src=x onerror=alert(1)>')
    expect(g.colKeys[0]).toBe(long)
  })

  it('handles 500 cells', () => {
    const rows: any[][] = []
    for (let i = 0; i < 25; i++) for (let j = 0; j < 20; j++) rows.push([`r${i}`, `c${j}`, i * j])
    const g = grid(rows)
    expect(g.rows).toHaveLength(25)
    expect(g.colKeys).toHaveLength(20)
    expect(g.rows[24].cells[19]?.text).toBe('456')
  })
})
