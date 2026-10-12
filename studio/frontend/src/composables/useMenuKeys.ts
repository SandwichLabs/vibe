/**
 * useMenuKeys — the one keyboard handler for the app's popup menus and listboxes
 * (F7): the Connections rail's context menu, the workspace switcher and the "Run
 * on" connection select. ArrowDown/ArrowUp move to the next/previous ENABLED
 * entry and wrap, Home/End go to the first/last enabled one, Escape closes and
 * returns focus, Tab closes (and, for a menu that keeps focus inside itself,
 * stops the browser's focus move). A listbox passes `choose`, so Enter and Space
 * pick the current entry.
 */
interface MenuKeysOptions {
  /** One flag per entry: can it be chosen? (separators and disabled entries: false) */
  enabled: () => boolean[]
  /** The entry the menu is on now (-1: none). */
  current: () => number
  /** Move to entry i (set the roving index and/or focus it). */
  moveTo: (i: number) => void
  close: (restoreFocus: boolean) => void
  /** Tab: 'restore' stops the browser's focus move and returns focus to the opener; 'leave' lets it go on. */
  tab: 'restore' | 'leave'
  /** Enter / Space on the current entry (listboxes only). */
  choose?: (i: number) => void
}

function menuKeydown(e: KeyboardEvent, o: MenuKeysOptions): void {
  const on = o.enabled()
  const n = on.length
  // step: the next enabled entry from the current one in direction d, wrapping.
  const step = (d: number) => {
    let i = o.current()
    for (let k = 0; k < n; k++) {
      i = (i + d + n) % n
      if (on[i]) return o.moveTo(i)
    }
  }
  switch (e.key) {
    case 'ArrowDown': e.preventDefault(); step(1); break
    case 'ArrowUp': e.preventDefault(); step(-1); break
    case 'Home': e.preventDefault(); { const i = on.indexOf(true); if (i >= 0) o.moveTo(i) } break
    case 'End': e.preventDefault(); { const i = on.lastIndexOf(true); if (i >= 0) o.moveTo(i) } break
    case 'Enter': case ' ':
      if (o.choose) { e.preventDefault(); e.stopPropagation(); o.choose(o.current()) }
      break
    case 'Escape': e.preventDefault(); e.stopPropagation(); o.close(true); break
    case 'Tab':
      if (o.tab === 'restore') { e.preventDefault(); o.close(true) } else o.close(false)
      break
  }
}

/** useMenuKeys returns the keydown handler for a menu described by `o`. */
export function useMenuKeys(o: MenuKeysOptions): (e: KeyboardEvent) => void {
  return (e) => menuKeydown(e, o)
}
