/**
 * useArtifactViewTrust — where one artifact's custom (script) view stands for
 * this person (#159): undecided, allowed, changed since allowed, or denied,
 * with the exact files its hash covers. Allow sends back the hash the person
 * was shown, so a file that changed after they looked is refused by Go. A
 * change to the artifact on disk re-reads it at once (the watcher event), so a
 * view that was allowed and was then edited asks again. The view is never run
 * here; everything below is text.
 */
import { ref, shallowRef, watch, onMounted, onUnmounted } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

export function useArtifactViewTrust(name: () => string) {
  // status is what is on screen, and the only thing Allow acts on.
  const status = shallowRef<ArtifactViewStatus | null>(null)
  // True when a reload found different bytes while an Allow was armed on the
  // ones on screen: the person must look at the new version before Allow works.
  const changedWhileReading = ref(false)
  let pending: ArtifactViewStatus | null = null
  const busy = ref(false)
  // True from the moment the artifact changes on disk until it has been read again: a running view must not outlive the bytes it was allowed for.
  const refreshing = ref(false)
  const error = ref('')
  let seq = 0

  async function load() {
    const mine = ++seq
    try {
      const s = await App.ArtifactViewState(name())
      if (mine !== seq) return
      adopt(s)
      error.value = ''
    } catch (e) {
      if (mine === seq) { status.value = null; error.value = errorText(e) }
    } finally {
      if (mine === seq) refreshing.value = false
    }
  }

  function changed() { refreshing.value = true; void load() }

  // adopt shows a fresh read. If an Allow is armed (undecided or changed) on
  // other bytes, the new ones wait behind showNew(): what the person reads must
  // be what the click approves.
  function adopt(s: ArtifactViewStatus) {
    const cur = status.value
    const armed = !!cur && !!cur.hash && (cur.state === 'undecided' || cur.state === 'changed')
    if (armed && s.hash !== cur!.hash) {
      pending = s
      changedWhileReading.value = true
      return
    }
    pending = null
    changedWhileReading.value = false
    status.value = s
  }

  function showNew() {
    if (pending) status.value = pending
    pending = null
    changedWhileReading.value = false
  }

  async function decide(call: () => Promise<ArtifactViewStatus>) {
    busy.value = true
    error.value = ''
    try {
      seq++
      status.value = await call()
      refreshing.value = false
      pending = null
      changedWhileReading.value = false
    } catch (e) {
      const msg = errorText(e)
      await load() // a refused allow (the files changed) shows the files as they are now
      error.value = msg
    }
    busy.value = false
  }

  // Allow approves exactly the hash on screen.
  const allow = () => {
    const shown = status.value
    if (!shown || changedWhileReading.value) return
    return decide(() => App.AllowArtifactView(name(), shown.hash))
  }
  const deny = () => decide(() => App.DenyArtifactView(name()))
  const revoke = () => decide(() => App.RevokeArtifactView(name()))

  const offs: Array<() => void> = []
  onMounted(() => {
    void load()
    if (!hasWailsRuntime()) return
    offs.push(EventsOn('workspace:changed-on-disk', (p?: { kind?: string; id?: string }) => { if (p?.kind === 'artifact' && p.id === name()) changed() }))
    offs.push(EventsOn('workspace:changed', (p?: { kind?: string; id?: string }) => { if (p?.kind === 'artifactview' && p.id === name()) changed() }))
  })
  watch(name, () => { status.value = null; pending = null; changedWhileReading.value = false; void load() })
  onUnmounted(() => { seq++; for (const off of offs) off() })

  return { status, changedWhileReading, showNew, busy, error, refreshing, allow, deny, revoke, reload: load }
}
