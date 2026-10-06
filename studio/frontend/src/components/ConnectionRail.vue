<script setup lang="ts">
// ConnectionRail — the DataGrip/DBeaver-style explorer under the nav (SPEC 05):
// connection > schema > table or view > column, loaded lazily. Opening a
// workspace dials nothing; expanding a connection connects it. One tab stop with
// a roving tabindex (WAI-ARIA tree), a context menu, status shapes, and under
// 1000px a drawer behind the plug button.
//
// The tree is rendered as one flat list of treeitems (aria-level / posinset),
// built from the lazily loaded levels, which keeps keyboard movement simple.
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
import StatusIcon from './StatusIcon.vue'
import ScopeBadge from './ScopeBadge.vue'
import ConnTypeTile from './ConnTypeTile.vue'
import {
  connectionList, connecting, catalogVersion, connect, openPromptFor, refreshConnections, startConnectionTracking,
} from '../composables/useConnections'
import { workspaceInfo, takeOverOpen } from '../composables/useWorkspace'
import { askConfirm } from '../composables/useConfirm'
import { errorText } from '../lib/errors'
import { detailLine, statusKey, selectFrom, scopeLabel, typeLabel, type DetailLine } from '../lib/connections'

const router = useRouter()
const emit = defineEmits<{ error: [message: string] }>()

// ---- state ----
type NodeKey = string // c:<conn> | s:<conn>/<schema> | t:<conn>/<schema>/<table>
const expanded = ref<Set<string>>(new Set())
const levels = ref<Record<string, CatalogListing | { loading: true } | { error: string }>>({})
const autoOpened = new Set<string>()
const filter = ref('')
const filterEl = ref<HTMLInputElement | null>(null)
const focusKey = ref<NodeKey>('')
const treeEl = ref<HTMLElement | null>(null)
const open = ref(false) // the narrow drawer
const narrow = ref(false)

interface Row {
  key: NodeKey
  kind: 'conn' | 'schema' | 'table' | 'view' | 'column' | 'note'
  level: number
  label: string
  conn: ConnectionInfo
  schema?: string
  table?: string
  node?: CatalogNode
  expandable: boolean
  isOpen: boolean
  parent?: NodeKey
  detail?: DetailLine | null
  noteText?: string
  setsize: number
  pos: number
}

const connKey = (n: string) => `c:${n}`
const schemaKey = (n: string, s: string) => `s:${n}/${s}`
const tableKey = (n: string, s: string, t: string) => `t:${n}/${s}/${t}`

const q = computed(() => filter.value.trim().toLowerCase())

function listing(key: string): CatalogListing | null {
  const v = levels.value[key]
  return v && 'nodes' in v ? v : null
}

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  const conns = connectionList.value
  conns.forEach((c, ci) => {
    const ck = connKey(c.name)
    const isOpen = expanded.value.has(ck)
    const detail = detailLine(c)
    out.push({ key: ck, kind: 'conn', level: 1, label: c.name, conn: c, expandable: !c.catalogless || c.status === 'ok', isOpen, detail, setsize: conns.length, pos: ci + 1 })
    if (!isOpen) return
    const top = levels.value[ck]
    if (!top) return
    if ('loading' in top) { out.push(note(ck, c, 2, 'Loading…')); return }
    if ('error' in top) { out.push(note(ck, c, 2, top.error)); return }
    if (c.catalogless) {
      out.push(note(ck, c, 2, 'Storage connection: read files with read_parquet(’scope/…’). It has no tables.'))
      return
    }
    if (top.kind === 'tables') {
      pushTables(out, c, top, 2, ck, top.schema)
      return
    }
    top.nodes.forEach((s, si) => {
      const sk = schemaKey(c.name, s.name)
      const sOpen = expanded.value.has(sk)
      out.push({ key: sk, kind: 'schema', level: 2, label: s.name, conn: c, schema: s.name, node: s, expandable: true, isOpen: sOpen, parent: ck, setsize: top.nodes.length, pos: si + 1 })
      if (!sOpen) return
      const sl = levels.value[sk]
      if (!sl) return
      if ('loading' in sl) { out.push(note(sk, c, 3, 'Loading…')); return }
      if ('error' in sl) { out.push(note(sk, c, 3, sl.error)); return }
      pushTables(out, c, sl, 3, sk, s.name)
    })
  })
  return out
})

