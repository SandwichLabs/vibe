// The parameter controls of a custom (script) view (#160), made from the
// manifest. A script view has no A2UI page of its own to hold controls, so the
// host builds one control per parameter out of the level-1 control components
// (A2UIControl): the same four types, the same runtime, the same validation.
import type { A2UIComponentT } from './types'

/** One control component per parameter; consecutive date parameters pair into a range. */
export function controlsFor(params: ArtifactParamT[]): A2UIComponentT[] {
  const out: A2UIComponentT[] = []
  let open: A2UIComponentT | undefined // a dateRange still waiting for its end date
  for (const p of params) {
    if (p.type === 'date') {
      if (open) {
        open.end = p.name
        open = undefined
      } else {
        open = { id: `ctl-${p.name}`, type: 'dateRange', start: p.name }
        out.push(open)
      }
      continue
    }
    open = undefined // a range pairs consecutive date parameters only
    const type = p.type === 'enum' || p.type === 'boolean' ? 'select' : p.type === 'text' ? 'textInput' : 'numberInput'
    out.push({ id: `ctl-${p.name}`, type, param: p.name })
  }
  return out
}
