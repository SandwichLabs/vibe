// useBoundQuery — the data of a component inside a data artifact view (#162).
// The rows come from the injected artifact runtime, which runs each named query
// once for every component bound to it and re-runs it when a parameter it uses
// changes. Inside a view a raw `sql` binding does not exist (Go refuses it when
// the view is validated); if one shows up anyway it is shown as an error and
// never run, so a view can only ever reach its own named queries.
import { computed, shallowRef, type ComputedRef } from 'vue'
import type { A2UIComponentT } from './types'
import type { ArtifactRuntime, ArtifactQueryState } from './artifactRuntime'
import type { LiveQueryResult } from './query'

export interface BoundQuery {
  loading: ComputedRef<boolean>
  error: ComputedRef<string>
  fetched: ComputedRef<boolean>
  data: ComputedRef<LiveQueryResult | null>
}

const NOT_ALLOWED = 'A data artifact view can only use its own named queries.'

export function useBoundQuery(runtime: ArtifactRuntime, component: () => A2UIComponentT): BoundQuery {
  const c = component()
  const name = typeof c.query === 'string' ? c.query : ''
  // Resolved once, at setup: asking the runtime for a name is what starts its run.
  const state = shallowRef<ArtifactQueryState | null>(name ? runtime.result(name) : null)
  // A component with neither (inline data) has nothing to show here and no error.
  const refused = !name && !!c.sql
  return {
    loading: computed(() => !!state.value?.loading),
    error: computed(() => (state.value ? state.value.error : refused ? NOT_ALLOWED : '')),
    fetched: computed(() => (state.value ? state.value.fetched : refused)),
    data: computed(() => state.value?.data ?? null),
  }
}