function note(parent: string, c: ConnectionInfo, level: number, text: string): Row {
  return { key: `n:${parent}`, kind: 'note', level, label: text, conn: c, expandable: false, isOpen: false, parent, noteText: text, setsize: 1, pos: 1 }
}

function pushTables(out: Row[], c: ConnectionInfo, l: CatalogListing, level: number, parent: string, schema: string) {
  const f = q.value
  const nodes = l.nodes.filter((n) => {
    if (!f) return true
    if (n.name.toLowerCase().includes(f)) return true
    const cols = listing(tableKey(c.name, schema, n.name))
    return !!cols && cols.nodes.some((x) => x.name.toLowerCase().includes(f))
  })
  if (l.nodes.length === 0) {
    out.push(note(parent, c, level, 'No tables yet. Import a CSV, or ask the agent to build one.'))
    return
  }
  nodes.forEach((n, i) => {
    const tk = tableKey(c.name, schema, n.name)
    const isOpen = expanded.value.has(tk)
    out.push({ key: tk, kind: n.kind === 'view' ? 'view' : 'table', level, label: n.name, conn: c, schema, table: n.name, node: n, expandable: true, isOpen, parent, setsize: nodes.length, pos: i + 1 })
    if (!isOpen) return
    const cl = levels.value[tk]
    if (!cl) return
    if ('loading' in cl) { out.push(note(tk, c, level + 1, 'Loading…')); return }
    if ('error' in cl) { out.push(note(tk, c, level + 1, cl.error)); return }
    const cols = f && !n.name.toLowerCase().includes(f) ? cl.nodes.filter((x) => x.name.toLowerCase().includes(f)) : cl.nodes
    cols.forEach((col, k) => {
      out.push({ key: `col:${tk}/${col.name}`, kind: 'column', level: level + 1, label: col.name, conn: c, schema, table: n.name, node: col, expandable: false, isOpen: false, parent: tk, setsize: cols.length, pos: k + 1 })
    })
  })
}

const focusables = computed(() => rows.value.filter((r) => r.kind !== 'note'))
const tabKey = computed(() => (focusables.value.some((r) => r.key === focusKey.value) ? focusKey.value : focusables.value[0]?.key ?? ''))

// ---- loading levels ----
async function load(key: string, req: { connection: string; schema: string; table: string }) {
  levels.value = { ...levels.value, [key]: { loading: true } }
  try {
    const l = await window.go.main.App.ListCatalog(req)
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

function collapseRow(r: Row) {
  const next = new Set(expanded.value)
  next.delete(r.key)
  expanded.value = next
}

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
    if (c.status !== 'ok' && expanded.value.has(ck)) {
      const next = new Set(expanded.value)
      next.delete(ck)
      expanded.value = next
    }
  }
}, { immediate: true, deep: false })

// ---- activation ----
async function activate(r: Row) {
  if (r.kind === 'conn') {
    const c = r.conn
    switch (statusKey(c.status)) {
      case 'err': return router.push({ path: `/connections/${c.name}`, query: { test: '1' } })
      case 'creds': case 'ext': return void openPromptFor(c)
      case 'lock': takeOverOpen.value = true; return
      case 'ok': return toggleRow(r)
      default: return expandConn(c)
    }
  }
  if (r.kind === 'table' || r.kind === 'view') return openDatasheet(r)
  if (r.kind === 'schema') return toggleRow(r)
}

function openDatasheet(r: Row) {
  if (!r.node?.qualified) return
  closeDrawer()
  void router.push({ path: '/tables', query: { table: r.node.qualified } })
}

function newQueryHere(r: Row) {
  closeDrawer()
  const query: Record<string, string> = { conn: r.conn.name }
  if ((r.kind === 'table' || r.kind === 'view') && r.node?.qualified) query.sql = selectFrom(r.node.qualified)
  void router.push({ path: '/queries', query: { ...query, new: String(Date.now()) } })
}

