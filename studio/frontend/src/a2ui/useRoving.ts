// useRoving — one tab stop for a set of drillable rows, bars or cells, moved
// with the arrow keys (the usual pattern for a composite widget), so a 500-cell
// pivot does not put 500 stops in the tab order. Items carry `data-roving`
// and `data-roving-i` (and `data-col` in a grid); the host element carries the two handlers.
// Left/Right and Up/Down move through the items in document order; in a grid
// (`data-col`), Up/Down move to the same column of the nearest row that has one.
import { ref } from 'vue'

export function useRoving(count: () => number) {
  const current = ref(0)
  // current may point past the end after the data shrank: then the last item is the stop.
  const tab = (i: number) => (i === Math.min(current.value, Math.max(count() - 1, 0)) ? 0 : -1)

  function onFocusin(e: FocusEvent) {
    const el = (e.target as HTMLElement | null)?.closest<HTMLElement>('[data-roving]')
    const i = Number(el?.dataset.rovingI)
    if (Number.isInteger(i)) current.value = i
  }

  function vertical(items: HTMLElement[], from: HTMLElement, dir: 1 | -1): HTMLElement | undefined {
    const col = from.dataset.col
    if (col === undefined) return items[items.indexOf(from) + dir]
    let tr = from.closest('tr')?.[dir === 1 ? 'nextElementSibling' : 'previousElementSibling']
    while (tr) {
      const hit = Array.from(tr.querySelectorAll<HTMLElement>('[data-roving]')).find((x) => x.dataset.col === col)
      if (hit) return hit
      tr = tr[dir === 1 ? 'nextElementSibling' : 'previousElementSibling']
    }
    return undefined
  }

  function onKeydown(e: KeyboardEvent) {
    const from = (e.target as HTMLElement | null)?.closest<HTMLElement>('[data-roving]')
    if (!from) return
    const items = Array.from((e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[data-roving]'))
    const at = items.indexOf(from)
    let to: HTMLElement | undefined
    switch (e.key) {
      case 'ArrowRight': to = items[at + 1]; break
      case 'ArrowLeft': to = items[at - 1]; break
      case 'ArrowDown': to = vertical(items, from, 1); break
      case 'ArrowUp': to = vertical(items, from, -1); break
      case 'Home': to = items[0]; break
      case 'End': to = items[items.length - 1]; break
      default: return
    }
    e.preventDefault()
    to?.focus()
  }

  return { tab, onFocusin, onKeydown }
}
