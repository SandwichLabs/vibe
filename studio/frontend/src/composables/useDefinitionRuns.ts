// useDefinitionRuns — the person's runs of what a semantic definition declares
// (E4): the assertions of an entity or metric, a profile of a join, and a
// metric's "Try it". Each is one Studio binding that goes through the agent
// policy's read-only path; nothing here can change data. A profile writes the
// join's measured: block, and a join file that changed meanwhile comes back as
// a conflict to show, never an overwrite.
import { computed, onUnmounted, ref, type UnwrapNestedRefs } from 'vue'
import { App } from '../lib/bindings'
import { errorText, isCancelled, isConflict } from '../lib/errors'

export function useDefinitionRuns() {
  const results = ref<AssertionRow[]>([])
  const profile = ref<JoinProfile | null>(null)
  const tried = ref<QueryResult | null>(null)
  const busy = ref(false)
  const error = ref('')
  // Set when the last run was stopped with Stop: a neutral line, not an error.
  const cancelled = ref(false)
  // The Go run key of the run in flight (studio/run_cancel.go), for Stop.
  let runKey = ''
  // Set when a profile was refused because the join file changed while it ran.
  const conflict = ref('')

  // Seconds the current run has taken, for a run that takes a while (a check
  // can take up to two minutes); ticks only while busy.
  const elapsed = ref(0)
  let ticker: ReturnType<typeof setInterval> | undefined
  function stopTicker() { if (ticker) { clearInterval(ticker); ticker = undefined } }
  // Leaving the view mid-run stops it (the Go run is registered by kind and would run on unseen).
  onUnmounted(() => {
    stopTicker()
    if (busy.value && runKey) void App.CancelRun(runKey)
  })

  const failed = computed(() => results.value.filter((r) => r.status === 'fail').length)
  const errored = computed(() => results.value.filter((r) => r.status === 'error').length)

  // One run at a time: a second start while one is going does nothing, so a
  // double click or a second card can never double-start a check.
  async function guarded<T>(key: string, job: () => Promise<T>): Promise<T | null> {
    if (busy.value) return null
    busy.value = true
    runKey = key
    cancelled.value = false
    error.value = ''
    conflict.value = ''
    elapsed.value = 0
    ticker = setInterval(() => { elapsed.value += 1 }, 1000)
    try {
      return await job()
    } catch (e) {
      if (isCancelled(e)) cancelled.value = true
      else if (isConflict(e)) conflict.value = errorText(e)
      else error.value = errorText(e)
      return null
    } finally {
      runKey = ''
      stopTicker()
      busy.value = false
    }
  }

  /** Stop the run in flight; it then ends with the cancelled error and shows no output. */
  function stop() {
    if (busy.value && runKey) void App.CancelRun(runKey)
  }

  /** Forget the last run's output (another definition was opened). */
  function reset() {
    results.value = []
    profile.value = null
    tried.value = null
    error.value = ''
    cancelled.value = false
    conflict.value = ''
  }

  /** Run the assertions of one entity or metric ('' kind and name: everything). */
  async function runAssertions(kind = '', name = '') {
    if (busy.value) return
    results.value = []
    const rows = await guarded('assertions', () => App.RunAssertions(kind, name))
    if (rows) results.value = rows
  }

  /** Profile a join and record the measure in its file. */
  async function profileJoin(name: string) {
    if (busy.value) return
    const p = await guarded('profile', () => App.ProfileJoin(name))
    if (p) profile.value = p
  }

  /** Try a metric by a subset of its dimensions. */
  async function tryMetric(name: string, dims: string[]) {
    if (busy.value) return
    tried.value = null
    const t = await guarded('metric', () => App.RunMetric(name, dims))
    if (t) tried.value = t
  }

  return { results, profile, tried, busy, elapsed, error, cancelled, conflict, failed, errored, runAssertions, profileJoin, tryMetric, stop, reset }
}

/** The runs as the cards get them (wrap in reactive() so the refs read as values). */
export type DefinitionRuns = UnwrapNestedRefs<ReturnType<typeof useDefinitionRuns>>