function onDetailAction(r: Row) {
  const d = r.detail
  if (!d) return
  if (d.action === 'fix') void router.push({ path: `/connections/${r.conn.name}`, query: { test: '1' } })
  else if (d.action === 'takeover') takeOverOpen.value = true
  else openPromptFor(r.conn)
}

// ---- connection actions ----
async function disconnect(c: ConnectionInfo) {
  try {
    await window.go.main.App.DisconnectConnection({ name: c.name, scope: '' })
    const ck = connKey(c.name)
    const next = new Set(expanded.value)
    next.delete(ck)
    expanded.value = next
    await refreshConnections()
  } catch (e: unknown) { emit('error', errorText(e)) }
}

async function refreshConn(c?: ConnectionInfo) {
  try {
    await window.go.main.App.RefreshCatalog({ name: c?.name ?? '', scope: '' })
    await refreshConnections()
    await reloadOpen(c?.name)
  } catch (e: unknown) { emit('error', errorText(e)) }
}

async function testConn(c: ConnectionInfo) {
  closeDrawer()
  void router.push({ path: `/connections/${c.name}`, query: { test: '1' } })
}

function editConn(c: ConnectionInfo) {
  closeDrawer()
  void router.push(`/connections/${c.name}`)
}

function duplicateConn(c: ConnectionInfo) {
  closeDrawer()
  void router.push({ path: '/connections/new', query: { from: c.name } })
}

async function removeConn(c: ConnectionInfo) {
  const ok = await askConfirm({
    title: `Remove ${c.name}?`,
    message: `This removes the connection from ${scopeLabel(c.scope)}. Your database isn’t touched. Saved credentials in your keychain are also deleted.`,
    variant: 'destructive',
    confirmLabel: 'Remove',
  })
  if (!ok) return
  try {
    await window.go.main.App.DeleteConnection({ name: c.name, scope: c.scope })
    await refreshConnections()
  } catch (e: unknown) { emit('error', errorText(e)) }
}

async function copyName(r: Row) {
  try { await navigator.clipboard.writeText(r.node?.qualified ?? r.label) } catch { /* clipboard unavailable */ }
}

// ---- context menu ----
interface MenuItem { id: string; label: string; icon?: string; kbd?: string; danger?: boolean; disabled?: boolean; sep?: boolean; run: () => void }
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
        ? { id: 'disconnect', label: 'Disconnect', icon: 'unlock', disabled: own, run: () => disconnect(c) }
        : { id: 'connect', label: 'Connect', icon: 'plug', run: () => { void expandConn(c) } },
      { id: 'refresh', label: 'Refresh', icon: 'refresh', kbd: 'F5', disabled: !isOpen, run: () => refreshConn(c) },
      { id: 'newq', label: 'New query here', icon: 'terminal', kbd: 'Ctrl Alt N', run: () => newQueryHere(r) },
      { id: 's1', label: '', sep: true, run: () => {} },
      { id: 'edit', label: 'Edit…', icon: 'edit', kbd: 'Enter', run: () => editConn(c) },
      { id: 'test', label: 'Test', icon: 'zap', run: () => testConn(c) },
      { id: 'dup', label: 'Duplicate', icon: 'copy', run: () => duplicateConn(c) },
      { id: 's2', label: '', sep: true, run: () => {} },
      { id: 'remove', label: 'Remove…', icon: 'trash', kbd: 'Del', danger: true, run: () => removeConn(c) },
    ]
  }
  if (r.kind === 'table' || r.kind === 'view') {
    return [
      { id: 'tq', label: 'New query on this table', icon: 'terminal', run: () => newQueryHere(r) },
      { id: 'ds', label: 'Open as datasheet', icon: 'tableIcon', run: () => openDatasheet(r) },
      { id: 'cp', label: 'Copy name', icon: 'copy', run: () => copyName(r) },
    ]
  }
  if (r.kind === 'schema') return [{ id: 'cp', label: 'Copy name', icon: 'copy', run: () => copyName(r) }]
  return [{ id: 'cp', label: 'Copy name', icon: 'copy', run: () => copyName(r) }]
}

