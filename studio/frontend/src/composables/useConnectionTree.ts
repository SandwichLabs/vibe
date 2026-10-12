/**
 * useConnectionTree — the Connections rail's tree state (F10): which nodes are
 * expanded, the lazily loaded catalog levels, the flat treeitem rows built from
 * them (lib/railTree), and the roving-tabindex focus. Opening a workspace dials
 * nothing; expanding a connection connects it. A local connection that is
 * attached opens by itself, once; remote ones never dial on their own.
 */
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { App } from '../lib/bindings'
import { connectionList, catalogVersion, connect, refreshConnections, startConnectionTracking } from './useConnections'
import { workspaceInfo } from './useWorkspace'
import { errorText } from '../lib/errors'
import { buildRows, connKey, type Levels, type NodeKey, type Row } from '../lib/railTree'

export function useConnectionTree() {
  const expanded = ref<Set<string>>(new Set())
  const levels = ref<Levels>({})
  const autoOpened = new Set<string>()
  const filter = ref('')
  const focusKey = ref<NodeKey>('')
  const treeEl = ref<HTMLElement | null>(null)

  const q = computed(() => filter.value.trim().toLowerCase())
  const rows = computed<Row[]>(() => buildRows(connectionList.value, expanded.value, levels.value, q.value))
  const focusables = computed(() => rows.value.filter((r) => r.kind !== 'note'))
  const tabKey = computed(() => (focusables.value.some((r) => r.key === focusKey.value) ? focusKey.value : focusables.value[0]?.key ?? ''))

  // ---- loading levels ----
  async function load(key: string, req: { connection: string; schema: string; table: string }) {
    levels.value = { ...levels.value, [key]: { loading: true } }
    try {
      const l = await App.ListCatalog(req)
      levels.value = { ...levels.value, [key]: l }
    } catch (e: unknown) {
      levels.value = { ...levels.value, [key]: { error: errorText(e) } }
    }
  }

  async function expandConn(c: ConnectionInfo) {
    const ck = connKey(c.name)
    if (c.status !== 'ok') {
      const res = await connect(c.name)
      if (!res.ok) return
    }
    expanded.value = new Set(expanded.value).add(ck)
    if (!levels.value[ck] || 'error' in levels.value[ck]) await load(ck, { connection: c.name, schema: '', table: '' })
  }

  async function expandRow(r: Row) {
    if (r.kind === 'conn') return expandConn(r.conn)
    const next = new Set(expanded.value)
    next.add(r.key)
    expanded.value = next
    if (levels.value[r.key] && !('error' in levels.value[r.key])) return
    if (r.kind === 'schema') await load(r.key, { connection: r.conn.name, schema: r.schema!, table: '' })
    else await load(r.key, { connection: r.conn.name, schema: r.schema!, table: r.table! })
  }

  function collapseKey(key: string) {
    const next = new Set(expanded.value)
    next.delete(key)
    expanded.value = next
  }
  const collapseRow = (r: Row) => collapseKey(r.key)
  const collapseConn = (c: ConnectionInfo) => collapseKey(connKey(c.name))

  async function toggleRow(r: Row) {
    if (!r.expandable) return
    if (r.isOpen) collapseRow(r)
    else await expandRow(r)
  }

  // Reload every open level (after Refresh, DDL, or a catalog:changed event).
  async function reloadOpen(conn?: string) {
    const keys = Object.keys(levels.value)
    for (const k of keys) {
      if (!expanded.value.has(k) && !k.startsWith('c:')) continue
      const m = /^(c|s|t):([^/]+)(?:\/([^/]*))?(?:\/(.*))?$/.exec(k)
      if (!m) continue
      if (conn && m[2] !== conn) continue
      const [, kind, name, schema, table] = m
      if (kind === 'c') {
        if (!expanded.value.has(k)) continue
        await load(k, { connection: name, schema: '', table: '' })
      } else if (kind === 's') await load(k, { connection: name, schema: schema!, table: '' })
      else await load(k, { connection: name, schema: schema!, table: table! })
    }
  }

  watch(catalogVersion, () => { void reloadOpen() })

  // A local connection that is attached opens by itself, once (SPEC 05: local and
  // the other local DuckDB files start expanded; remote ones never dial).
  watch(connectionList, (list) => {
    for (const c of list) {
      const ck = connKey(c.name)
      if (!autoOpened.has(ck) && c.status === 'ok' && !c.remote && !c.catalogless) {
        autoOpened.add(ck)
        void expandConn(c)
      }
      // Gone, or no longer connected: forget its levels.
      if (c.status !== 'ok' && expanded.value.has(ck)) collapseKey(ck)
    }
  }, { immediate: true, deep: false })

  // A different workspace has different connections: start clean.
  watch(() => workspaceInfo.value?.sessionId, () => {
    expanded.value = new Set()
    levels.value = {}
    autoOpened.clear()
    void refreshConnections()
  })

  onMounted(() => {
    startConnectionTracking()
    void refreshConnections()
  })

  // ---- focus ----
  // rowEl finds a row by its key without building a selector from it.
  function rowEl(key: NodeKey): HTMLElement | null {
    const all = treeEl.value?.querySelectorAll<HTMLElement>('[data-key]')
    if (!all) return null
    for (const el of all) if (el.dataset.key === key) return el
    return null
  }

  function focusRow(key: NodeKey) {
    focusKey.value = key
    void nextTick(() => rowEl(key)?.focus())
  }

  return {
    expanded, filter, focusKey, treeEl, rows, focusables, tabKey,
    expandConn, expandRow, collapseRow, collapseConn, toggleRow, reloadOpen, rowEl, focusRow,
  }
}

export type ConnectionTree = ReturnType<typeof useConnectionTree>
