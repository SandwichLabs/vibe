import { describe, it, expect } from 'vitest'
import { sqlSummary, cellTitle, cellMeta } from './cellSummary'

describe('sqlSummary', () => {
  it('returns 0 lines and an empty firstLine for empty source', () => {
    expect(sqlSummary('')).toEqual({ lines: 0, firstLine: '' })
  })

  it('skips blank lines and comment lines, joining the rest into firstLine, but counts every line', () => {
    const s = sqlSummary('\n-- a comment\n\nSELECT 1\nFROM t')
    expect(s.firstLine).toBe('SELECT 1 FROM t')
    expect(s.lines).toBe(4)
  })

  it('treats \\r\\n the same as \\n', () => {
    const crlf = sqlSummary('-- c\r\nSELECT 1\r\nFROM t')
    const lf = sqlSummary('-- c\nSELECT 1\nFROM t')
    expect(crlf).toEqual(lf)
  })

  it('collapses internal whitespace per line and joins multiple lines with a single space', () => {
    expect(sqlSummary('SELECT   1,    2\nFROM t').firstLine).toBe('SELECT 1, 2 FROM t')
  })

  it('gives an empty firstLine but a nonzero line count for a comment-only source', () => {
    const s = sqlSummary('-- just a comment\n-- another one')
    expect(s.firstLine).toBe('')
    expect(s.lines).toBe(2)
  })
})

describe('cellTitle', () => {
  it('prefers an sql label over the source', () => {
    expect(cellTitle({ type: 'sql', label: 'Revenue', source: 'SELECT 1' })).toBe('Revenue')
  })

  it('falls back to the first real line when an sql cell has no label', () => {
    expect(cellTitle({ type: 'sql', source: '-- note\nSELECT region FROM sales' })).toBe('SELECT region FROM sales')
  })

  it('picks the first ATX heading anywhere in a markdown cell', () => {
    expect(cellTitle({ type: 'markdown', source: 'intro line\n## Sales' })).toBe('Sales')
  })

  it('falls back to the first non-blank line when markdown has no heading', () => {
    expect(cellTitle({ type: 'markdown', source: '\n  Just some text\nmore' })).toBe('Just some text')
  })

  it('uses page.title for a2ui', () => {
    expect(cellTitle({ type: 'a2ui', page: { title: 'KPIs' } })).toBe('KPIs')
  })

  it('returns empty string when there is nothing to show', () => {
    expect(cellTitle({ type: 'sql', source: '' })).toBe('')
    expect(cellTitle({ type: 'markdown', source: '' })).toBe('')
    expect(cellTitle({ type: 'a2ui' })).toBe('')
  })
})

describe('cellMeta', () => {
  it('reports error for a failed run', () => {
    expect(cellMeta({ type: 'sql', result: { rows: [], error: 'boom' } })).toBe('error')
  })

  it('reports singular/plural row counts', () => {
    expect(cellMeta({ type: 'sql', result: { rows: [[1]] } })).toBe('1 row')
    expect(cellMeta({ type: 'sql', result: { rows: [[1], [2], [3]] } })).toBe('3 rows')
  })

  it('is empty when the cell has not run', () => {
    expect(cellMeta({ type: 'sql', result: null })).toBe('')
  })

  it('reports the component count for a2ui', () => {
    expect(cellMeta({ type: 'a2ui', page: { components: [{}, {}] } })).toBe('2 components')
  })

  it('is empty for markdown', () => {
    expect(cellMeta({ type: 'markdown' })).toBe('')
  })
})
