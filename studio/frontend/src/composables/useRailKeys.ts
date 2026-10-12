/**
 * useRailKeys — the Connections rail's keyboard model (F10), WAI-ARIA tree:
 * arrows move and expand/collapse, Home/End, type-ahead, `*` expands siblings,
 * Enter/Space activate, Delete removes a connection, F5 refreshes, Shift+F10 /
 * the Menu key opens the context menu, Ctrl+Alt+N a query, `/` the filter.
 */
import type { Ref } from 'vue'
import type { Row } from '../lib/railTree'
import type { ConnectionTree } from './useConnectionTree'
import type { RailActions } from './useRailActions'

export function useRailKeys(tree: ConnectionTree, a: RailActions, opts: { openMenu: (r: Row) => void; filterEl: Ref<HTMLInputElement | null> }) {
  function onTreeKey(e: KeyboardEvent, r: Row) {
    const list = tree.focusables.value
    const i = list.findIndex((x) => x.key === r.key)
    const move = (n: number) => { const t = list[Math.max(0, Math.min(list.length - 1, n))]; if (t) tree.focusRow(t.key) }
    if (e.key === 'F10' && e.shiftKey || e.key === 'ContextMenu') { e.preventDefault(); opts.openMenu(r); return }
    if (e.key === 'F5') { e.preventDefault(); e.stopPropagation(); void a.refreshConn(r.conn); return }
    if (e.ctrlKey && e.altKey && e.key.toLowerCase() === 'n') { e.preventDefault(); a.newQueryHere(r); return }
    if (e.ctrlKey || e.metaKey || e.altKey) return
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); move(i + 1); break
      case 'ArrowUp': e.preventDefault(); move(i - 1); break
      case 'Home': e.preventDefault(); move(0); break
      case 'End': e.preventDefault(); move(list.length - 1); break
      case 'ArrowRight':
        e.preventDefault()
        if (r.expandable && !r.isOpen) void tree.expandRow(r)
        else if (r.isOpen) move(i + 1)
        break
      case 'ArrowLeft':
        e.preventDefault()
        if (r.isOpen) tree.collapseRow(r)
        else if (r.parent) tree.focusRow(r.parent)
        break
      case 'Enter': case ' ':
        e.preventDefault()
        if (r.kind === 'conn' && e.key === 'Enter' && r.conn.status === 'ok' && !r.detail) a.editConn(r.conn)
        else void a.activate(r)
        break
      case 'Delete':
        if (r.kind === 'conn') { e.preventDefault(); void a.removeConn(r.conn) }
        break
      case '*': {
        e.preventDefault()
        list.filter((x) => x.level === r.level && x.parent === r.parent && x.expandable && !x.isOpen).forEach((x) => { void tree.expandRow(x) })
        break
      }
      case '/': e.preventDefault(); opts.filterEl.value?.focus(); break
      default:
        if (e.key.length === 1 && /\S/.test(e.key)) {
          const ch = e.key.toLowerCase()
          const order = [...list.slice(i + 1), ...list.slice(0, i + 1)]
          const hit = order.find((x) => x.label.toLowerCase().startsWith(ch))
          if (hit) { e.preventDefault(); tree.focusRow(hit.key) }
        }
    }
  }

  function onRowClick(e: MouseEvent, r: Row) {
    tree.focusKey.value = r.key
    if ((e.target as HTMLElement).closest('.row-menu, .tn-note')) return
    if (r.kind === 'conn' && (e.target as HTMLElement).closest('.chev')) { void tree.toggleRow(r); return }
    void a.activate(r)
  }

  return { onTreeKey, onRowClick }
}
