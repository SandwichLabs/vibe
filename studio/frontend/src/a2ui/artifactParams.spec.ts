import { describe, it, expect } from 'vitest'
import { coerceParam, isRealDate } from './artifactParams'

const p = (o: Partial<ArtifactParamT> & { type: ArtifactParamT['type'] }): ArtifactParamT => ({ name: 'x', default: '', ...o })

describe('isRealDate', () => {
  it('accepts real calendar dates only', () => {
    expect(isRealDate('2026-02-28')).toBe(true)
    expect(isRealDate('2024-02-29')).toBe(true)
    expect(isRealDate('2026-02-30')).toBe(false)
    expect(isRealDate('2026-2-3')).toBe(false)
    expect(isRealDate('2026-02-03T00:00')).toBe(false)
    expect(isRealDate('')).toBe(false)
  })
})

describe('coerceParam', () => {
  it('text: length in characters, cap 1000', () => {
    expect(coerceParam(p({ type: 'text', max_length: 3 }), 'abc')).toEqual({ ok: true, value: 'abc' })
    expect(coerceParam(p({ type: 'text', max_length: 3 }), 'abcd').ok).toBe(false)
    expect(coerceParam(p({ type: 'text' }), 'x'.repeat(1001)).ok).toBe(false)
    expect(coerceParam(p({ type: 'text' }), 7).ok).toBe(false)
  })
  it('integer: whole numbers in range, from a typed string', () => {
    const q = p({ type: 'integer', min: 1, max: 100 })
    expect(coerceParam(q, '10')).toEqual({ ok: true, value: 10 })
    expect(coerceParam(q, ' 10 ')).toEqual({ ok: true, value: 10 })
    for (const bad of ['1.5', '0', '101', '', 'abc', '1e3', 'NaN', Infinity, 2 ** 60]) expect(coerceParam(q, bad).ok, String(bad)).toBe(false)
  })
  it('number: decimals and exponents, never NaN or Infinity', () => {
    const q = p({ type: 'number' })
    expect(coerceParam(q, '0.5')).toEqual({ ok: true, value: 0.5 })
    expect(coerceParam(q, '-2e3')).toEqual({ ok: true, value: -2000 })
    for (const bad of ['', 'Infinity', 'NaN', '0x10', '1,5', '.5']) expect(coerceParam(q, bad).ok, bad).toBe(false)
  })
  it('date and boolean take only their own shapes', () => {
    expect(coerceParam(p({ type: 'date' }), '2026-03-01').ok).toBe(true)
    expect(coerceParam(p({ type: 'date' }), '2026-13-01').ok).toBe(false)
    expect(coerceParam(p({ type: 'boolean' }), true).ok).toBe(true)
    expect(coerceParam(p({ type: 'boolean' }), 'true').ok).toBe(false)
  })
  it('enum: a static list takes only its options; a query-fed one any short string', () => {
    const fixed = p({ type: 'enum', options: ['a', 'b'] })
    expect(coerceParam(fixed, 'a').ok).toBe(true)
    expect(coerceParam(fixed, 'c').ok).toBe(false)
    expect(coerceParam(p({ type: 'enum', options_query: 'regions' }), 'West').ok).toBe(true)
    expect(coerceParam(p({ type: 'enum', options_query: 'regions' }), 'x'.repeat(1001)).ok).toBe(false)
  })
})
