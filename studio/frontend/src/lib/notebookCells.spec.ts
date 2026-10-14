// notebookCells — the notebook editor's cell model (F10): what is saved and what
// is not, the page a result renders as, and the toolbar labels.
import { describe, it, expect } from 'vitest'
import {
  newCellState, blankCell, envelopeToRaw, buildSavePayload, editKey, isStale, pickView, synthesizedPage,
  mismatchInfo, titleOf, ariaTitleOf, summaryOf,
} from './notebookCells'

const sql = (over: Record<string, any> = {}) => newCellState({ id: 'c1', type: 'sql', source: 'SELECT 1', view: 'table', ...over })

describe('newCellState', () => {
  it('opens an empty sql cell’s drawer, previews a markdown cell that has text, and lazy-mounts only a2ui', () => {
    expect(newCellState({ id: 'a', type: 'sql', source: '' }).sourceOpen).toBe(true)
    expect(newCellState({ id: 'a', type: 'sql', source: 'SELECT 1' }).sourceOpen).toBe(false)
    expect(newCellState({ id: 'm', type: 'markdown', source: '# hi' }).mdPreview).toBe(true)
    expect(newCellState({ id: 'm', type: 'markdown', source: '' }).mdPreview).toBe(false)
    expect(newCellState({ id: 'p', type: 'a2ui', page: { title: 't', components: [] } }).visible).toBe(false)
    expect(sql().visible).toBe(true)
  })
})

describe('blankCell / envelopeToRaw', () => {
  it('a new a2ui cell carries a placeholder component, since the server rejects zero components', () => {
    const raw = blankCell('a2ui')
    expect(raw.page?.components).toHaveLength(1)
    expect(blankCell('sql').id).toMatch(/^cell_/)
  })
  it('an AI proposal becomes the same shape a hand-typed cell has', () => {
    expect(envelopeToRaw('x', { type: 'sql', sql: 'SELECT 2', view: 'bar', label: 'L', connection: 'pg' } as any))
      .toEqual({ id: 'x', type: 'sql', source: 'SELECT 2', view: 'bar', viewPinned: true, label: 'L', connection: 'pg' })
    expect(envelopeToRaw('y', { type: 'markdown', markdown: '# t' } as any)).toEqual({ id: 'y', type: 'markdown', source: '# t' })
    expect(envelopeToRaw('z', { type: 'sql', sql: 'SELECT 3' } as any)).toMatchObject({ view: 'table', viewPinned: false })
  })
})

describe('buildSavePayload', () => {
  it('carries source, view, label and connection, and never a result, an editor or UI state', () => {
    const c = sql({ label: 'by region', connection: 'pg' })
    c.result = { columns: ['x'], types: ['INTEGER'], rows: [[1]] }
    c.sourceOpen = true
    const p = buildSavePayload('nb1', 'T', [c])
    expect(p).toEqual({
      id: 'nb1', title: 'T',
      cells: [{ id: 'c1', type: 'sql', collapsed: false, source: 'SELECT 1', view: 'table', viewPinned: false, label: 'by region', connection: 'pg' }],
    })
  })
  it('leaves connection out when unset, and a2ui carries its page only', () => {
    const a = newCellState({ id: 'p', type: 'a2ui', page: { title: 't', components: [{ id: 'k', type: 'text', text: 'x' }] } })
    const p = buildSavePayload('nb1', 'T', [sql(), a])
    expect(p.cells[0]).not.toHaveProperty('connection')
    expect(p.cells[1]).toEqual({ id: 'p', type: 'a2ui', collapsed: false, page: a.page })
  })
})

describe('editKey', () => {
  it('ignores the auto-chosen view of an unpinned sql cell: running a cell is not an edit', () => {
    const c = sql()
    const before = editKey(buildSavePayload('nb', 'T', [c]))
    c.view = 'bar' // picked by a run
    expect(editKey(buildSavePayload('nb', 'T', [c]))).toBe(before)
    c.viewPinned = true // pinned by the person: now it counts
    expect(editKey(buildSavePayload('nb', 'T', [c]))).not.toBe(before)
  })
})

describe('isStale / pickView', () => {
  it('a cell is stale once its source differs from what last ran', () => {
    const c = sql()
    expect(isStale(c)).toBe(false) // never run
    c.lastRunSource = 'SELECT 1'
    expect(isStale(c)).toBe(false)
    c.source = 'SELECT 2'
    expect(isStale(c)).toBe(true)
  })
  it('pickView honors and pins the choice, even when it does not fit', () => {
    const c = sql()
    pickView(c, 'line')
    expect(c).toMatchObject({ view: 'line', viewPinned: true })
  })
})

