// useDefinitionRuns — the person's runs of what a semantic definition declares
// (E4): the assertions of an entity or metric, a profile of a join, and a
// metric's "Try it". Each is one Studio binding that goes through the agent
// policy's read-only path; nothing here can change data. A profile writes the
// join's measured: block, and a join file that changed meanwhile comes back as
// a conflict to show, never an overwrite.
import { computed, ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText, isConflict } from '../lib/errors'

export function useDefinitionRuns() {
  const results = ref<AssertionRow[]>([])
  const profile = ref<JoinProfile | null>(null)
  const tried = ref<QueryResult | null>(null)
  const busy = ref(false)
  const error = ref('')
  // Set when a profile was refused because the join file changed while it ran.
  const conflict = ref('')

  const failed = computed(() => results.value.filter((r) => r.status === 'fail').length)
  const errored = computed(() => results.value.filter((r) => r.status === 'error').length)

  async function guarded<T>(job: () => Promise<T>): Promise<T | null> {
    busy.value = true
    error.value = ''
    conflict.value = ''
    try {
      return await job()
    } catch (e) {
      if (isConflict(e)) conflict.value = errorText(e)
      else error.value = errorText(e)
      return null
    } finally {
      busy.value = false
    }
  }

  /** Run the assertions of one entity or metric ('' kind and name: everything). */
  async function runAssertions(kind = '', name = '') {
    const rows = await guarded(() => App.RunAssertions(kind, name))
    results.value = rows ?? []
  }

  /** Profile a join and record the measure in its file. */
  async function profileJoin(name: string) {
    profile.value = await guarded(() => App.ProfileJoin(name))
  }

  /** Try a metric by a subset of its dimensions. */
  async function tryMetric(name: string, dims: string[]) {
    tried.value = await guarded(() => App.RunMetric(name, dims))
  }

  return { results, profile, tried, busy, error, conflict, failed, errored, runAssertions, profileJoin, tryMetric }
}
