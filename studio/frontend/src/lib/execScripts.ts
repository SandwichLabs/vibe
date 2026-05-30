/**
 * Execute inline <script> tags inside a container element.
 * Vue's v-html does not execute scripts — this function clones each
 * <script> into a fresh element so the browser evaluates it.
 *
 * Call this from a nextTick/watch after setting v-html content
 * that contains Chart.js (or other) inline scripts.
 */
export function execScripts(container: HTMLElement | null): void {
  if (!container) return

  // Destroy any existing Chart.js instances on canvases to avoid
  // "Canvas is already in use" errors on re-render.
  const Chart = (window as any).Chart
  if (Chart) {
    const canvases = container.querySelectorAll('canvas')
    canvases.forEach((canvas) => {
      const existing = Chart.getChart(canvas)
      if (existing) existing.destroy()
    })
  }

  const scripts = container.querySelectorAll('script')
  scripts.forEach((old) => {
    const fresh = document.createElement('script')
    // Copy attributes (type, src, etc.)
    for (const attr of old.attributes) {
      fresh.setAttribute(attr.name, attr.value)
    }
    fresh.textContent = old.textContent
    old.replaceWith(fresh)
  })

  wireTabs(container)
}

/**
 * Wire up {tabs} block interactivity: clicking a .vibe-tab-header activates its
 * sibling .vibe-tab-panel (matched by data-tab-index) and deactivates the rest.
 * The server emits the first tab as `.active` so this is purely for switching.
 */
export function wireTabs(container: HTMLElement | null): void {
  if (!container) return
  const tabBlocks = container.querySelectorAll<HTMLElement>('.vibe-tabs')
  tabBlocks.forEach((block) => {
    const headers = block.querySelectorAll<HTMLElement>('.vibe-tab-header')
    const panels = block.querySelectorAll<HTMLElement>('.vibe-tab-panel')
    headers.forEach((header) => {
      header.addEventListener('click', () => {
        const idx = header.getAttribute('data-tab-index')
        headers.forEach((h) =>
          h.classList.toggle('active', h.getAttribute('data-tab-index') === idx),
        )
        panels.forEach((p) =>
          p.classList.toggle('active', p.getAttribute('data-tab-index') === idx),
        )
      })
    })
  })
}