function openMenu(r: Row, at?: { x: number; y: number }) {
  let x = at?.x, y = at?.y
  if (x === undefined || y === undefined) {
    const el = rowEl(r.key)
    const b = el?.getBoundingClientRect()
    x = (b?.left ?? 80) + 40
    y = (b?.bottom ?? 120)
  }
  x = Math.min(x, window.innerWidth - 260)
  y = Math.min(y, window.innerHeight - 320)
  focusKey.value = r.key
  menu.value = { x, y, title: r.kind === 'conn' ? `${r.label} · ${typeLabel(r.conn.type)}` : r.label, items: itemsFor(r), from: r.key }
  menuIndex.value = menu.value.items.findIndex((i) => !i.sep && !i.disabled)
  void nextTick(() => menuEl.value?.querySelector<HTMLElement>('[role=menuitem]:not([aria-disabled=true])')?.focus())
}

function closeMenu(restore = true) {
  const from = menu.value?.from
  menu.value = null
  if (restore && from) void nextTick(() => focusRow(from))
}

function runItem(it: MenuItem) {
  if (it.disabled || it.sep) return
  closeMenu(false)
  it.run()
}

function onMenuKey(e: KeyboardEvent) {
  const m = menu.value
  if (!m) return
  const live = m.items.map((it, i) => ({ it, i })).filter((x) => !x.it.sep && !x.it.disabled)
  const pos = live.findIndex((x) => x.i === menuIndex.value)
  const go = (n: number) => {
    menuIndex.value = live[(n + live.length) % live.length].i
    void nextTick(() => menuEl.value?.querySelectorAll<HTMLElement>('[role=menuitem]')[menuIndex.value]?.focus())
  }
  switch (e.key) {
    case 'ArrowDown': e.preventDefault(); go(pos + 1); break
    case 'ArrowUp': e.preventDefault(); go(pos - 1); break
    case 'Home': e.preventDefault(); go(0); break
    case 'End': e.preventDefault(); go(live.length - 1); break
    case 'Escape': e.preventDefault(); e.stopPropagation(); closeMenu(); break
    case 'Tab': e.preventDefault(); closeMenu(); break
  }
}

// ---- keyboard (WAI-ARIA tree) ----
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

function onTreeKey(e: KeyboardEvent, r: Row) {
  const list = focusables.value
  const i = list.findIndex((x) => x.key === r.key)
  const move = (n: number) => { const t = list[Math.max(0, Math.min(list.length - 1, n))]; if (t) focusRow(t.key) }
  if (e.key === 'F10' && e.shiftKey || e.key === 'ContextMenu') { e.preventDefault(); openMenu(r); return }
  if (e.key === 'F5') { e.preventDefault(); e.stopPropagation(); void refreshConn(r.conn); return }
  if (e.ctrlKey && e.altKey && e.key.toLowerCase() === 'n') { e.preventDefault(); newQueryHere(r); return }
  if (e.ctrlKey || e.metaKey || e.altKey) return
  switch (e.key) {
    case 'ArrowDown': e.preventDefault(); move(i + 1); break
    case 'ArrowUp': e.preventDefault(); move(i - 1); break
    case 'Home': e.preventDefault(); move(0); break
    case 'End': e.preventDefault(); move(list.length - 1); break
    case 'ArrowRight':
      e.preventDefault()
      if (r.expandable && !r.isOpen) void expandRow(r)
      else if (r.isOpen) move(i + 1)
      break
    case 'ArrowLeft':
      e.preventDefault()
      if (r.isOpen) collapseRow(r)
      else if (r.parent) focusRow(r.parent)
      break
    case 'Enter': case ' ':
      e.preventDefault()
      if (r.kind === 'conn' && e.key === 'Enter' && r.conn.status === 'ok' && !r.detail) editConn(r.conn)
      else void activate(r)
      break
    case 'Delete':
      if (r.kind === 'conn') { e.preventDefault(); void removeConn(r.conn) }
      break
    case '*': {
      e.preventDefault()
      list.filter((x) => x.level === r.level && x.parent === r.parent && x.expandable && !x.isOpen).forEach((x) => { void expandRow(x) })
      break
    }
    case '/': e.preventDefault(); filterEl.value?.focus(); break
    default:
      if (e.key.length === 1 && /\S/.test(e.key)) {
        const ch = e.key.toLowerCase()
        const order = [...list.slice(i + 1), ...list.slice(0, i + 1)]
        const hit = order.find((x) => x.label.toLowerCase().startsWith(ch))
        if (hit) { e.preventDefault(); focusRow(hit.key) }
      }
  }
}

