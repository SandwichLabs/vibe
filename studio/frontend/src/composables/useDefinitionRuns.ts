// useDefinitionRuns — the person's runs of what a semantic definition declares
// (E4): the assertions of an entity or metric, a profile of a join, and a
// metric's "Try it". Each is one Studio binding that goes through the agent
// policy's read-only path; nothing here can change data. A profile writes the
// join's measured: block, and a join file that changed meanwhile comes back as
// a conflict to show, never an overwrite.
import { computed, onUnmounted, ref, type UnwrapNestedRefs } from 'vue'
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

  // Seconds the current run has taken, for a run that takes a while (a check
  // can take up to two minutes); ticks only while busy.
  const elapsed = ref(0)
  let ticker: ReturnType<typeof setInterval> | undefined
  function stopTicker() { if (ticker) { clearInterval(ticker); ticker = undefined } }
  onUnmounted(stopTicker)

  const failed = computed(() => results.value.filter((r) => r.status === 'fail').length)
  const errored = computed(() => results.value.filter((r) => r.status === 'error').length)

  // One run at a time: a second start while one is going does nothing, so a
  // double click or a second card can never double-start a check.
  async function guarded<T>(job: () => Promise<T>): Promise<T | null> {
    if (busy.value) return null
    busy.value = true
    error.value = ''
    conflict.value = ''
    elapsed.value = 0
    ticker = setInterval(() => { elapsed.value += 1 }, 1000)
    try {
      return await job()
    } catch (e) {
      if (isConflict(e)) conflict.value = errorText(e)
      else error.value = errorText(e)
      return null
    } finally {
      stopTicker()
      busy.value = false
    }
  }

  /** Forget the last run's output (another definition was opened). */
  function reset() {
    results.value = []
    profile.value = null
    tried.value = null
    error.value = ''
    conflict.value = ''
  }

  /** Run the assertions of one entity or metric ('' kind and name: everything). */
  async function runAssertions(kind = '', name = '') {
    if (busy.value) return
    results.value = []
    const rows = await guarded(() => App.RunAssertions(kind, name))
    if (rows) results.value = rows
  }

  /** Profile a join and record the measure in its file. */
  async function profileJoin(name: string) {
    if (busy.value) return
    const p = await guarded(() => App.ProfileJoin(name))
    if (p) profile.value = p
  }

  /** Try a metric by a subset of its dimensions. */
  async function tryMetric(name: string, dims: string[]) {
    if (busy.value) return
    tried.value = null
    const t = await guarded(() => App.RunMetric(name, dims))
    if (t) tried.value = t
  }

  return { results, profile, tried, busy, elapsed, error, conflict, failed, errored, runAssertions, profileJoin, tryMetric, reset }
}

/** The runs as the cards get them (wrap in reactive() so the refs read as values). */
export type DefinitionRuns = UnwrapNestedRefs<ReturnType<typeof useDefinitionRuns>>
