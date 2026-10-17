/**
 * useArtifact — loads one data artifact (#162) and keeps it current: a change
 * to either of its files on disk (an editor, git, an agent writing through
 * MCP) re-reads it. A broken artifact is not a failure: it loads with its
 * errors, which the view shows. The name comes from the route, so it is checked
 * before it reaches a binding.
 */
import { ref, shallowRef, watch, onMounted, onUnmounted } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

const ARTIFACT_NAME = /^[a-z0-9][a-z0-9-]{0,63}$/
const RELOAD_COALESCE_MS = 150

export function useArtifact(name: () => string) {
  const detail = shallowRef<ArtifactDetail | null>(null)
  const loading = ref(true)
  // A failure to open at all (no such artifact, a bad name); a broken artifact is detail.error.
  const failure = ref('')
  let timer: ReturnType<typeof setTimeout> | undefined
  let seq = 0

  async function load() {
    const n = name()
    const mine = ++seq
    if (!ARTIFACT_NAME.test(n)) {
      detail.value = null
      failure.value = 'That is not a valid artifact name.'
      loading.value = false
      return
    }
    try {
      const d = await App.GetArtifact(n)
      if (mine !== seq) return
      failure.value = ''
      // Same revision and same state: keep the object so nothing re-renders or re-runs.
      if (!(detail.value && detail.value.name === d.name && detail.value.rev === d.rev && detail.value.error === d.error)) detail.value = d
    } catch (e) {
      if (mine !== seq) return
      detail.value = null
      failure.value = errorText(e)
    } finally {
      if (mine === seq) loading.value = false
    }
  }

  function scheduleReload() {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => { timer = undefined; void load() }, RELOAD_COALESCE_MS)
  }

  function onDiskChange(p?: { kind?: string; id?: string }) {
    if (p?.kind === 'artifact' && p.id === name()) scheduleReload()
  }

  const offs: Array<() => void> = []
  onMounted(() => {
    void load()
    if (hasWailsRuntime()) offs.push(EventsOn('workspace:changed-on-disk', onDiskChange))
  })
  watch(name, () => { loading.value = true; void load() })
  onUnmounted(() => {
    seq++
    if (timer) clearTimeout(timer)
    for (const off of offs) off()
  })

  return { detail, loading, failure, reload: load }
}
