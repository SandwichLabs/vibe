// Parameter values for a data artifact, checked on this side before they reach
// the RunArtifactQuery binding. Go re-validates every value against the manifest
// (internal/artifacts), so this is the courtesy layer: it gives an inline error
// for a bad value and keeps one from ever being sent. Whatever came from the DOM
// (an input's string) or anywhere else is a candidate here, never a value.

export type ParamValue = string | number | boolean

export type Coerced = { ok: true; value: ParamValue } | { ok: false; error: string }

const MAX_TEXT = 1000
const MAX_SAFE = 2 ** 53
const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/
const DECIMAL = /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/
const INTEGER = /^-?\d+$/

const bad = (error: string): Coerced => ({ ok: false, error })

// isRealDate: YYYY-MM-DD that exists on the calendar (2026-02-30 does not).
export function isRealDate(s: string): boolean {
  if (!ISO_DATE.test(s)) return false
  const [y, m, d] = s.split('-').map(Number)
  const t = new Date(Date.UTC(y, m - 1, d))
  return t.getUTCFullYear() === y && t.getUTCMonth() === m - 1 && t.getUTCDate() === d
}

function coerceNumeric(p: ArtifactParamT, raw: unknown): Coerced {
  let n: number
  if (typeof raw === 'number') n = raw
  else if (typeof raw === 'string' && (p.type === 'integer' ? INTEGER : DECIMAL).test(raw.trim())) n = Number(raw.trim())
  else return bad(p.type === 'integer' ? 'Enter a whole number.' : 'Enter a number.')
  if (!Number.isFinite(n)) return bad('Enter a number.')
  if (p.type === 'integer' && (!Number.isInteger(n) || Math.abs(n) > MAX_SAFE)) return bad('Enter a whole number.')
  if (p.min !== undefined && n < p.min) return bad(`Must be at least ${p.min}.`)
  if (p.max !== undefined && n > p.max) return bad(`Must be at most ${p.max}.`)
  return { ok: true, value: n }
}

// coerceParam turns a candidate into the typed value the binding takes, or says
// why not. An enum with static options takes only those; one fed by a query
// takes a string (Go cannot check rows either, and the select only offers the
// rows it loaded).
export function coerceParam(p: ArtifactParamT, raw: unknown): Coerced {
  switch (p.type) {
    case 'text': {
      if (typeof raw !== 'string') return bad('Enter text.')
      const limit = Math.min(p.max_length || MAX_TEXT, MAX_TEXT)
      return [...raw].length > limit ? bad(`At most ${limit} characters.`) : { ok: true, value: raw }
    }
    case 'number':
    case 'integer':
      return coerceNumeric(p, raw)
    case 'date':
      return typeof raw === 'string' && isRealDate(raw) ? { ok: true, value: raw } : bad('Pick a valid date.')
    case 'boolean':
      return typeof raw === 'boolean' ? { ok: true, value: raw } : bad('Choose yes or no.')
    case 'enum': {
      if (typeof raw !== 'string' || [...raw].length > MAX_TEXT) return bad('Choose one of the options.')
      if (p.options?.length && !p.options.includes(raw)) return bad('Choose one of the options.')
      return { ok: true, value: raw }
    }
  }
  return bad('Unknown parameter type.')
}