function onRowClick(e: MouseEvent, r: Row) {
  focusKey.value = r.key
  if ((e.target as HTMLElement).closest('.row-menu, .tn-note')) return
  if (r.kind === 'conn' && (e.target as HTMLElement).closest('.chev')) { void toggleRow(r); return }
  void activate(r)
}

// ---- drawer (narrow windows) ----
const mql = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(max-width: 1000px)') : null
function syncNarrow() { narrow.value = !!mql?.matches; if (!narrow.value) open.value = false }
function closeDrawer() { if (open.value) { open.value = false; void nextTick(() => document.querySelector<HTMLElement>('.rail-toggle')?.focus()) } }
function toggleDrawer() {
  open.value = !open.value
  if (open.value) void nextTick(() => (treeEl.value?.querySelector<HTMLElement>('[role=treeitem][tabindex="0"]') ?? filterEl.value)?.focus())
}
function onDrawerKey(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Escape' && !menu.value) { e.preventDefault(); closeDrawer(); return }
  if (e.key === 'Tab') {
    const root = document.querySelector('.conn-rail')
    const f = Array.from(root?.querySelectorAll<HTMLElement>('button:not([disabled]), input, [tabindex="0"]') ?? []).filter((x) => x.offsetParent !== null)
    if (!f.length) return
    const first = f[0], last = f[f.length - 1]
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
  }
}

function onDocClick(e: MouseEvent) {
  if (menu.value && !(e.target as HTMLElement).closest('.menu.floating')) closeMenu(false)
}

onMounted(() => {
  startConnectionTracking()
  void refreshConnections()
  mql?.addEventListener?.('change', syncNarrow)
  syncNarrow()
  document.addEventListener('keydown', onDrawerKey)
  document.addEventListener('mousedown', onDocClick)
})
onUnmounted(() => {
  mql?.removeEventListener?.('change', syncNarrow)
  document.removeEventListener('keydown', onDrawerKey)
  document.removeEventListener('mousedown', onDocClick)
})

// A different workspace has different connections: start clean.
watch(() => workspaceInfo.value?.sessionId, () => {
  expanded.value = new Set()
  levels.value = {}
  autoOpened.clear()
  void refreshConnections()
})

const onlyLocal = computed(() => connectionList.value.length === 1 && !connectionList.value[0].remote)
const rowDescribedBy = (r: Row) => (r.detail ? `${domId(r.key)}-d` : undefined)
function domId(k: string) { return 'rail-' + k.replace(/[^a-zA-Z0-9_-]/g, '_') }
function rowsText(n: number) { return n < 0 ? '' : `~${n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e3 ? Math.round(n / 1e3) + 'K' : n}` }
function colMeta(r: Row) {
  const n = r.node!
  return { type: n.dataType ?? '', tag: n.pk ? 'PK' : n.fk ? 'FK' : '' }
}
defineExpose({ rows, toggleDrawer, refreshConn, expanded })
</script>

