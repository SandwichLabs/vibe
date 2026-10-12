/**
 * useRailDrawer — under 1000px the Connections rail is a drawer behind the plug
 * button (F10): open/close, the narrow-window media query, Escape to close and a
 * Tab trap that keeps focus inside the open drawer.
 */
import { ref, nextTick, onMounted, onUnmounted, type Ref } from 'vue'

export function useRailDrawer(opts: { treeEl: Ref<HTMLElement | null>; filterEl: Ref<HTMLInputElement | null>; menuOpen: () => boolean }) {
  const open = ref(false) // the narrow drawer
  const narrow = ref(false)

  const mql = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(max-width: 1000px)') : null
  function syncNarrow() { narrow.value = !!mql?.matches; if (!narrow.value) open.value = false }
  function closeDrawer() { if (open.value) { open.value = false; void nextTick(() => document.querySelector<HTMLElement>('.rail-toggle')?.focus()) } }
  function toggleDrawer() {
    open.value = !open.value
    if (open.value) void nextTick(() => (opts.treeEl.value?.querySelector<HTMLElement>('[role=treeitem][tabindex="0"]') ?? opts.filterEl.value)?.focus())
  }
  function onDrawerKey(e: KeyboardEvent) {
    if (!open.value) return
    if (e.key === 'Escape' && !opts.menuOpen()) { e.preventDefault(); closeDrawer(); return }
    if (e.key === 'Tab') {
      const root = document.querySelector('.conn-rail')
      const f = Array.from(root?.querySelectorAll<HTMLElement>('button:not([disabled]), input, [tabindex="0"]') ?? []).filter((x) => x.offsetParent !== null)
      if (!f.length) return
      const first = f[0], last = f[f.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
    }
  }

  onMounted(() => {
    mql?.addEventListener?.('change', syncNarrow)
    syncNarrow()
    document.addEventListener('keydown', onDrawerKey)
  })
  onUnmounted(() => {
    mql?.removeEventListener?.('change', syncNarrow)
    document.removeEventListener('keydown', onDrawerKey)
  })

  return { open, closeDrawer, toggleDrawer }
}
