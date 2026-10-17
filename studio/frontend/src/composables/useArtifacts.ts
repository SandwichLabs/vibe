/**
 * useArtifacts — the list of data artifacts (#162). It refreshes when an
 * artifact's file changes on disk (an editor, git, an agent over MCP). A folder
 * that cannot be read lists with its error, so it can be found and fixed.
 */
import { ref, onMounted, onUnmounted } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

const RELOAD_COALESCE_MS = 150

export function useArtifacts() {
  const artifacts = ref<ArtifactSummary[]>([])
  const loading = ref(true)
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | undefined
  let gone = false

  async function load() {
    try {
      const list = (await App.ListArtifacts()) || []
      if (gone) return
      artifacts.value = list
      error.value = ''
    } catch (e) {
      if (!gone) error.value = errorText(e)
    } finally {
      if (!gone) loading.value = false
    }
  }

  const offs: Array<() => void> = []
  onMounted(() => {
    void load()
    if (!hasWailsRuntime()) return
    offs.push(EventsOn('workspace:changed-on-disk', (p?: { kind?: string }) => {
      if (p?.kind !== 'artifact') return
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => { timer = undefined; void load() }, RELOAD_COALESCE_MS)
    }))
  })
  onUnmounted(() => {
    gone = true
    if (timer) clearTimeout(timer)
    for (const off of offs) off()
  })

  return { artifacts, loading, error, reload: load }
}