<template>
  <section class="conn-rail" :class="{ open }" aria-label="Connections" data-testid="conn-rail">
    <div class="rail-head">
      <button
        class="icon-btn rail-toggle"
        type="button"
        :aria-expanded="open"
        aria-controls="conn-tree-wrap"
        aria-label="Connections"
        title="Connections"
        data-testid="rail-toggle"
        @click="toggleDrawer"
      ><Icon name="plug" :size="16" /></button>
      <span class="side-section rail-title">Connections</span>
      <router-link class="icon-btn keep" to="/connections/new" aria-label="New connection" title="New connection" data-testid="rail-add" @click="closeDrawer"><Icon name="plus" :size="16" /></router-link>
      <button class="icon-btn keep" type="button" aria-label="Refresh all connections" title="Refresh all (F5)" data-testid="rail-refresh" @click="refreshConn()"><Icon name="refresh" :size="16" /></button>
    </div>

    <div id="conn-tree-wrap" class="tree-wrap">
      <!-- The drawer (narrow windows) carries its own header: title, new, refresh. -->
      <div class="rail-head drawer-head">
        <span class="side-section rail-title">Connections</span>
        <router-link class="icon-btn" to="/connections/new" aria-label="New connection" title="New connection" data-testid="drawer-add" @click="closeDrawer"><Icon name="plus" :size="16" /></router-link>
        <button class="icon-btn" type="button" aria-label="Refresh all connections" title="Refresh all (F5)" data-testid="drawer-refresh" @click="refreshConn()"><Icon name="refresh" :size="16" /></button>
      </div>
      <div class="rail-filter">
        <label class="sr" for="rail-filter-input">Filter tables and columns</label>
        <Icon name="search" :size="14" />
        <input id="rail-filter-input" ref="filterEl" v-model="filter" type="text" placeholder="Filter tables and columns" autocomplete="off" data-testid="rail-filter" />
      </div>

      <div ref="treeEl" class="tree" role="tree" aria-label="Connections" data-testid="rail-tree">
        <div
          v-for="r in rows"
          :key="r.key"
          :data-key="r.key"
          :role="r.kind === 'note' ? 'none' : 'treeitem'"
          :aria-level="r.kind === 'note' ? undefined : r.level"
          :aria-setsize="r.kind === 'note' ? undefined : r.setsize"
          :aria-posinset="r.kind === 'note' ? undefined : r.pos"
          :aria-expanded="r.kind !== 'note' && r.expandable ? r.isOpen : undefined"
          :aria-selected="r.kind !== 'note' ? tabKey === r.key : undefined"
          :aria-busy="r.kind === 'conn' && connecting.has(r.conn.name) ? 'true' : undefined"
          :aria-describedby="r.kind === 'conn' ? rowDescribedBy(r) : undefined"
          :aria-label="r.kind === 'conn' ? `${r.label}, ${typeLabel(r.conn.type)}, ${scopeLabel(r.conn.scope)}, ${r.conn.status === 'ok' ? 'connected' : r.conn.status === 'not-connected' ? 'not connected' : r.conn.status}` : undefined"
          :tabindex="r.kind === 'note' ? undefined : tabKey === r.key ? 0 : -1"
          :data-testid="r.kind === 'conn' ? 'conn-row' : r.kind === 'note' ? 'rail-note' : 'tree-row'"
          :data-kind="r.kind"
          :data-name="r.label"
          @keydown="r.kind !== 'note' && onTreeKey($event, r)"
          @contextmenu.prevent="r.kind !== 'note' && openMenu(r, { x: $event.clientX, y: $event.clientY })"
        >
          <!-- A note row (loading, empty, error) is plain text under its parent. -->
          <div v-if="r.kind === 'note'" class="tn-note" :style="{ paddingLeft: 14 + r.level * 14 + 'px' }">{{ r.noteText }}</div>

          <template v-else>
            <div
              class="tn"
              :class="{ conn: r.kind === 'conn', sel: tabKey === r.key && false }"
              :style="{ paddingLeft: 4 + (r.level - 1) * 14 + 'px' }"
              :title="r.kind === 'conn' ? `${r.label} · ${typeLabel(r.conn.type)} · ${statusKey(r.conn.status)}` : undefined"
              @click="onRowClick($event, r)"
            >
              <Icon v-if="r.expandable" :name="r.isOpen ? 'chevronDown' : 'chevronRight'" :size="14" class="chev" />
              <Icon v-else name="chevronRight" :size="14" class="chev none" />

              <template v-if="r.kind === 'conn'">
                <ConnTypeTile :type="r.conn.type" />
                <span class="nm">{{ r.label }}</span>
                <Icon v-if="r.conn.readOnly && r.conn.type !== 'motherduck'" name="lock" :size="12" class="ro" aria-label="Read-only" title="Read-only" />
                <Icon v-if="r.conn.shadows?.length" name="layers" :size="12" class="ro" aria-label="Overrides another copy" title="This computer's copy overrides a Team or All-my-workspaces one of the same name" />
                <span class="rowtail">
                  <ScopeBadge :scope="r.conn.scope" />
                  <span v-if="connecting.has(r.conn.name)" class="spin" role="img" aria-label="Connecting"></span>
                  <StatusIcon v-else :status="r.conn.status" />
                  <button class="icon-btn row-menu" type="button" tabindex="-1" :aria-label="`Actions for ${r.label}`" @click.stop="openMenu(r)"><Icon name="more" :size="14" /></button>
                </span>
              </template>
              <template v-else-if="r.kind === 'schema'">
                <Icon name="layers" :size="14" class="ico" />
                <span class="nm">{{ r.label }}</span>
              </template>
              <template v-else-if="r.kind === 'table' || r.kind === 'view'">
                <Icon :name="r.kind === 'view' ? 'eye' : 'tableIcon'" :size="14" class="ico" />
                <span class="nm">{{ r.label }}</span>
                <span class="meta">{{ r.kind === 'view' ? 'view' : rowsText(r.node?.rows ?? -1) }}</span>
              </template>
              <template v-else>
                <Icon :name="r.node?.pk ? 'key' : r.node?.fk ? 'link' : 'columns'" :size="14" class="ico" />
                <span class="nm">{{ r.label }}</span>
                <span class="meta" :class="r.node?.pk ? 'pk' : r.node?.fk ? 'fk' : 'type'">{{ colMeta(r).type }}<template v-if="colMeta(r).tag"> &middot; {{ colMeta(r).tag }}</template></span>
              </template>
            </div>
            <div v-if="r.kind === 'conn' && r.detail" :id="rowDescribedBy(r)" class="tn-note" :class="statusKey(r.conn.status) === 'err' ? 'err' : 'warn'" role="note" :style="{ paddingLeft: '34px' }" data-testid="conn-detail">
              {{ r.detail.text }}
              <button class="linkbtn" type="button" tabindex="-1" @click.stop="onDetailAction(r)">{{ r.detail.actionLabel }}</button>
            </div>
          </template>
        </div>

        <div v-if="onlyLocal" class="rail-empty" data-testid="rail-empty">
          <b>Your data somewhere else?</b>
          Connect Postgres, MotherDuck, a SQLite file or cloud storage. Nothing connects until you use it.
          <div style="margin-top: 10px;"><router-link class="btn btn-sm" to="/connections/new" data-testid="rail-add-card" @click="closeDrawer"><Icon name="plus" :size="14" /> Add a connection</router-link></div>
        </div>
        <div v-else-if="connectionList.length === 0" class="rail-empty" data-testid="rail-empty-none">
          <b>No connections</b>
          Add one to read data where it lives.
        </div>
      </div>
    </div>

    <!-- Context menu -->
    <div
      v-if="menu"
      ref="menuEl"
      class="menu floating"
      role="menu"
      :aria-label="`Actions for ${menu.title}`"
      :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
      data-testid="rail-menu"
      @keydown="onMenuKey"
    >
      <div class="mhead">{{ menu.title }}</div>
      <template v-for="(it, i) in menu.items" :key="it.id">
        <div v-if="it.sep" class="msep" role="separator"></div>
        <button
          v-else
          type="button"
          role="menuitem"
          class="mi"
          :class="{ danger: it.danger, disabled: it.disabled }"
          :aria-disabled="it.disabled ? 'true' : undefined"
          :tabindex="menuIndex === i ? 0 : -1"
          :data-testid="`menu-${it.id}`"
          @click="runItem(it)"
          @mouseenter="menuIndex = i"
        >
          <Icon v-if="it.icon" :name="it.icon" :size="16" />
          <span class="grow"><span class="t">{{ it.label }}</span></span>
          <span v-if="it.kbd" class="end"><kbd>{{ it.kbd }}</kbd></span>
        </button>
      </template>
    </div>
  </section>
</template>
