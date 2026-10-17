import { describe, it, expect } from 'vitest'
import { readDrill, drillColumnProblem, drillCandidate } from './drill'
import { coerceParam } from './artifactParams'

const p = (o: Partial<ArtifactParamT> & { type: ArtifactParamT['type'] }): ArtifactParamT => ({ name: 'x', default: '', ...o })

describe('drill helpers', () => {
  it('reads a well-formed drill only', () => {
    expect(readDrill({ drill: { param: 'region', column: 'region' } })).toEqual({ param: 'region', column: 'region' })
    expect(readDrill({})).toBeNull()
    expect(readDrill({ drill: { param: 'r' } })).toBeNull()
    expect(readDrill({ drill: { param: 1, column: 'c' } })).toBeNull()
  })

  it('says when the drill column is not in the result, and waits for a result', () => {
    const spec = { param: 'region', column: 'nope' }
    expect(drillColumnProblem(spec, ['region', 'revenue'])).toContain('"nope"')
    expect(drillColumnProblem(spec, ['region', 'revenue'])).toContain('region, revenue')
    expect(drillColumnProblem(spec, [])).toBe('')
    expect(drillColumnProblem(spec, null)).toBe('')
    expect(drillColumnProblem({ param: 'a', column: 'region' }, ['region'])).toBe('')
  })

  it('reads a cell as a candidate, losslessly, and refuses NULL and nested values', () => {
    expect(drillCandidate(p({ type: 'text' }), 42, 'c')).toEqual({ ok: true, raw: '42' })
    expect(drillCandidate(p({ type: 'date' }), '2026-03-01 00:00:00', 'c')).toEqual({ ok: true, raw: '2026-03-01' })
    expect(drillCandidate(p({ type: 'integer' }), 7, 'c')).toEqual({ ok: true, raw: 7 })
    expect(drillCandidate(p({ type: 'text' }), null, 'c').ok).toBe(false)
    expect(drillCandidate(p({ type: 'text' }), { a: 1 }, 'c').ok).toBe(false)
  })

  it('the candidate still has to pass the parameter: a value outside a static enum is refused', () => {
    const e = p({ type: 'enum', options: ['East', 'West'] })
    const c = drillCandidate(e, 'North', 'region')
    expect(c.ok && coerceParam(e, c.raw).ok).toBe(false)
    const ok = drillCandidate(e, 'West', 'region')
    expect(ok.ok && coerceParam(e, ok.raw).ok).toBe(true)
  })
})
