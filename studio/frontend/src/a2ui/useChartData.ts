// useChartData — a KINETIC chart's data: its live "sql" result when bound,
// else its inline columns + rows (the same shape). streamChart's "refresh"
// re-runs the query every N seconds while the chart is mounted and the window
// is visible, keeping the last `window` rows.
import { computed, inject, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import type { A2UIComponentT } from './types'
import { runLiveQuery, type LiveQueryResult } from './query'
import type { TableData } from './series'

export function useChartData(component: () => A2UIComponentT) {
  const abortSignal = inject<AbortSignal | undefined>('a2uiAbortSignal', undefined)
  const loading = ref(false)
  const error = ref('')
  const live = shallowRef<LiveQueryResult | null>(null)
  let timer: ReturnType<typeof setInterval> | undefined
  let unmounted = false
  let inFlight = false

  async function fetchOnce(first: boolean) {
    const c = component()
    if (!c.sql || inFlight) return
    inFlight = true
    if (first) loading.value = true
    const res = await runLiveQuery(c.sql, abortSignal, 'low', c.connection || undefined)
    inFlight = false
    if (res.aborted || unmounted) return
    loading.value = false
    error.value = res.error ?? ''
    if (!res.error) live.value = res
  }

  onMounted(() => {
    void fetchOnce(true)
    const refresh = Number(component().refresh)
    if (component().sql && Number.isFinite(refresh) && refresh >= 5) {
      timer = setInterval(() => {
        if (typeof document !== 'undefined' && document.hidden) return
        void fetchOnce(false)
      }, Math.min(refresh, 3600) * 1000)
    }
  })
  onBeforeUnmount(() => {
    unmounted = true
    if (timer) clearInterval(timer)
  })

  const table = computed<TableData>(() => {
    const c = component()
    const src = c.sql ? live.value : { columns: c.columns ?? [], rows: c.rows ?? [] }
    if (!src) return { columns: [], rows: [] }
    const win = Number(c.window ?? (c.type === 'streamChart' ? 30 : Number.NaN))
    const rows = Number.isFinite(win) && win > 0 ? src.rows.slice(-Math.min(win, 120)) : src.rows
    return { columns: src.columns ?? [], rows }
  })

  return { loading, error, table }
}
