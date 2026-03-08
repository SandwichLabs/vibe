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
}
