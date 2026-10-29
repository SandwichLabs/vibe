/**
 * useQueryParams — the inputs for a saved query's declared $name parameters
 * (#235). Each is pre-filled with its declared default and typed like the
 * artifact controls (the same coerceParam check), so a bad value is shown
 * beside its input and never sent. Go binds what is sent (queries.Bind, the
 * binder run_saved_query uses): a value is a bound parameter, never SQL text.
 */
import { reactive, ref, computed } from 'vue'
import { coerceParam, type ParamValue } from '../a2ui/artifactParams'

type Raw = string | boolean

export type ParamsPayload = { ok: true; params: Record<string, ParamValue> } | { ok: false; error: string }

const asArtifactParam = (p: QueryParamT): ArtifactParamT => ({ name: p.name, type: p.type, default: p.default ?? null })

export function useQueryParams() {
  const decls = ref<QueryParamT[]>([])
  const raw = reactive<Record<string, Raw>>({})
  // Only a value the person has changed is checked while typing; a run checks all.
  const shownErrors = reactive<Record<string, string>>({})

  function initial(p: QueryParamT): Raw {
    if (p.type === 'boolean') return p.default === 'true'
    // A relative date default (today-30d) is not a value a date input can hold:
    // it stays blank and Go resolves the default.
    if (p.type === 'date') return /^\d{4}-\d{2}-\d{2}$/.test(p.default ?? '') ? (p.default as string) : ''
    return p.default ?? ''
  }

  function load(params: QueryParamT[] | null | undefined) {
    clear()
    decls.value = params ?? []
    for (const p of decls.value) raw[p.name] = initial(p)
  }

  function clear() {
    decls.value = []
    for (const k of Object.keys(raw)) delete raw[k]
    for (const k of Object.keys(shownErrors)) delete shownErrors[k]
  }

  function check(p: QueryParamT): string {
    const v = raw[p.name]
    // Empty with no default to fall back on is missing, for any type (never a silent "").
    if (v === '' && p.required && !p.default) return 'A value is required.'
    if (v === '' && p.type !== 'text') return ''
    const c = coerceParam(asArtifactParam(p), v)
    return c.ok ? '' : c.error
  }

  function set(name: string, value: Raw) {
    raw[name] = value
    const p = decls.value.find((d) => d.name === name)
    if (p) shownErrors[name] = check(p)
  }

  function reset(name: string) {
    const p = decls.value.find((d) => d.name === name)
    if (!p) return
    raw[name] = initial(p)
    shownErrors[name] = ''
  }

  const isDefault = (name: string) => {
    const p = decls.value.find((d) => d.name === name)
    return !p || raw[name] === initial(p)
  }

  /** The typed values to send, or the first error. A blank non-text value is left out: Go applies the default. */
  function payload(): ParamsPayload {
    const params: Record<string, ParamValue> = {}
    for (const p of decls.value) {
      const msg = check(p)
      shownErrors[p.name] = msg
      if (msg) return { ok: false, error: `Parameter ${p.name}: ${msg}` }
      if (raw[p.name] === '' && p.type !== 'text') continue
      const c = coerceParam(asArtifactParam(p), raw[p.name])
      if (c.ok) params[p.name] = c.value
    }
    return { ok: true, params }
  }

  const hasParams = computed(() => decls.value.length > 0)

  return { decls, raw, shownErrors, hasParams, load, clear, set, reset, isDefault, payload }
}
