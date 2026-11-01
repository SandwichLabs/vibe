/**
 * usePageWindow — a scripted HTML page's own window (#162, H4, step P4). The page
 * does not run inside Studio: Go opens it in a native window under a network gate,
 * and this composable only asks for that and follows its state. States:
 *   unavailable  this system cannot open one (Go says why, with a fixed text)
 *   idle         not open
 *   opening      asked for, waiting for the page to load
 *   open         the page loaded
 *   stopped      "This page stopped responding" (a missed heartbeat, a crash)
 *   setup        "This page window could not be set up on this Mac." (the helper could not make its network gate)
 *   changed      "This page changed" (a file or the decision changed; ask again)
 *   closed       the person closed it
 * Reopen asks again, and Go checks the page is still allowed at the hash it was
 * shown. Errors are the fixed texts Go sends.
 */
import { onMounted, onUnmounted, ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { pageQuery } from './usePageFrame'
import type { ParamValue } from '../a2ui/artifactParams'

/** True on macOS, where the page window's network gate has one accepted, disclosed leak (#189). */
export function onMac(): boolean {
  return typeof navigator !== 'undefined' && /Mac/.test(navigator.platform || '')
}

export type PageWindowState = 'unavailable' | 'idle' | 'opening' | 'open' | 'stopped' | 'setup' | 'changed' | 'closed'

/** The --slaw-* roles of the window the person is looking at, from which Go builds the page's host stylesheet. */
function themeRoles(root: HTMLElement = document.documentElement): Record<string, string> {
  const out: Record<string, string> = {}
  for (let i = 0; i < root.style.length; i++) {
    const k = root.style.item(i)
    if (k.startsWith('--slaw-')) out[k] = root.style.getPropertyValue(k).trim()
  }
  if (root.dataset.themeType) out['--slaw-theme-type'] = root.dataset.themeType
  return out
}

export function usePageWindow(name: () => string, detail: () => ArtifactDetail, values: () => Record<string, ParamValue>) {
  const state = ref<PageWindowState>('idle')
  const unavailable = ref('')
  const error = ref('')
  let hash = ''

  async function open() {
    error.value = ''
    state.value = 'opening'
    try {
      const info = await App.OpenArtifactPageWindow(name(), pageQuery(detail(), values()).replace(/^\?/, ''), themeRoles())
      hash = info.hash
    } catch (e) {
      state.value = 'idle'
      error.value = errorText(e)
    }
  }

  async function close() {
    try { await App.ClosePageWindow(name()) } catch (e) { error.value = errorText(e) }
  }

  const offs: Array<() => void> = []
  onMounted(async () => {
    try {
      unavailable.value = await App.PageWindowSupport()
    } catch (e) {
      unavailable.value = errorText(e)
    }
    if (unavailable.value) state.value = 'unavailable'
    if (!hasWailsRuntime()) return
    offs.push(EventsOn('pagewindow:state', (p?: { name?: string; state?: string; hash?: string }) => {
      if (!p || p.name !== name()) return
      if (hash && p.hash && p.hash !== hash) return // an older window's end
      // A reopen ends the old window first; its "closed" is not the new window's.
      if (state.value === 'opening' && p.state === 'closed') return
      if (p.state === 'open' || p.state === 'stopped' || p.state === 'setup' || p.state === 'changed' || p.state === 'closed') state.value = p.state
    }))
  })
  // The window belongs to the artifact on screen, not to this component: it is
  // left open when the person navigates away, and found again by its events.
  onUnmounted(() => { for (const off of offs) off() })

  return { state, unavailable, error, open, close }
}
