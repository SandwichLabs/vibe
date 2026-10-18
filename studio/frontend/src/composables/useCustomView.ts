/**
 * useCustomView — wires one allowed custom artifact view (#160) to the screen:
 * starts its Worker, asks it to draw on open and on every change of a
 * parameter, the theme or the width (debounced; the latest request wins), and
 * restarts it on Reload. The bridge itself (limits, watchdog, outputs) is
 * useViewBridge.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import { useViewBridge } from './useViewBridge'
import { useTheme } from '../theme/useTheme'
import { viewTheme, type RunParams } from '../lib/viewBridge'

const REDRAW_DEBOUNCE_MS = 150
const MIN_WIDTH = 240
const MAX_WIDTH = 2400
const VIEW_HEIGHT = 400

export function useCustomView(name: () => string, values: () => Record<string, unknown>, stage: Ref<HTMLElement | null>) {
  const { activeTheme } = useTheme()
  const theme = computed(() => viewTheme(activeTheme.value.colors, activeTheme.value.type))
  const bridge = useViewBridge(name, () => theme.value)
  const width = ref(640)
  let timer: ReturnType<typeof setTimeout> | undefined
  let observer: ResizeObserver | undefined

  function redraw() {
    // Only the manifest's typed values, as plain JSON.
    bridge.draw(JSON.parse(JSON.stringify(values())) as RunParams, width.value, VIEW_HEIGHT)
  }
  function redrawSoon() {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => { timer = undefined; redraw() }, REDRAW_DEBOUNCE_MS)
  }
  async function reload() {
    await bridge.start()
    redraw()
  }

  watch([values, theme, width], redrawSoon, { deep: true })
  const fit = (w: number) => { if (w > 0) width.value = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.round(w))) }
  onMounted(() => {
    const el = stage.value
    if (el) {
      // Measured now as well as observed: a webview that is not painting (a hidden window) delivers no first observation.
      fit(el.clientWidth)
      if (typeof ResizeObserver !== 'undefined') {
        observer = new ResizeObserver((entries) => fit(entries[0]?.contentRect.width ?? 0))
        observer.observe(el)
      }
    }
    void reload()
  })
  onBeforeUnmount(() => {
    if (timer) clearTimeout(timer)
    observer?.disconnect()
  })

  return { ...bridge, reload }
}
