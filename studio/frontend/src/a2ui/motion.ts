// A2UI motion — the easing presets every animated chart shares (ported from the
// KINETIC motion chart library's PHYSICS_PRESETS). A component's "motion" field
// (internal/a2ui's closed motion object, validated server side) only ever picks
// a preset and tunes numbers that feed this math; nothing here reaches a style
// attribute as a string.

type Easing = 'spring' | 'expo' | 'cinema' | 'steps' | 'custom' | 'none'

export const EASINGS: Easing[] = ['spring', 'expo', 'cinema', 'steps', 'custom', 'none']

export const EASING_LABELS: Record<Easing, string> = {
  spring: 'Elastic spring',
  expo: 'Snappy expo',
  cinema: 'Smooth cinema',
  steps: 'Mechanical steps',
  custom: 'Custom spring',
  none: 'No animation',
}

export interface MotionT {
  easing?: Easing
  speed?: number
  stagger?: boolean
  stiffness?: number
  damping?: number
  mass?: number
}

export interface ResolvedMotion {
  easing: Easing
  speed: number
  stagger: boolean
  stiffness: number
  damping: number
  mass: number
}

// The same bounds internal/a2ui's motionRanges enforces; clamped again here so
// a hand-built (unvalidated, e.g. synthesized) motion object can't stall or
// explode an animation either.
const RANGES = { speed: [0.25, 4], stiffness: [10, 500], damping: [1, 100], mass: [0.1, 10] } as const

function clampNum(v: unknown, [lo, hi]: readonly [number, number], dflt: number): number {
  const n = typeof v === 'number' ? v : Number.NaN
  if (!Number.isFinite(n)) return dflt
  return Math.min(Math.max(n, lo), hi)
}

export function resolveMotion(m?: MotionT | null): ResolvedMotion {
  const easing = m?.easing && EASINGS.includes(m.easing) ? m.easing : 'spring'
  return {
    easing,
    speed: clampNum(m?.speed, RANGES.speed, 1),
    stagger: m?.stagger ?? true,
    stiffness: clampNum(m?.stiffness, RANGES.stiffness, 180),
    damping: clampNum(m?.damping, RANGES.damping, 13),
    mass: clampNum(m?.mass, RANGES.mass, 1),
  }
}

// solveSpring is a damped harmonic oscillator's step response, sampled at
// t in [0,1] of the animation (time scaled so a default spring settles).
function solveSpring(t: number, stiffness: number, damping: number, mass: number): number {
  if (t <= 0) return 0
  if (t >= 1) return 1
  const w0 = Math.sqrt(stiffness / mass)
  const zeta = damping / (2 * Math.sqrt(stiffness * mass))
  if (zeta < 1) {
    const wd = w0 * Math.sqrt(1 - zeta * zeta)
    return 1 - Math.exp(-zeta * w0 * t) * (Math.cos(wd * t) + ((zeta * w0) / wd) * Math.sin(wd * t))
  }
  return 1 - (1 + w0 * t) * Math.exp(-w0 * t)
}

// ease maps linear progress t in [0,1] to eased progress. Springs overshoot
// (values above 1 mid-flight), which is the point; each ends at exactly 1.
function ease(t: number, m: ResolvedMotion): number {
  if (t <= 0) return 0
  if (t >= 1) return 1
  switch (m.easing) {
    case 'spring':
      return Math.pow(2, -10 * t) * Math.sin((t * 10 - 0.75) * ((2 * Math.PI) / 3)) + 1
    case 'custom':
      return solveSpring(t, m.stiffness, m.damping, m.mass)
    case 'expo':
      return 1 - Math.pow(2, -10 * t)
    case 'cinema':
      return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2
    case 'steps':
      return Math.floor(t * 12) / 12
    default:
      return 1
  }
}

// durationMs is one value's animation length; staggerMs the delay between
// consecutive values when stagger is on. Springs get longer to settle.
function durationMs(m: ResolvedMotion): number {
  const base = m.easing === 'spring' || m.easing === 'custom' ? 1150 : 750
  return base / m.speed
}

function staggerMs(m: ResolvedMotion): number {
  return m.stagger ? 55 / m.speed : 0
}

// animationsEnabled is false when the person asked the OS for reduced motion,
// and outside a real browser (no matchMedia or requestAnimationFrame, e.g.
// jsdom), where charts draw their final frame at once.
export function animationsEnabled(m: ResolvedMotion): boolean {
  if (m.easing === 'none') return false
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  if (typeof window.requestAnimationFrame !== 'function') return false
  return !window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// frameAt computes every value's position `elapsed` ms into an animation from
// `from` to `to`, honouring stagger. Pure, so the specs can check it directly.
export function frameAt(from: number[], to: number[], elapsed: number, m: ResolvedMotion): number[] {
  const dur = durationMs(m)
  const step = staggerMs(m)
  return to.map((target, i) => {
    const start = from[i] ?? 0
    const local = Math.max(0, elapsed - i * step)
    return start + (target - start) * ease(Math.min(local / dur, 1), m)
  })
}

export function totalMs(count: number, m: ResolvedMotion): number {
  return durationMs(m) + staggerMs(m) * Math.max(0, count - 1)
}
