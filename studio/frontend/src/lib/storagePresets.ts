// Presets for S3-compatible stores (#130 step 2). The registry data comes from
// Go (ConnTypeSpec.presets); this is only how a pick fills the form. A preset
// sets the three storage options and nothing else, and is never stored.

export const REGION_PLACEHOLDER = '<region>'
const PRESET_KEYS = ['endpoint', 'url_style', 'use_ssl'] as const
// The Region field's default for s3 (registry); a region still at it was never chosen.
const DEFAULT_REGION = 'us-east-1'

export interface PresetFill {
  // The three options after the pick: a key missing here is cleared.
  options: Record<string, string>
  region: string
}

// presetFill computes what the form holds after picking `preset` while it holds
// `options` and `region`. "other" clears nothing. Any other preset replaces the
// three options (Amazon S3 therefore clears an endpoint left by another store).
// A preset endpoint with <region> takes the Region field; a region the person
// never chose (empty or the default) becomes the preset's hint so the endpoint is
// a real host, and a region left unusable keeps the placeholder, which validation
// refuses before save.
export function presetFill(preset: ConnPreset, options: Record<string, string>, region: string): PresetFill {
  if (preset.id === 'other') return { options: { ...options }, region }
  const next = { ...options }
  for (const k of PRESET_KEYS) delete next[k]
  let reg = region
  const hasPlaceholder = Object.values(preset.options).some((v) => v.includes(REGION_PLACEHOLDER))
  if (hasPlaceholder && (!reg.trim() || reg.trim() === DEFAULT_REGION) && preset.regionHint) reg = preset.regionHint
  for (const k of PRESET_KEYS) {
    const v = preset.options[k]
    if (v === undefined) continue
    next[k] = fillRegion(v, reg)
  }
  return { options: next, region: reg }
}

// fillRegion fills the <region> of an endpoint template from a region. A region
// that is not a region name leaves the placeholder, which validation refuses.
export function fillRegion(template: string, region: string): string {
  const r = region.trim()
  return template.split(REGION_PLACEHOLDER).join(/^[A-Za-z0-9_-]{1,64}$/.test(r) ? r : REGION_PLACEHOLDER)
}

// The endpoint a preset filled from a template, remembered so a later change of
// Region can re-derive it while the person has not edited the endpoint, and so
// a region only the hint set can be taken back.
export interface FilledTemplate { template: string; endpoint: string; hintRegion: string }

// followRegion returns the endpoint to hold after Region changed to `region`:
// the re-filled template while the endpoint still equals what was filled, else null.
export function followRegion(state: FilledTemplate | null, endpoint: string, region: string): string | null {
  if (!state || endpoint !== state.endpoint) return null
  const next = fillRegion(state.template, region)
  return next === endpoint ? null : next
}

// endpointProblem is what is wrong with an endpoint before it is saved ('' when
// nothing). The server still validates the host name; this only catches the
// placeholder a preset leaves for a region.
export function endpointProblem(endpoint: string): string {
  return endpoint.includes(REGION_PLACEHOLDER) ? `Replace ${REGION_PLACEHOLDER} in the endpoint with your bucket's region.` : ''
}
