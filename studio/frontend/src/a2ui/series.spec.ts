// series — the KINETIC charts' data shaping, from the columns + rows a sql
// binding (or inline data) provides.
import { describe, it, expect } from 'vitest'
import { areaPath, heatLevel, polar, seriesClass, splinePath, toFlows, toMatrix, toRings, toSeries } from './series'

describe('toSeries', () => {
  it('takes the first text column as labels and every numeric column as a series', () => {
    const d = toSeries({ columns: ['hour', 'primary', 'baseline'], rows: [['00', 28, 18], ['03', 44, '25']] })
    expect(d.labels).toEqual(['00', '03'])
    expect(d.series).toEqual([{ name: 'primary', values: [28, 44] }, { name: 'baseline', values: [18, 25] }])
  })
  it('finds the label column wherever it is, and falls back to column 0 when all are numeric', () => {
    expect(toSeries({ columns: ['v', 'name'], rows: [[1, 'a']] }).labels).toEqual(['a'])
    expect(toSeries({ columns: ['year', 'v'], rows: [[2024, 5]] })).toEqual({ labels: ['2024'], series: [{ name: 'v', values: [5] }] })
  })
  it('names columns by index when there are none', () => {
    expect(toSeries({ columns: [], rows: [['a', 1]] }).series[0].name).toBe('1')
  })
})

describe('toRings / toFlows / toMatrix', () => {
  it('rings read label, value[, max], default the max, and cap at six', () => {
    const rings = toRings({ columns: [], rows: [['a', 50], ['b', 3, 4], ...Array.from({ length: 6 }, () => ['x', 1])] }, 200)
    expect(rings[0]).toEqual({ label: 'a', value: 50, max: 200 })
    expect(rings[1]).toEqual({ label: 'b', value: 3, max: 4 })
    expect(rings).toHaveLength(6)
    expect(toRings({ columns: [], rows: [['a', 1]] })[0].max).toBe(100)
  })
  it('flows keep first-seen node order, total each node, and drop non-positive weights', () => {
    const f = toFlows({ columns: [], rows: [['in', 'h100', 32], ['in', 'edge', 14], ['cache', 'h100', 12], ['x', 'y', 0]] })
    expect(f.sources).toEqual([{ name: 'in', total: 46 }, { name: 'cache', total: 12 }])
    expect(f.targets).toEqual([{ name: 'h100', total: 44 }, { name: 'edge', total: 14 }])
    expect(f.flows).toEqual([{ from: 0, to: 0, weight: 32 }, { from: 0, to: 1, weight: 14 }, { from: 1, to: 0, weight: 12 }])
  })
  it('matrix pivots long rows into a grid with gaps as null', () => {
    const m = toMatrix({ columns: [], rows: [['A', '00h', 35], ['A', '04h', 58], ['B', '04h', 42]] })
    expect(m.rowLabels).toEqual(['A', 'B'])
    expect(m.colLabels).toEqual(['00h', '04h'])
    expect(m.cells).toEqual([[35, 58], [null, 42]])
    expect([m.min, m.max]).toEqual([35, 58])
  })
  it('heat levels quantize into four buckets', () => {
    expect([0, 24, 25, 60, 100].map((v) => heatLevel(v, 0, 100))).toEqual([0, 0, 1, 2, 3])
    expect(heatLevel(5, 5, 5)).toBe(3)
  })
})

describe('geometry', () => {
  it('splinePath passes through every point; areaPath closes to the baseline', () => {
    const pts = [{ x: 0, y: 10 }, { x: 50, y: 0 }, { x: 100, y: 10 }]
    const d = splinePath(pts)
    expect(d.startsWith('M 0.0 10.0')).toBe(true)
    expect(d).toContain('50.0 0.0')
    expect(d.endsWith('100.0 10.0')).toBe(true)
    expect(areaPath(pts, 20).endsWith('L 100.0 20 L 0.0 20 Z')).toBe(true)
    expect(areaPath([pts[0]], 20)).toBe('')
  })
  it('polar starts at 12 o\'clock and clamps overshoot', () => {
    expect(polar(0, 4, 100, 100, 50, 50, 40)).toEqual({ x: 50, y: 10 })
    expect(polar(0, 4, 1000, 100, 50, 50, 40).y).toBeCloseTo(6)
  })
  it('series classes cycle through the six palette roles', () => {
    expect(seriesClass(0)).toBe('a2ui-series-0')
    expect(seriesClass(7)).toBe('a2ui-series-1')
  })
})
