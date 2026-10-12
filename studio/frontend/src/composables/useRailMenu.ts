/**
 * useRailMenu — the Connections rail's context menu (F10): the items for a
 * connection, schema, table or column row, where the menu opens (the pointer, or
 * beside the row from the keyboard), and its keyboard handling (useMenuKeys).
 */
import { ref, nextTick, onMounted, onUnmounted } from 'vue'
import { typeLabel } from '../lib/connections'
import type { NodeKey, Row } from '../lib/railTree'
import type { ConnectionTree } from './useConnectionTree'
import type { RailActions } from './useRailActions'
import { useMenuKeys } from './useMenuKeys'

interface MenuItem { id: string; label: string; icon?: string; kbd?: string; danger?: boolean; disabled?: boolean; sep?: boolean; run: () => void }

export function useRailMenu(tree: ConnectionTree, a: RailActions) {
  const menu = ref<{ x: number; y: number; title: string; items: MenuItem[]; from: NodeKey } | null>(null)
  const menuEl = ref<HTMLElement | null>(null)
  const menuIndex = ref(0)

  function itemsFor(r: Row): MenuItem[] {
    if (r.kind === 'conn') {
      const c = r.conn
      const isOpen = c.status === 'ok'
      const own = c.default && !c.remote
      return [
        isOpen
          ? { id: 'disconnect', label: 'Disconnect', icon: 'unlock', disabled: own, run: () => a.disconnect(c) }
          : { id: 'connect', label: 'Connect', icon: 'plug', run: () => { void tree.expandConn(c) } },
        { id: 'refresh', label: 'Refresh', icon: 'refresh', kbd: 'F5', disabled: !isOpen, run: () => a.refreshConn(c) },
        { id: 'newq', label: 'New query here', icon: 'terminal', kbd: 'Ctrl Alt N', run: () => a.newQueryHere(r) },
        { id: 's1', label: '', sep: true, run: () => {} },
        { id: 'edit', label: 'Edit…', icon: 'edit', kbd: 'Enter', run: () => a.editConn(c) },
        { id: 'test', label: 'Test', icon: 'zap', run: () => a.testConn(c) },
        { id: 'dup', label: 'Duplicate', icon: 'copy', run: () => a.duplicateConn(c) },
        { id: 's2', label: '', sep: true, run: () => {} },
        { id: 'remove', label: 'Remove…', icon: 'trash', kbd: 'Del', danger: true, run: () => a.removeConn(c) },
      ]
    }
    if (r.kind === 'table' || r.kind === 'view') {
      return [
        { id: 'tq', label: 'New query on this table', icon: 'terminal', run: () => a.newQueryHere(r) },
        { id: 'ds', label: 'Open as datasheet', icon: 'tableIcon', run: () => a.openDatasheet(r) },
        { id: 'cp', label: 'Copy name', icon: 'copy', run: () => a.copyName(r) },
      ]
    }
    return [{ id: 'cp', label: 'Copy name', icon: 'copy', run: () => a.copyName(r) }]
  }

  function openMenu(r: Row, at?: { x: number; y: number }) {
    let x = at?.x, y = at?.y
    if (x === undefined || y === undefined) {
      const el = tree.rowEl(r.key)
      const b = el?.getBoundingClientRect()
      x = (b?.left ?? 80) + 40
      y = (b?.bottom ?? 120)
    }
    x = Math.min(x, window.innerWidth - 260)
    y = Math.min(y, window.innerHeight - 320)
    tree.focusKey.value = r.key
    menu.value = { x, y, title: r.kind === 'conn' ? `${r.label} · ${typeLabel(r.conn.type)}` : r.label, items: itemsFor(r), from: r.key }
    menuIndex.value = menu.value.items.findIndex((i) => !i.sep && !i.disabled)
    void nextTick(() => menuEl.value?.querySelector<HTMLElement>('[role=menuitem]:not([aria-disabled=true])')?.focus())
  }

  function closeMenu(restore = true) {
    const from = menu.value?.from
    menu.value = null
    if (restore && from) void nextTick(() => tree.focusRow(from))
  }

  function runItem(it: MenuItem) {
    if (it.disabled || it.sep) return
    closeMenu(false)
    it.run()
  }

  const onMenuKey = useMenuKeys({
    enabled: () => (menu.value?.items ?? []).map((it) => !it.sep && !it.disabled),
    current: () => menuIndex.value,
    moveTo: (i) => {
      menuIndex.value = i
      // menuIndex counts separators, so focus by item id rather than by position among menuitems.
      const id = menu.value?.items[i]?.id
      if (id) void nextTick(() => menuEl.value?.querySelector<HTMLElement>(`[data-testid="menu-${id}"]`)?.focus())
    },
    close: (restore) => closeMenu(restore),
    tab: 'restore',
  })

  function onDocClick(e: MouseEvent) {
    if (menu.value && !(e.target as HTMLElement).closest('.menu.floating')) closeMenu(false)
  }
  onMounted(() => document.addEventListener('mousedown', onDocClick))
  onUnmounted(() => document.removeEventListener('mousedown', onDocClick))

  return { menu, menuEl, menuIndex, openMenu, closeMenu, runItem, onMenuKey }
}
