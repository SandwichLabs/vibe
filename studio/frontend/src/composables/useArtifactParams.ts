/**
 * useArtifactParams — the parameter values of an open data artifact (#162):
 * starts at the manifest's resolved defaults, takes candidates from the
 * controls, commits only a value that validates (a bad one leaves an inline
 * error and runs nothing), debounces typing, and resets to the default.
 * `values` holds committed, typed values only; it is what the runs send.
 */
import { reactive, onScopeDispose } from 'vue'
import { coerceParam, type ParamValue, type Coerced } from '../a2ui/artifactParams'

const TYPING_DEBOUNCE_MS = 400

export function useArtifactParams(detail: () => ArtifactDetail | null, debounceMs = TYPING_DEBOUNCE_MS) {
  const values = reactive<Record<string, ParamValue>>({})
  const drafts = reactive<Record<string, unknown>>({})
  const errors = reactive<Record<string, string>>({})
  const timers = new Map<string, ReturnType<typeof setTimeout>>()

  const param = (name: string) => detail()?.params.find((p) => p.name === name)
  const defaultOf = (name: string) => detail()?.defaults[name]

  function clearTimer(name: string) {
    const t = timers.get(name)
    if (t) clearTimeout(t)
    timers.delete(name)
  }

  // The defaults the current values were derived from, so a reload can tell a value
  // the person changed from one still sitting at the old default.
  let loadedDefaults: Record<string, unknown> = {}

  /**
   * Re-derive the values for a (re)loaded artifact: kept where still valid, else the
   * default. A value still at its old default follows a changed default (an edit in
   * the Source tab), so the view shows what the file now says.
   */
  function load(d: ArtifactDetail | null, keep = false) {
    for (const n of [...timers.keys()]) clearTimer(n)
    const previous = keep ? { ...values } : {}
    const wasDefault = keep ? loadedDefaults : {}
    loadedDefaults = { ...(d?.defaults ?? {}) }
    for (const k of Object.keys(values)) delete values[k]
    for (const k of Object.keys(drafts)) delete drafts[k]
    for (const k of Object.keys(errors)) delete errors[k]
    for (const p of d?.params ?? []) {
      const atOldDefault = p.name in previous && previous[p.name] === wasDefault[p.name]
      const kept = p.name in previous && !atOldDefault ? coerceParam(p, previous[p.name]) : undefined
      if (kept?.ok) values[p.name] = kept.value
      else if (p.name in d!.defaults) values[p.name] = d!.defaults[p.name]
    }
  }

  function commit(name: string): Coerced | undefined {
    const p = param(name)
    if (!p || !(name in drafts)) return undefined
    const res = coerceParam(p, drafts[name])
    if (res.ok) {
      delete errors[name]
      values[name] = res.value
    } else {
      errors[name] = res.error
    }
    return res
  }

  /** Offer a raw candidate for a parameter. Typing is debounced; a pick is not. */
  function set(name: string, raw: unknown, debounce = false): Coerced | undefined {
    if (!param(name)) return undefined
    drafts[name] = raw
    clearTimer(name)
    if (debounce) {
      timers.set(name, setTimeout(() => { timers.delete(name); commit(name) }, debounceMs))
      return undefined
    }
    return commit(name)
  }

  function reset(name: string) {
    clearTimer(name)
    delete drafts[name]
    delete errors[name]
    if (name in (detail()?.defaults ?? {})) values[name] = detail()!.defaults[name]
  }

  const display = (name: string): unknown => (name in drafts ? drafts[name] : values[name])
  const error = (name: string) => errors[name] ?? ''
  const isDefault = (name: string) => !errors[name] && values[name] === defaultOf(name)

  onScopeDispose(() => { for (const n of [...timers.keys()]) clearTimer(n) })

  return { values, param, display, error, isDefault, set, reset, load }
}
