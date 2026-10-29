// useSemantics — the Semantics view's list and the definition open beside it
// (E4). One read lists entities, metrics, joins, glossary terms and the number of
// open proposals; picking a row opens it. The selection lives in the URL
// (?kind=metric&name=net_revenue; kind=proposal&name=<id>), so a reload and a
// link keep it. The list reloads, coalesced, when a semantics file or a
// proposal changes on disk (an editor, git, another process) or the in-app
// agent files a proposal (`semantics:proposed`); an open definition is read
// again so its card is never older than its file.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

export type SemanticsKind = 'entity' | 'metric' | 'join' | 'glossary' | 'proposal'
const KINDS: readonly string[] = ['entity', 'metric', 'join', 'glossary', 'proposal']
const RELOAD_COALESCE_MS = 150

export function useSemantics() {
  const route = useRoute()
  const router = useRouter()
  const list = ref<DefinitionList | null>(null)
  const loading = ref(true)
  const error = ref('')
  const detail = ref<DefinitionDetail | null>(null)
  const detailError = ref('')

  const selected = computed<{ kind: SemanticsKind; name: string } | null>(() => {
    const kind = route.query.kind
    const name = route.query.name
    if (typeof kind !== 'string' || !KINDS.includes(kind)) return null
    return { kind: kind as SemanticsKind, name: typeof name === 'string' ? name : '' }
  })
  const isEmpty = computed(() => !!list.value && !list.value.entities.length && !list.value.metrics.length && !list.value.joins.length && !list.value.terms.length && !list.value.open_proposals)

  function select(kind: SemanticsKind, name = '') {
    void router.replace({ path: route.path, query: { kind, name } })
  }

  async function loadList() {
    try {
      list.value = await App.ListDefinitions()
      error.value = ''
    } catch (e) {
      error.value = errorText(e)
    } finally {
      loading.value = false
    }
  }

  async function loadDetail() {
    const s = selected.value
    if (!s || s.kind === 'proposal') { detail.value = null; detailError.value = ''; return }
    try {
      detail.value = await App.GetDefinition(s.kind, s.name)
      detailError.value = ''
    } catch (e) {
      detail.value = null
      detailError.value = errorText(e)
    }
  }

  async function reload() { await Promise.all([loadList(), loadDetail()]) }

  watch(selected, () => { void loadDetail() })

  let timer: ReturnType<typeof setTimeout> | undefined
  let gone = false
  const offs: Array<() => void> = []
  onMounted(() => {
    void reload()
    if (!hasWailsRuntime()) return
    const schedule = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => { timer = undefined; if (!gone) void reload() }, RELOAD_COALESCE_MS)
    }
    offs.push(EventsOn('workspace:changed-on-disk', (p?: { kind?: string }) => {
      if (p?.kind === 'semantics' || p?.kind === 'proposal' || p?.kind === 'query') schedule()
    }))
    offs.push(EventsOn('semantics:proposed', schedule))
    offs.push(EventsOn('data:changed', schedule))
  })
  onUnmounted(() => {
    gone = true
    if (timer) clearTimeout(timer)
    for (const off of offs) off()
  })

  return { list, loading, error, detail, detailError, selected, isEmpty, select, reload }
}