describe('synthesizedPage / mismatchInfo', () => {
  it('no page until there is a successful result', () => {
    const c = sql()
    expect(synthesizedPage(c)).toBeNull()
    c.result = { columns: [], types: [], rows: [], error: 'boom' }
    expect(synthesizedPage(c)).toBeNull()
  })
  it('a table view stamps the DuckDB types on one component, and puts the label on the component only', () => {
    const c = sql({ label: 'Totals' })
    c.result = { columns: ['id', 'amt'], types: ['BIGINT', 'DECIMAL(12,2)'], rows: [['1', 2.5]] }
    expect(synthesizedPage(c)).toEqual({
      title: '',
      components: [{ id: 'c', type: 'table', label: 'Totals', columns: ['id', 'amt'], rows: [['1', 2.5]], types: ['BIGINT', 'DECIMAL(12,2)'] }],
    })
  })
  it('a stat view needs a label: falls back to the first column, then "Result"', () => {
    const c = sql({ view: 'stat' })
    c.result = { columns: ['n'], types: ['INTEGER'], rows: [[7]] }
    expect((synthesizedPage(c)!.components[0] as any)).toMatchObject({ type: 'stat', label: 'n', value: 7 })
    c.result = { columns: [], types: [], rows: [] }
    expect((synthesizedPage(c)!.components[0] as any).label).toBe('Result')
  })
  it('explains a pinned view that does not fit the result, and offers the ones that do', () => {
    const c = sql({ view: 'line', viewPinned: true })
    c.result = { columns: ['name'], types: ['VARCHAR'], rows: [['a'], ['b']] }
    const info = mismatchInfo(c)!
    expect(info.label).toBe('LINE')
    expect(info.shapeLine).toBe('this query returned 2 rows, 1 column (numeric: none)')
    expect(info.fits).toContain('table')
    expect(info.fits).not.toContain('line')
    c.view = 'table'
    expect(mismatchInfo(c)).toBeNull()
  })
})

describe('titles', () => {
  it('title falls back by type; the aria title is capped at 60 characters', () => {
    expect(titleOf(newCellState({ id: 'm', type: 'markdown', source: '' }))).toBeTruthy()
    const long = sql({ source: 'SELECT ' + 'a_very_long_column_name, '.repeat(10) })
    const t = ariaTitleOf(long)
    expect(t.length).toBeLessThanOrEqual(61)
    expect(t.endsWith('…')).toBe(true)
  })
  it('summaryOf counts lines and shows the first meaningful ones', () => {
    expect(summaryOf(sql({ source: '-- note\nSELECT 1\nFROM t' }))).toBe('Show SQL · 3 lines · SELECT 1 FROM t')
    expect(summaryOf(sql({ source: 'SELECT 1' }))).toBe('Show SQL · 1 line · SELECT 1')
  })
})

describe('KINETIC views and motion', () => {
  it('a chart view hands the result over as columns + rows, with the cell motion', () => {
    const c = sql({ view: 'sankey', label: 'Routing', motion: { easing: 'expo', speed: 2 } })
    c.result = { columns: ['src', 'dst', 'w'], types: ['VARCHAR', 'VARCHAR', 'INTEGER'], rows: [['a', 'b', 3]] }
    expect(synthesizedPage(c)!.components[0]).toEqual({
      id: 'c', type: 'sankeyChart', label: 'Routing', columns: ['src', 'dst', 'w'], rows: [['a', 'b', 3]], motion: { easing: 'expo', speed: 2 },
    })
  })
  it('table and stat views never carry motion; bar does', () => {
    const c = sql({ motion: { easing: 'steps' } })
    c.result = { columns: ['k', 'v'], types: ['VARCHAR', 'INTEGER'], rows: [['a', 1]] }
    expect(synthesizedPage(c)!.components[0]).not.toHaveProperty('motion')
    c.view = 'bar'
    expect(synthesizedPage(c)!.components[0]).toMatchObject({ type: 'barChart', motion: { easing: 'steps' } })
  })
  it('saves motion when set and leaves it out otherwise', () => {
    const withMotion = buildSavePayload('nb', 't', [sql({ view: 'radar', motion: { easing: 'none' } })])
    expect(withMotion.cells[0]).toMatchObject({ view: 'radar', motion: { easing: 'none' } })
    expect(buildSavePayload('nb', 't', [sql()]).cells[0]).not.toHaveProperty('motion')
  })
})
