// motion — the easing presets and the frame math every animated chart shares.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { animationsEnabled, frameAt, resolveMotion, totalMs } from './motion'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('resolveMotion', () => {
  it('defaults to a staggered spring at 1x', () => {
    expect(resolveMotion(undefined)).toEqual({ easing: 'spring', speed: 1, stagger: true, stiffness: 180, damping: 13, mass: 1 })
  })
  it('clamps numbers and ignores an unknown easing (a hand-built object is never trusted)', () => {
    const m = resolveMotion({ easing: 'wobble' as any, speed: 99, mass: 0, damping: Number.NaN })
    expect(m.easing).toBe('spring')
    expect(m.speed).toBe(4)
    expect(m.mass).toBe(0.1)
    expect(m.damping).toBe(13)
  })
})

describe('frameAt', () => {
  const to = [10, 20, 30]
  for (const easing of ['spring', 'expo', 'cinema', 'steps', 'custom'] as const) {
    it(`${easing}: starts at the origin and lands exactly on the targets`, () => {
      const m = resolveMotion({ easing })
      expect(frameAt([0, 0, 0], to, 0, m)).toEqual([0, 0, 0])
      expect(frameAt([0, 0, 0], to, totalMs(3, m), m)).toEqual(to)
    })
  }
  it('stagger delays each value; without it they move together', () => {
    const on = resolveMotion({ easing: 'expo' })
    const [a, b] = frameAt([0, 0], [100, 100], 40, on)
    expect(a).toBeGreaterThan(b)
    const off = resolveMotion({ easing: 'expo', stagger: false })
    const [c, d] = frameAt([0, 0], [100, 100], 40, off)
    expect(c).toBe(d)
  })
  it('the elastic spring overshoots mid-flight; steps quantize', () => {
    const spring = resolveMotion({ easing: 'spring', stagger: false })
    const samples = Array.from({ length: 40 }, (_, i) => frameAt([0], [100], i * 25, spring)[0])
    expect(Math.max(...samples)).toBeGreaterThan(100)
    const steps = resolveMotion({ easing: 'steps', stagger: false })
    const v = frameAt([0], [120], 200, steps)[0]
    expect(v % 10).toBe(0)
  })
  it('speed scales the duration', () => {
    expect(totalMs(1, resolveMotion({ speed: 2 }))).toBe(totalMs(1, resolveMotion({})) / 2)
  })
})

describe('animationsEnabled', () => {
  it('is off without a browser (jsdom has no matchMedia) and for easing "none"', () => {
    expect(animationsEnabled(resolveMotion({}))).toBe(false)
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    expect(animationsEnabled(resolveMotion({ easing: 'none' }))).toBe(false)
  })
  it('honours prefers-reduced-motion', () => {
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    expect(animationsEnabled(resolveMotion({}))).toBe(false)
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    expect(animationsEnabled(resolveMotion({}))).toBe(true)
  })
})
