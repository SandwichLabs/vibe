import { describe, it, expect } from 'vitest'
import { describeShape, viewFit, bestView } from './notebookViews'

describe('describeShape', () => {
  it('classifies numeric vs label columns from sampled row values', () => {
    const shape = describeShape(['region', 'revenue'], [['North', 100], ['South', 200]])
    expect(shape.numeric).toEqual(['revenue'])
    expect(shape.label).toEqual(['region'])
    expect(shape.firstDistinct).toBe(true)
  })

  it('detects a non-distinct first column', () => {
    const shape = describeShape(['month', 'x'], [['Jan', 1], ['Jan', 2]])
    expect(shape.firstDistinct).toBe(false)
  })

  // Classifies by the declared DuckDB type when given, closing the "mixed
  // column" edge case value-sampling alone had —
  // a HUGEINT/DECIMAL column whose sampled value is out of JS's safe
  // integer range crosses the wire as a string (lib/formatCell's wire
  // contract), which plain `typeof v === 'number'` sampling would wrongly
  // call a label column.
  it('classifies by types when given, not by sampling — even when the sampled value is a big-int string', () => {
    const shape = describeShape(['label', 'total'], [['North', '18014398509481982']], ['VARCHAR', 'HUGEINT'])
    expect(shape.numeric).toEqual(['total'])
    expect(shape.label).toEqual(['label'])
  })

  it('classifies a DECIMAL/BIGINT/DOUBLE type as numeric, and DATE/VARCHAR/nested as label', () => {
    const shape = describeShape(
      ['amt', 'name', 'd', 'tags'],
      [[1, 'x', '2025-01-01', ['a']]],
      ['DECIMAL(12,2)', 'VARCHAR', 'DATE', 'VARCHAR[]'],
    )
    expect(shape.numeric).toEqual(['amt'])
    expect(shape.label).toEqual(['name', 'd', 'tags'])
  })

  it('falls back to sampling for a column with no type at hand', () => {
    // types has fewer entries than columns — untyped columns still sample.
    const shape = describeShape(['a', 'b'], [[1, 'x']], ['INTEGER'])
    expect(shape.numeric).toEqual(['a'])
    expect(shape.label).toEqual(['b'])
  })

  it('falls back to sampling entirely when types is omitted, unchanged from before', () => {
    const shape = describeShape(['region', 'revenue'], [['North', 100]])
    expect(shape.numeric).toEqual(['revenue'])
  })
})

describe('viewFit', () => {
  it('table always fits when there are columns', () => {
    expect(viewFit('table', describeShape(['a'], [[1]])).ok).toBe(true)
  })

  it('bar/line need a numeric column and two columns', () => {
    const textOnly = describeShape(['a', 'b'], [['x', 'y']])
    expect(viewFit('bar', textOnly).ok).toBe(false)
    expect(viewFit('bar', textOnly).why).toContain('numeric')

    const oneCol = describeShape(['a'], [[1]])
    expect(viewFit('bar', oneCol).ok).toBe(false)
    expect(viewFit('bar', oneCol).why).toContain('two columns')

    const good = describeShape(['region', 'revenue'], [['North', 100]])
    expect(viewFit('bar', good).ok).toBe(true)
  })

  it('stat needs exactly one row', () => {
    expect(viewFit('stat', describeShape(['n'], [])).ok).toBe(false)
    expect(viewFit('stat', describeShape(['n'], [[1], [2]])).ok).toBe(false)
    expect(viewFit('stat', describeShape(['n'], [[1]])).ok).toBe(true)
  })
})

describe('bestView', () => {
  it('picks stat for a single numeric cell', () => {
    expect(bestView(describeShape(['n'], [[42]]))).toBe('stat')
  })

  it('picks bar for a group-by shape with a plain label', () => {
    const shape = describeShape(['region', 'revenue'], [['North', 100], ['South', 200], ['East', 50]])
    expect(bestView(shape)).toBe('bar')
  })

  it('picks line when the label column looks ordered (dates/months)', () => {
    const shape = describeShape(['month', 'revenue'], [['2026-01', 100], ['2026-02', 200]])
    expect(bestView(shape)).toBe('line')
  })

  it('falls back to table when rows are not distinct or too many to chart', () => {
    const dup = describeShape(['month', 'revenue'], [['2026-01', 100], ['2026-01', 200]])
    expect(bestView(dup)).toBe('table')

    const many = describeShape(
      ['region', 'revenue'],
      Array.from({ length: 30 }, (_, i) => [`r${i}`, i]),
    )
    expect(bestView(many)).toBe('table')
  })

  it('falls back to table for a single row with more than one column', () => {
    const shape = describeShape(['a', 'b'], [[1, 2]])
    expect(bestView(shape)).toBe('table')
  })
})

describe('viewFit — KINETIC chart views (mirrors internal/agents)', () => {
  const series = describeShape(['region', 'a', 'b'], [['x', 1, 2], ['y', 3, 4]])
  it('area, columns and rings take a label then numbers, like bar/line', () => {
    for (const v of ['area', 'columns', 'rings'] as const) expect(viewFit(v, series).ok).toBe(true)
    expect(viewFit('area', describeShape(['name'], [['Ada']])).ok).toBe(false)
  })
  it('radar needs three axes; rings draw at most six', () => {
    expect(viewFit('radar', series).why).toMatch(/three rows/)
    const seven = describeShape(['k', 'v'], Array.from({ length: 7 }, (_, i) => [`k${i}`, i]))
    expect(viewFit('radar', seven).ok).toBe(true)
    expect(viewFit('rings', seven).why).toMatch(/at most 6 rings/)
  })
  it('sankey and heatmap need three columns with a numeric third', () => {
    const triple = describeShape(['src', 'dst', 'w'], [['a', 'b', 3]])
    expect(viewFit('sankey', triple).ok).toBe(true)
    expect(viewFit('heatmap', triple).ok).toBe(true)
    expect(viewFit('sankey', describeShape(['a', 'b', 'c'], [['x', 'y', 'z']])).why).toMatch(/source, target/)
    expect(viewFit('heatmap', series).ok).toBe(true)
  })
  it('bestView never guesses a KINETIC chart', () => {
    expect(bestView(describeShape(['src', 'dst', 'w'], [['a', 'b', 3]]))).toBe('table')
  })
})
