/**
 * useLibrary: the Library list (docs/plans/library.md L5). One merged list
 * from ListNotebooks and ListArtifacts, loaded together; either failing shows
 * its error and keeps the other's rows. It reloads when a notebook or an
 * artifact changes on disk (an editor, git, an agent over MCP) or the in-app
 * agent changes data (`data:changed`). The type chip
 * lives in the URL (?type=) so Back keeps it; the name filter and sort do not.
 */
import { ref, computed, onMounted, onUnmounted, type Ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { artifactItem, chipsFor, filterItems, notebookItem, sortItems, type LibraryItem } from '../lib/libraryItems'
import type { LibraryType } from '../lib/libraryRoutes'

const RELOAD_COALESCE_MS = 150

export function useLibrary(type: Ref<LibraryType | null>) {
  const notebooks = ref<WailsNotebookSummary[]>([])
  const artifacts = ref<ArtifactSummary[]>([])
  const notebookError = ref('')
  const artifactError = ref('')
  const loading = ref(true)
  const filter = ref('')
  const sortBy = ref<'recent' | 'name'>('recent')
  let timer: ReturnType<typeof setTimeout> | undefined
  let gone = false

  async function load() {
    const [nb, ar] = await Promise.allSettled([App.ListNotebooks(), App.ListArtifacts()])
    if (gone) return
    if (nb.status === 'fulfilled') { notebooks.value = nb.value || []; notebookError.value = '' } else notebookError.value = errorText(nb.reason)
    if (ar.status === 'fulfilled') { artifacts.value = ar.value || []; artifactError.value = '' } else artifactError.value = errorText(ar.reason)
    loading.value = false
  }

  const all = computed<LibraryItem[]>(() => [...notebooks.value.map(notebookItem), ...artifacts.value.map(artifactItem)])
  const chips = computed(() => chipsFor(all.value, type.value))
  const items = computed(() => sortItems(filterItems(all.value, type.value, filter.value), sortBy.value))
  const errors = computed(() => [notebookError.value && `Notebooks: ${notebookError.value}`, artifactError.value && `Artifacts: ${artifactError.value}`].filter(Boolean) as string[])

  const offs: Array<() => void> = []
  onMounted(() => {
    void load()
    if (!hasWailsRuntime()) return
    const schedule = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => { timer = undefined; void load() }, RELOAD_COALESCE_MS)
    }
    offs.push(EventsOn('workspace:changed-on-disk', (p?: { kind?: string }) => {
      if (p?.kind === 'artifact' || p?.kind === 'notebook') schedule()
    }))
    // An agent tool that changed data (write_artifact among them) in this process.
    offs.push(EventsOn('data:changed', schedule))
  })
  onUnmounted(() => {
    gone = true
    if (timer) clearTimeout(timer)
    for (const off of offs) off()
  })

  return { all, items, chips, errors, loading, filter, sortBy, reload: load }
}
