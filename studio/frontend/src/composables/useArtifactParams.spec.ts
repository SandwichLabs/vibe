import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope } from 'vue'
import { useArtifactParams } from './useArtifactParams'

const detail = (): ArtifactDetail => ({
  view_kind: 'a2ui', name: 'sales', title: 'Sales', description: '', rev: 'r1', updated_at: '', queries: [],
  params: [
    { name: 'from', type: 'date', default: 'today-90d' },
    { name: 'top', type: 'integer', default: '10', min: 1, max: 100 },
    { name: 'region', type: 'enum', default: '', options: ['East', 'West'] },
  ],
  defaults: { from: '2026-01-01', top: 10, region: '' },
})

function make(d: ArtifactDetail | null = detail()) {
  const scope = effectScope()
  const p = scope.run(() => useArtifactParams(() => d, 400))!
  p.load(d)
  return { p, scope }
}

describe('useArtifactParams', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('starts at the resolved defaults', () => {
    const { p } = make()
    expect({ ...p.values }).toEqual({ from: '2026-01-01', top: 10, region: '' })
    expect(p.isDefault('top')).toBe(true)
  })

  it('commits a pick at once and a typed value after the debounce', () => {
    const { p } = make()
    expect(p.set('region', 'West')?.ok).toBe(true)
    expect(p.values.region).toBe('West')
    p.set('top', '2', true); p.set('top', '25', true)
    expect(p.values.top).toBe(10)
    vi.advanceTimersByTime(399)
    expect(p.values.top).toBe(10)
    vi.advanceTimersByTime(1)
    expect(p.values.top).toBe(25) // one commit, for the last value typed
    expect(p.display('top')).toBe('25')
  })

  it('a value that does not validate shows an error and changes nothing', () => {
    const { p } = make()
    expect(p.set('top', '500')?.ok).toBe(false)
    expect(p.values.top).toBe(10)
    expect(p.error('top')).toMatch(/at most 100/)
    expect(p.display('top')).toBe('500')
    p.set('region', 'North')
    expect(p.values.region).toBe('')
    expect(p.error('region')).not.toBe('')
    expect(p.set('nope', 'x')).toBeUndefined() // an unknown name is ignored
    expect('nope' in p.values).toBe(false)
  })

  it('Reset restores the default and clears the error and the draft', () => {
    const { p } = make()
    p.set('top', '50')
    expect(p.isDefault('top')).toBe(false)
    p.reset('top')
    expect(p.values.top).toBe(10)
    expect(p.isDefault('top')).toBe(true)
    p.set('top', '0')
    p.reset('top')
    expect(p.error('top')).toBe('')
    expect(p.display('top')).toBe(10)
  })

  it('a reload keeps still-valid values and resets the rest', () => {
    const { p } = make()
    p.set('top', '40'); p.set('region', 'East')
    const next = detail()
    next.params[2] = { name: 'region', type: 'enum', default: '', options: ['North'] }
    p.load(next, true)
    expect(p.values.top).toBe(40)
    expect(p.values.region).toBe('')
  })

  it('a value still at its old default follows an edited default; a changed one is kept', () => {
    const { p } = make()
    p.set('top', '40')
    const next = detail()
    next.defaults = { from: '2026-02-02', top: 20, region: '' }
    p.load(next, true)
    expect(p.values.from).toBe('2026-02-02')
    expect(p.values.top).toBe(40)
  })
})
