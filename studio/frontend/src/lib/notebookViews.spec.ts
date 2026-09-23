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
