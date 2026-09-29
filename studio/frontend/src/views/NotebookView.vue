<script setup lang="ts">
// NotebookView — the notebook editor (#66 part A): cells rendered in order,
// each an sql / markdown / a2ui cell. SQL cells synthesize a one-component
// A2UI page from their result and render it through A2UIPage — the SAME
// renderer #67's agent-authored dashboards use, so a hand-written cell and
// an agent-written one look identical.
//
// Outputs are never persisted: a cell's `result` lives only in this
// component's local state (never sent to SaveNotebook, which only ever
// carries source/view/page — see notebooks.Coerce). Every sql cell re-runs
// on open (runAll in onMounted).
//
// Part B seam: the "Generate with AI" affordance below is a disabled
// placeholder behind AI_CELL_ENABLED. When task-mode authoring (add_query_cell
// / create_dashboard tools, issue #66 part B) lands, flip the flag and wire
// its click handler to open the AI-cell dialog — no other change to this
// file should be required, since cells are added the same way (insertCell)
// whether typed by hand or proposed by the agent.
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import Icon from '../components/Icon.vue'
import A2UIPage from '../a2ui/A2UIPage.vue'
import { runLiveQuery, toPoints } from '../a2ui/query'
import type { A2UIPageT } from '../a2ui/types'
import { createVibeEditor, type VibeEditorInstance } from '../composables/useVibeEditor'
import { renderSafeMarkdown } from '../lib/markdown'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { register as registerPendingSave } from '../composables/usePendingSaves'
import { describeShape, viewFit, bestView, VIEWS, VIEW_LABELS, VIEW_COMPONENT, VIEW_HINTS, type ViewKind, type Shape } from '../lib/notebookViews'
import { cellsToMerge, pruneConfirmedTombstones } from '../lib/notebookMerge'
import AiCellDialog from '../components/AiCellDialog.vue'
import { useAiCellDialog, type EnvelopeCell } from '../composables/useAiCellDialog'
import { errorText } from '../lib/errors'
import CellDrawer from '../components/CellDrawer.vue'
import { sqlSummary, cellTitle, cellMeta, CELL_FALLBACK_TITLE } from '../lib/cellSummary'

const hasWailsRuntime = () => typeof window !== 'undefined' && !!(window as any).runtime

// Part B seam (see file header): flipped on now that task-mode authoring
// (add_query_cell/create_dashboard tools + the AgentTask binding, #66 part
// B) has landed.
const AI_CELL_ENABLED = true

interface RawCell {
  id: string
  type: 'sql' | 'markdown' | 'a2ui'
  collapsed?: boolean
  source?: string
  view?: ViewKind
  viewPinned?: boolean
  label?: string
  page?: A2UIPageT
}

interface CellState {
  id: string
  type: 'sql' | 'markdown' | 'a2ui'
  collapsed: boolean
  source: string
  view: ViewKind
  viewPinned: boolean
  label: string
  page: A2UIPageT | null
  // Runtime-only (never saved):
  lastRunSource: string | null
  result: { columns: string[]; types: string[]; rows: any[][]; error?: string } | null
  running: boolean
  mdPreview: boolean
  // SQL drawer / a2ui card definition open. Runtime-only, never saved (not
  // in buildSavePayload or the autosave key).
  sourceOpen: boolean
  editorEl?: HTMLElement | null
  editor?: VibeEditorInstance | null
  // Lazy-mount (review fix, step 4a #1): an a2ui cell's A2UIPage — and every
  // live query its data-bound components fire — is not rendered until the
  // cell has actually scrolled into view, via the IntersectionObserver set
  // up in observeCell(). A notebook packed with many a2ui cells (bounded,
  // but still up to 500) never fires more than a screenful of live queries
  // at once just from being opened.
  visible: boolean
  rootEl?: HTMLElement | null
}

const route = useRoute()
const router = useRouter()

const title = ref('')
const cells = ref<CellState[]>([])
const loading = ref(true)
const notFound = ref(false)
const errorMessage = ref('')
// infoMessage is the neutral/marigold toast (coordinator follow-up #2) — for
// informational notices that are NOT errors (e.g. the AI-cell apply note),
// so they never appear in the red --sh-error toast, which the SchoolHouse
// palette reserves for destructive/failed states.
const infoMessage = ref('')
// statusMessage is a success confirmation (#91: notebook export) — same
// click-to-dismiss shape as infoMessage, plus a 4s auto-dismiss timeout.
const statusMessage = ref('')
const saveState = ref<'idle' | 'saving' | 'saved'>('idle')
let saveTimer: ReturnType<typeof setTimeout> | null = null
let suppressAutosave = true // true while loading, so the initial populate doesn't trigger a save

// loadedNotebookId is the id THIS component instance actually loaded —
// captured once, at load time. Review fix, step 4a #3: vue-router reuses
// the same component instance across a /notebooks/:id -> /notebooks/:id2
// navigation (only the route param changes), so anything derived live from
// `route.params.id` (as the previous `notebookId = computed(...)` was) goes
// stale mid-navigation: a debounced save that fires after the route has
// already changed would build its payload with the NEW id and silently
// overwrite the wrong notebook. Every save below reads loadedNotebookId,
// never the live route.
let loadedNotebookId = ''

// lastPersisted is the JSON.stringify of the last payload known to match
// what the server has (set after loadNotebook populates, after a
// successful saveNow, and after a successful doFlush — #95 carry-over):
// doFlush skips its own save entirely when the current payload matches, so
// a no-edit visit (route-leave/unmount with nothing changed since load or
// the last save) never overwrites a save that happened elsewhere (e.g. an
// agent append merged in, or a concurrent MCP write) with a stale local
// copy. Reset to '' wherever loadedNotebookId is cleared.
//
// saveSeq guards against a narrow ordering race: if a doFlush (e.g. from a route-leave) starts and finishes
// WHILE an earlier saveNow's await is still in flight, that earlier
// saveNow resolving afterward must not stomp lastPersisted back to its own
// (now-stale) payload. Every save grabs the next seq before awaiting the
// binding and only writes lastPersisted if it is still the most recently
// STARTED save when it resolves — an in-flight save that got superseded by
// a later one silently skips updating the baseline.
let lastPersisted = ''
let saveSeq = 0

// loadedSessionId is the file-open session (studio.App.sessionID, surfaced
// via GetFileInfo().session_id) in effect when THIS component instance
// loaded its notebook — captured once, alongside loadedNotebookId, and
// never re-fetched afterward. Every save sends it back to SaveNotebook,
// which rejects a save whose token no longer matches the CURRENTLY open
// file. This closes a real data-corruption bug (reviewer-caught, live-
// proven): calling OpenFile/CreateFile/CreateFromTemplate while this view
// was still mounted for the PREVIOUS file let an in-flight debounced save
// land in the NEW container, because the container pointer had already
// been swapped by the time the old save fired. Using a value captured at
// load time (not a live/reactive "current session" ref) matters — a
// shared, live-updating ref would give the OLD view the NEW session id the
// instant the file switched, defeating the whole check.
let loadedSessionId = ''

// loadAbort is this component's current "generation" — a fresh
// AbortController per loadNotebook() call. Aborting it tells every
// in-flight runLiveQuery call (sql cells' own runs, and every a2ui cell's
// data-bound components, via A2UIPage's `signal` prop) that this load no
// longer matters, so a result that resolves after a notebook switch or
// unmount is discarded instead of being applied to now-torn-down state.
let loadAbort: AbortController | null = null

let cellObserver: IntersectionObserver | null = null

function newCellState(raw: RawCell): CellState {
  return reactive({
    id: raw.id,
    type: raw.type,
    collapsed: !!raw.collapsed,
    source: raw.source ?? '',
    view: (raw.view as ViewKind) ?? 'table',
    viewPinned: !!raw.viewPinned,
    label: raw.label ?? '',
    page: raw.page ?? null,
    lastRunSource: null,
    result: null,
    running: false,
    mdPreview: (raw.source ?? '').trim() !== '',
    sourceOpen: raw.type === 'sql' && !(raw.source ?? '').trim(),
    editorEl: null,
    editor: null,
    visible: raw.type !== 'a2ui', // only a2ui cells start hidden/lazy
    rootEl: null,
  }) as CellState
}

function observeCell(cell: CellState, el: Element | null) {
  cell.rootEl = el as HTMLElement | null
  if (cell.type !== 'a2ui' || cell.visible) return
  if (!el || !cellObserver) return
  cellObserver.observe(el)
}

function setupCellObserver() {
  cellObserver?.disconnect()
  if (typeof IntersectionObserver === 'undefined') {
    // jsdom (Vitest) and some embedded webviews have no IntersectionObserver
    // — degrade to "always visible" rather than leaving a2ui cells stuck
    // permanently unmounted.
    for (const cell of cells.value) cell.visible = true
    return
  }
  cellObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        const id = (entry.target as HTMLElement).dataset.cellId
        const cell = cells.value.find((c) => c.id === id)
        if (cell) cell.visible = true
        cellObserver?.unobserve(entry.target)
      }
    },
    { rootMargin: '200px' },
  )
  for (const cell of cells.value) {
    if (cell.type === 'a2ui' && !cell.visible && cell.rootEl) cellObserver.observe(cell.rootEl)
  }
}

async function loadNotebook(id: string) {
  loadAbort?.abort()
  loadAbort = new AbortController()
  cellObserver?.disconnect()
  cellObserver = null

  loading.value = true
  notFound.value = false
  suppressAutosave = true
  loadedNotebookId = id
  // A fresh load already reflects current server state once it resolves —
  // anything queued for whatever was loading/loaded before this is moot.
  pendingNotebooksChangedQueue = []
  try {
    const [nb, fileInfo] = await Promise.all([
      window.go.main.App.GetNotebook(id),
      typeof window.go.main.App.GetFileInfo === 'function'
        ? window.go.main.App.GetFileInfo().catch(() => null)
        : Promise.resolve(null),
    ])
    if (loadedNotebookId !== id) return // superseded by another load before this one resolved
    loadedSessionId = fileInfo?.session_id ?? ''
    title.value = nb.title
    cells.value = (nb.cells || []).map((c: RawCell) => newCellState(c))
    lastPersisted = JSON.stringify(buildSavePayload(id, title.value, cells.value))
    // loading must flip BEFORE the nextTick below — the cell list template
    // is behind `v-if="!loading"`, so while loading is still true the cell
    // DOM (and therefore every editorEl ref) does not exist yet, and
    // mountEditors() would silently no-op for every cell.
    loading.value = false
    await nextTick()
    mountEditors()
    setupCellObserver()
    if (hasWailsRuntime()) window.go.main.App.SetActiveNotebook(id)
    await runAll()
    await replayPendingNotebooksChanged(id)
  } catch (e: any) {
    notFound.value = true
    errorMessage.value = 'Failed to load notebook: ' + errorText(e)
    loading.value = false
  }
  await nextTick()
  if (loadedNotebookId === id) suppressAutosave = false
}

function mountEditors() {
  for (const cell of cells.value) {
    if (cell.type === 'a2ui') continue
    if (cell.collapsed) continue
    if (cell.type === 'sql' && !cell.sourceOpen) continue
    if (!cell.editorEl || cell.editor) continue
    cell.editor = createVibeEditor(cell.editorEl, {
      value: cell.source,
      lang: cell.type === 'sql' ? 'sql' : 'markdown',
      onChange: (v) => { cell.source = v },
      extraKeymap: cell.type === 'sql'
        ? [{ key: 'Mod-Enter', run: () => { runCell(cell); return true } }]
        : [],
    })
  }
}

function shapeOf(cell: CellState): Shape | null {
  if (!cell.result || cell.result.error) return null
  return describeShape(cell.result.columns, cell.result.rows, cell.result.types)
}

function isStale(cell: CellState): boolean {
  return cell.type === 'sql' && cell.lastRunSource !== null && cell.lastRunSource !== cell.source.trim()
}

async function runCell(cell: CellState) {
  if (cell.type !== 'sql') return
  cell.running = true
  const signal = loadAbort?.signal
  // 'high' priority (review fix, step 4a round 2 #5): a notebook sql cell's
  // Run button/Ctrl-Enter is a direct, user-initiated action — it must
  // never wait behind a background a2ui component storm's auto-fetches
  // (which stay 'low', the default), which is exactly what "returns in
  // under 1s regardless of how many a2ui components are mounted" requires.
  const res = await runLiveQuery(cell.source, signal, 'high')
  if (res.aborted) return // this load was superseded (notebook switch / unmount) — discard
  cell.running = false
  cell.lastRunSource = cell.source.trim()
  if (res.error) {
    cell.result = { columns: [], types: [], rows: [], error: res.error }
    // Any run that errors opens the drawer so the failing SQL is visible —
    // it never auto-closes afterward, even once a later run succeeds.
    cell.sourceOpen = true
    nextTick(mountEditors)
    return
  }
  cell.result = { columns: res.columns, types: res.types, rows: res.rows }
  if (!cell.viewPinned) {
    cell.view = bestView(describeShape(res.columns, res.rows, res.types))
  }
}

async function runAll() {
  for (const cell of cells.value) {
    if (cell.type === 'sql' && cell.source.trim()) {
      await runCell(cell)
    }
  }
}

function pickView(cell: CellState, view: ViewKind) {
  const shape = shapeOf(cell)
  cell.view = view
  cell.viewPinned = true
  if (shape) {
    const fit = viewFit(view, shape)
    if (!fit.ok) {
      // Still honor the person's explicit choice — the mismatch note (fit.why)
      // is shown inline instead of silently overriding it.
    }
  }
}

// synthesizedPage builds the one-component A2UI page a sql cell's result
// renders as. Review fix, step 4a #10: the label is set on EITHER the page
// title OR the component itself, never both — every leaf component
// (A2UIStat/Table/BarChart/LineChart) already renders `component.label` as
// its own caption, so also setting `page.title` to the same text rendered
// it a second time, right above the first, on every sql cell that had a
// label. The page itself never needs its own title here: it holds exactly
// one component, whose own caption is enough.
function synthesizedPage(cell: CellState): A2UIPageT | null {
  if (!cell.result || cell.result.error) return null
  const { columns, rows, types } = cell.result
  const componentType = VIEW_COMPONENT[cell.view]
  const base = { id: 'c', type: componentType, label: cell.label || undefined }
  let fields: Record<string, any> = {}
  switch (cell.view) {
    case 'table':
      // types is client-only: stamped on the synthesized component so
      // A2UITable can format each column with its real DuckDB type (DECIMAL
      // scale, BIGINT grouping, …). Never part of the A2UI schema and never
      // persisted — synthesizedPage's output is render-only (a2ui/types.ts).
      fields = { columns, rows, types }
      break
    case 'bar':
      fields = { bars: toPoints(columns, rows) }
      break
    case 'line':
      fields = { points: toPoints(columns, rows) }
      break
    case 'stat':
      // a2ui's "stat" type requires a label — fall back to a column name or
      // a generic "Result" rather than an empty required field.
      fields = { label: cell.label || columns[0] || 'Result', value: rows[0]?.[0] ?? null, types: [types[0]] }
      break
  }
  return { title: '', components: [{ ...base, ...fields }] }
}

// mismatchInfo describes why the cell's current (pinned) view does not fit
// its last result, for the mismatch note — ported from notebook.js's
// #mismatchNote: a VIEW_LABEL-prefixed reason, the shape actually returned,
// and which OTHER views would fit, each offered as a one-click switch
// (review fix, step 4a #9).
function mismatchInfo(cell: CellState): { label: string; hint: string; shapeLine: string; fits: ViewKind[] } | null {
  const shape = shapeOf(cell)
  if (!shape) return null
  const fit = viewFit(cell.view, shape)
  if (fit.ok) return null
  const numeric = shape.numeric.length ? shape.numeric.join(', ') : 'none'
  return {
    label: VIEW_LABELS[cell.view],
    hint: VIEW_HINTS[cell.view],
    shapeLine: `this query returned ${shape.rowCount} row${shape.rowCount === 1 ? '' : 's'}, ${shape.cols.length} column${shape.cols.length === 1 ? '' : 's'} (numeric: ${numeric})`,
    fits: VIEWS.filter((v) => v !== cell.view && viewFit(v, shape).ok),
  }
}

// --- cell list ops ---

function addCell(type: CellState['type'], atEnd = true) {
  const id = 'cell_' + Math.random().toString(36).slice(2, 10)
  const raw: RawCell = type === 'sql'
    ? { id, type, source: '', view: 'table', viewPinned: false, label: '' }
    : type === 'markdown'
      ? { id, type, source: '' }
      // internal/a2ui.Validate rejects zero components, so a freshly added
      // a2ui cell needs a placeholder or it would fail Coerce on the very
      // next autosave.
      : { id, type, page: { title: 'Untitled', components: [{ id: 'placeholder', type: 'text', text: 'Empty A2UI cell — edit the source below.' }] } }
  const cell = newCellState(raw)
  if (type === 'markdown') cell.mdPreview = false
  if (atEnd) cells.value.push(cell)
  else cells.value.unshift(cell)
  // sourceOpen is already true for a hand-added sql cell (its source starts
  // empty — see newCellState), so mounting it right away and focusing the
  // editor lets the person start typing immediately.
  nextTick(() => { mountEditors(); if (type === 'sql') cell.editor?.view.focus() })
}

// --- AI cell authoring (#66 part B) ---

// insertCellFromEnvelope/replaceCellFromEnvelope convert a task-mode
// EnvelopeCell (the agent's proposal) into a RawCell and apply it the same
// way addCell does for a hand-added one — same newCellState, same
// mountEditors — so an agent-authored cell is indistinguishable from a
// hand-typed one the moment it lands.
function envelopeToRaw(id: string, ec: EnvelopeCell): RawCell {
  if (ec.type === 'sql') return { id, type: 'sql', source: ec.sql || '', view: (ec.view as ViewKind) || 'table', viewPinned: !!ec.view, label: ec.label || '' }
  return { id, type: 'markdown', source: ec.markdown || '' }
}

// insertCellFromEnvelope inserts right after afterCellId's CURRENT position
// (re-looked-up here, not trusted from a stale index the caller might have
// captured earlier) when given, so the drift/C2 "added as a new cell below"
// path lands the new cell where "below" actually means — coordinator
// follow-up #1. With no afterCellId (create mode; no live target to be
// below) it appends at the end, same as before.
// AI-authored sql cells stay closed (newCellState's sourceOpen is only true
// for an EMPTY source, and the agent always proposes actual SQL) — if the
// follow-up auto-run below errors, runCell's own error-open path takes care
// of it, which is exactly the "any run that errors" rule.
function insertCellFromEnvelope(ec: EnvelopeCell, afterCellId?: string) {
  const id = 'cell_' + Math.random().toString(36).slice(2, 10)
  const cell = newCellState(envelopeToRaw(id, ec))
  if (ec.type === 'markdown') cell.mdPreview = false
  const afterIdx = afterCellId ? cells.value.findIndex((c) => c.id === afterCellId) : -1
  if (afterIdx !== -1) cells.value.splice(afterIdx + 1, 0, cell)
  else cells.value.push(cell)
  nextTick(mountEditors)
  if (cell.type === 'sql') nextTick(() => runCell(cell))
}

// replaceCellFromEnvelope destroys the target cell's editor first (so a
// stale CodeMirror instance never lingers bound to the old source) then
// swaps its whole entry in place — never removes+re-adds, so its position
// in the notebook is preserved.
function replaceCellFromEnvelope(cellId: string, ec: EnvelopeCell) {
  const idx = cells.value.findIndex((c) => c.id === cellId)
  if (idx === -1) { insertCellFromEnvelope(ec); return } // target vanished mid-task — never silently drop the proposal
  const previousSourceOpen = cells.value[idx].sourceOpen
  cells.value[idx].editor?.destroy()
  const replacement = newCellState(envelopeToRaw(cellId, ec))
  if (ec.type === 'markdown') replacement.mdPreview = false
  // A drawer the person had open to read/compare the SQL must not snap shut
  // just because the AI replaced its content — "never auto-closes" applies
  // here too, not just to a run error. A closed drawer stays closed
  // (newCellState's own default for non-empty SQL already covers that).
  replacement.sourceOpen = previousSourceOpen || replacement.sourceOpen
  cells.value.splice(idx, 1, replacement)
  nextTick(mountEditors)
  if (replacement.type === 'sql') nextTick(() => runCell(replacement))
}

// snapshotAiCell/loadAiCellTables/aiCellNotebookSummary feed the AI-cell
// dialog's drift detection (review fix, finding 6) and request context
// (finding 7) — kept as small standalone functions (not inlined in the
// useAiCellDialog call) so they read naturally against this component's own
// cells/window.go bindings.
function snapshotAiCell(id: string) {
  const c = cells.value.find((c) => c.id === id)
  if (!c) return null
  return { source: c.source, label: c.label, view: c.view }
}
async function loadAiCellTables(): Promise<string[]> {
  if (!hasWailsRuntime()) return []
  try {
    return (await window.go.main.App.ListTables()) || []
  } catch {
    return []
  }
}
function aiCellNotebookSummary(): string[] {
  return cells.value.map(cellTitle).filter((s) => s.trim() !== '')
}

const aiCellDialog = useAiCellDialog({
  cells,
  insertCell: insertCellFromEnvelope,
  replaceCell: replaceCellFromEnvelope,
  snapshotCell: snapshotAiCell,
  getTables: loadAiCellTables,
  getNotebookSummary: aiCellNotebookSummary,
})

function openAiCellCreate() {
  aiCellDialog.openCreate()
}

// onAiCellApplied schedules a save and, when apply had to insert beside
// instead of replacing (a drifted edit or a type mismatch — review fix,
// finding 6), surfaces that as a toast so the user knows why they now have
// an extra cell instead of the one they expected replaced.
function onAiCellApplied(note: string) {
  scheduleSave()
  if (note) infoMessage.value = note
}
function openAiCellEdit(cell: CellState, titleOverride?: string) {
  aiCellDialog.openEdit({ id: cell.id, type: cell.type }, titleOverride)
}

// tombstones holds ids deleted locally but not yet CONFIRMED excluded by a
// successful save (review fix, finding 2) — cellsToMerge/handleNotebooksChanged
// consult this so a "notebooks:changed" event (our own save echo, or a
// remote agent append that raced the delete and read the pre-delete
// notebook) can never resurrect a cell the user just removed. Pruned in
// saveNow/doFlush once a save that excludes the id actually succeeds — see
// pruneConfirmedTombstones.
let tombstones = new Set<string>()

function deleteCell(cell: CellState) {
  cell.editor?.destroy()
  cells.value = cells.value.filter((c) => c.id !== cell.id)
  tombstones.add(cell.id)
}

function moveCell(cell: CellState, delta: number) {
  const i = cells.value.findIndex((c) => c.id === cell.id)
  const j = i + delta
  if (i === -1 || j < 0 || j >= cells.value.length) return
  const arr = cells.value.slice()
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  cells.value = arr
}

function toggleMdPreview(cell: CellState) {
  if (!cell.mdPreview) {
    // Leaving edit mode: tear down the editor so its DOM node (about to be
    // removed by the v-if swap) doesn't leak a live CodeMirror instance.
    cell.editor?.destroy()
    cell.editor = null
  }
  cell.mdPreview = !cell.mdPreview
  if (!cell.mdPreview) nextTick(mountEditors)
}

function toggleCollapsed(cell: CellState) {
  cell.collapsed = !cell.collapsed
  if (!cell.collapsed) nextTick(mountEditors)
  else { cell.editor?.destroy(); cell.editor = null }
}

// toggleSource opens/closes the SQL drawer (or the a2ui "card definition"
// drawer). It does NOT destroy the editor on close — nothing writes
// cell.source while an editor is unmounted-but-alive (merge only appends
// cells; replace destroys and rebuilds), so keeping it mounted preserves
// undo history across a peek.
function toggleSource(cell: CellState) {
  cell.sourceOpen = !cell.sourceOpen
  if (cell.sourceOpen) nextTick(mountEditors)
}

const CELL_ICON = { sql: 'terminal', markdown: 'fileText', a2ui: 'barChart' } as const
const CELL_KIND = { sql: 'Query cell', markdown: 'Text cell', a2ui: 'Card cell' } as const

function titleOf(cell: CellState): string {
  return cellTitle(cell) || CELL_FALLBACK_TITLE[cell.type]
}

// ariaTitleOf caps titleOf at 60 chars for the minimize chevron's
// aria-label — an unlabelled sql cell's title falls back to its first SQL
// line, which can run well past what a screen reader should read as a
// button name.
function ariaTitleOf(cell: CellState): string {
  const t = titleOf(cell)
  return t.length > 60 ? t.slice(0, 60).trimEnd() + '…' : t
}

// summaryOf computes the "Show SQL · N lines · first line" text once per
// cell per render, rather than re-parsing cell.source three times in the
// template.
function summaryOf(cell: CellState): string {
  const s = sqlSummary(cell.source)
  return `Show SQL · ${s.lines} line${s.lines === 1 ? '' : 's'}${s.firstLine ? ' · ' + s.firstLine : ''}`
}

// --- save ---

// buildSavePayload takes id/title/cellsSnapshot as EXPLICIT arguments,
// never reads live refs itself — every caller below passes in values it
// captured at the moment that mattered (load time for the id, "right now"
// for title/cells), so a payload already under construction can never pick
// up a mutation (a route change, a teardown) that happens mid-build.
function buildSavePayload(id: string, snapshotTitle: string, snapshotCells: CellState[]) {
  return {
    id,
    title: snapshotTitle,
    cells: snapshotCells.map((c) => {
      if (c.type === 'sql') return { id: c.id, type: 'sql', collapsed: c.collapsed, source: c.source, view: c.view, viewPinned: c.viewPinned, label: c.label }
      if (c.type === 'markdown') return { id: c.id, type: 'markdown', collapsed: c.collapsed, source: c.source }
      return { id: c.id, type: 'a2ui', collapsed: c.collapsed, page: c.page }
    }),
  }
}

// captureSavePayload snapshots the CURRENT title/cells under the id this
// component actually loaded (never the live route param — see
// loadedNotebookId's doc comment). Returns null if nothing has loaded yet.
function captureSavePayload(): ReturnType<typeof buildSavePayload> | null {
  if (!loadedNotebookId) return null
  return buildSavePayload(loadedNotebookId, title.value, cells.value)
}

async function saveNow() {
  const payload = captureSavePayload()
  if (!payload) return
  const seq = ++saveSeq
  saveState.value = 'saving'
  try {
    const result = await window.go.main.App.SaveNotebook(payload, loadedSessionId)
    if (result?.warnings?.length) {
      errorMessage.value = `Saved with ${result.warnings.length} warning(s): ${result.warnings[0]}`
    }
    saveState.value = 'saved'
    tombstones = pruneConfirmedTombstones(tombstones, new Set(payload.cells.map((c) => c.id)))
    if (seq === saveSeq) lastPersisted = JSON.stringify(payload)
  } catch (e: any) {
    errorMessage.value = 'Failed to save notebook: ' + errorText(e)
    saveState.value = 'idle'
  }
}

// doFlush cancels any pending debounce and saves NOW, using a payload
// captured before whatever prompted the flush (a route change, an unmount,
// a file close) can tear anything down — review fix, step 4a #2/#3. If
// `payload` is not supplied it is captured fresh (the common case: leaving
// the route, or unmounting with no pending navigation already holding one).
//
// It sets the toast AND rethrows on failure — review fix, step 4a round 3
// #2: the copy registered with usePendingSaves (see onMounted below) is
// called from AppShell.closeFile/UnlockView.openFile via flushAll(), and
// they need to know a save actually failed (not just see a toast that is
// about to unmount along with this view) so they can ask the person before
// closing/switching files and losing the edit. Every call site elsewhere in
// this component that does NOT need to propagate the failure goes through
// flushSave below instead, which never throws.
async function doFlush(payload?: ReturnType<typeof buildSavePayload> | null) {
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  const p = payload ?? captureSavePayload()
  if (!p) return
  // #95 carry-over: nothing has changed since the last load/save, so
  // there's nothing local to write — skip the save rather than overwriting
  // a newer save that may have landed from elsewhere (an agent append, a
  // concurrent MCP write) with this stale copy.
  const serialized = JSON.stringify(p)
  if (serialized === lastPersisted) return
  const seq = ++saveSeq
  try {
    await window.go.main.App.SaveNotebook(p, loadedSessionId)
    tombstones = pruneConfirmedTombstones(tombstones, new Set(p.cells.map((c) => c.id)))
    if (seq === saveSeq) lastPersisted = serialized
  } catch (e: any) {
    const message = 'Failed to save notebook: ' + errorText(e)
    errorMessage.value = message
    throw new Error(message)
  }
}

// flushSave is the best-effort variant used by every INTERNAL call site in
// this component (route-switch, route-leave, unmount, file-closed) — none
// of them have anywhere useful to propagate a failure to, so they rely on
// doFlush's own toast rather than an uncaught rejection. Only the copy
// registered with usePendingSaves calls doFlush directly (see onMounted).
async function flushSave(payload?: ReturnType<typeof buildSavePayload> | null) {
  try {
    await doFlush(payload)
  } catch {
    // doFlush already surfaced a toast; nothing further to do here.
  }
}

function scheduleSave() {
  if (suppressAutosave) return
  saveState.value = 'idle'
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(saveNow, 800)
}

watch(statusMessage, (msg) => {
  if (msg) setTimeout(() => { statusMessage.value = '' }, 4000)
})
watch(title, scheduleSave)
watch(() => cells.value.map((c) => `${c.id}:${c.type}:${c.source}:${c.view}:${c.viewPinned}:${c.label}:${c.collapsed}`).join('|'), scheduleSave)
watch(() => cells.value.length, scheduleSave)

// Review fix, step 4a #3: vue-router reuses this component instance across
// a /notebooks/:id -> /notebooks/:id2 navigation, so switching notebooks
// must flush THIS notebook's edits (captured under its own id, before
// anything changes), tear down its editors/observer/in-flight queries, and
// only THEN load the new one — in that order, or a debounced save still in
// flight when cells.value gets replaced would build its payload from the
// new notebook's cells under the old id (or vice versa).
watch(
  () => route.params.id,
  async (newId) => {
    const id = String(newId)
    if (!id || id === loadedNotebookId) return
    const payload = captureSavePayload()
    for (const cell of cells.value) cell.editor?.destroy()
    cellObserver?.disconnect()
    loadAbort?.abort()
    // A running/proposed AI-cell task belongs to the notebook it was
    // opened against — switching notebooks mid-task must never let that
    // task's eventual Apply land in the notebook we're navigating to.
    aiCellDialog.close()
    await flushSave(payload)
    await loadNotebook(id)
  },
)

function backToList() {
  router.push('/notebooks')
}

async function exportNotebook() {
  if (!loadedNotebookId) return
  try {
    const path = await window.go.main.App.ExportNotebook(loadedNotebookId, '')
    if (path) statusMessage.value = `Exported to ${path}`
  } catch (e: any) {
    errorMessage.value = 'Failed to export notebook: ' + errorText(e)
  }
}

// handleNotebooksChanged: an agent tool (add_query_cell/create_dashboard,
// #67 deferred part), possibly running against an MCP client with no
// Studio window watching it, appended a cell to a notebook and the backend
// emitted "notebooks:changed". If that notebook is the one open here, merge
// in ONLY the cells this view doesn't already have (by id) — appended at
// the end, in the order the backend returned them — and touch nothing else:
// not title, not any existing cell's source/view/label. A user mid-edit on
// cell 3 when this fires must see their unsaved keystrokes in cell 3
// untouched, with the agent's new cell simply showing up after it. Reloading
// the WHOLE notebook (the naive approach) would silently discard whatever
// the user was typing the instant this event arrived — that's the race this
// function exists to avoid. Skipped entirely while this view's own save is
// what's about to produce the next "notebooks:changed" (the common case: our
// own autosave triggers this event too) by only ever ADDING cells we don't
// have, never removing/reordering ones we do — replaying our own save is
// therefore a safe no-op here.
interface NotebooksChangedPayload {
  notebookId: string
  appendedCellIds?: string[]
}

// handleNotebooksChanged merges in ONLY the cells the event names in
// appendedCellIds (review fix, finding 2) — never "any remote id we don't
// already have". An event with no appendedCellIds (our own SaveNotebook
// echo, CreateNotebook/RenameNotebook/DeleteNotebook/ImportNotebook — none
// of which append a cell to THIS notebook) is a fast no-op via
// cellsToMerge's own guard, so it's still safe to call this unconditionally.
// Tombstoned ids (deleted locally, not yet confirmed excluded by a save —
// see the `tombstones` doc comment) are never merged back in even if a
// racing agent append's read of the notebook still had them.
// pendingNotebooksChangedQueue holds append events that arrived while
// loading.value was true (coordinator follow-up #3) — an agent's
// add_query_cell/create_dashboard can race a notebook load/switch, and the
// event used to be silently dropped in that window instead of merged once
// the load settled. Cleared at the START of every loadNotebook call: a
// fresh load already reflects current server state, so anything queued for
// whatever was loading/loaded before is moot.
let pendingNotebooksChangedQueue: NotebooksChangedPayload[] = []

async function handleNotebooksChanged(payload?: NotebooksChangedPayload) {
  if (!payload || !loadedNotebookId || payload.notebookId !== loadedNotebookId) return
  if (loading.value) {
    pendingNotebooksChangedQueue.push(payload)
    return
  }
  await applyNotebooksChanged(payload)
}

// replayPendingNotebooksChanged runs once loadNotebook(id) has finished
// (cells populated, editors mounted) — applies, in order, whatever
// append events queued while id was loading. Bails immediately if another
// load has since superseded this one (loadedNotebookId no longer id),
// including mid-replay, since applyNotebooksChanged itself awaits a fetch.
async function replayPendingNotebooksChanged(id: string) {
  if (loadedNotebookId !== id) return
  const queued = pendingNotebooksChangedQueue.filter((p) => p.notebookId === id)
  pendingNotebooksChangedQueue = []
  for (const p of queued) {
    if (loadedNotebookId !== id) return
    await applyNotebooksChanged(p)
  }
}

// applyNotebooksChanged merges in ONLY the cells the event names in
// appendedCellIds (review fix, finding 2) — never "any remote id we don't
// already have". An event with no appendedCellIds (our own SaveNotebook
// echo, CreateNotebook/RenameNotebook/DeleteNotebook/ImportNotebook — none
// of which append a cell to THIS notebook) is a fast no-op. Tombstoned ids
// (deleted locally, not yet confirmed excluded by a save — see the
// `tombstones` doc comment) are never merged back in even if a racing
// agent append's read of the notebook still had them.
async function applyNotebooksChanged(payload: NotebooksChangedPayload) {
  if (!payload.appendedCellIds || payload.appendedCellIds.length === 0) return

  const requestedId = loadedNotebookId
  let remote: { cells?: RawCell[] } | null = null
  try {
    remote = await window.go.main.App.GetNotebook(requestedId)
  } catch {
    return // notebook deleted, or a transient error — nothing safe to merge
  }
  // The route may have switched to a different notebook while this fetch
  // was in flight — discard rather than merging a stale notebook's cells
  // into whatever is now loaded.
  if (!remote || loadedNotebookId !== requestedId) return

  const incoming = cellsToMerge(cells.value, remote.cells || [], payload.appendedCellIds, tombstones)
  if (incoming.length === 0) return
  for (const raw of incoming) cells.value.push(newCellState(raw as RawCell))
  await nextTick()
  mountEditors()
  setupCellObserver()
  for (const raw of incoming) {
    const cell = cells.value.find((c) => c.id === raw.id)
    if (cell && cell.type === 'sql' && cell.source.trim()) runCell(cell)
  }
}

function handleFileClosed() {
  // Now largely redundant with AppShell's closeFile() awaiting flushAll()
  // BEFORE calling CloseFile (review fix, step 4a round 2 #1) — the happy
  // path flush already succeeded before the container closed. Kept as a
  // defensive no-op-in-practice fallback for a close that reaches this view
  // some other way; loadedNotebookId is cleared so nothing here keeps
  // trying to save into a file that may already be gone.
  flushSave()
  loadedNotebookId = ''
  loadedSessionId = ''
  lastPersisted = ''
}

// handleFileReverted (#98) reloads the notebook from the reverted container
// so its SQL cells re-run against the reverted data, matching what happens
// on open. A pending debounced save is flushed first (best-effort, same as
// every other internal call site) so a hand edit made just before the
// revert isn't silently lost by the reload that follows. A running/proposed
// AI-cell task belongs to the pre-revert data (its previews ran against
// it), so it's closed the same way the notebook-switch watcher closes one.
async function handleFileReverted() {
  await flushSave()
  // loadNotebook replaces cells.value wholesale but — unlike the
  // route.params.id watcher's own reload, which is the only other caller —
  // doesn't tear down the CURRENT cells' CodeMirror editors first, since it
  // is normally only ever called once per mount. Do that here, matching the
  // watcher's teardown, so reloading the SAME notebook after a revert
  // doesn't leak the old editor instances. loadNotebook itself disconnects
  // cellObserver first thing, so that teardown isn't repeated here.
  for (const cell of cells.value) cell.editor?.destroy()
  aiCellDialog.close()
  await loadNotebook(String(route.params.id))
}

onBeforeRouteLeave(async () => {
  await flushSave()
})

// unsubFileClosed holds the unsubscribe function EventsOn returns — review
// fix, step 4a round 2 #2: calling the global EventsOff('file:closed')
// removes EVERY listener for that event name, including App.vue's own,
// which is what actually swaps the view back to UnlockView. Storing and
// calling the specific unsubscribe function this component was given
// removes only this listener.
let unsubFileClosed: (() => void) | null = null
let unsubFileReverted: (() => void) | null = null
let unsubNotebooksChanged: (() => void) | null = null
let unregisterPendingSave: (() => void) | null = null

onMounted(() => {
  loadNotebook(String(route.params.id))
  if (hasWailsRuntime()) unsubFileClosed = EventsOn('file:closed', handleFileClosed)
  if (hasWailsRuntime()) unsubFileReverted = EventsOn('file:reverted', handleFileReverted)
  if (hasWailsRuntime()) unsubNotebooksChanged = EventsOn('notebooks:changed', handleNotebooksChanged)
  // Review fix, step 4a round 2 #1 / round 3 #2: register this view's flush
  // with the shared registry so AppShell's close path (and the app-quit
  // flow) can await it before the container actually closes — using
  // doFlush directly (not flushSave) so a real failure propagates instead
  // of being swallowed. The label is evaluated lazily (only if this flush
  // actually fails), reading the live title rather than whatever it was
  // when the notebook first loaded.
  unregisterPendingSave = registerPendingSave(() => title.value || 'Untitled notebook', () => doFlush())
})

onBeforeUnmount(() => {
  // Capture BEFORE any teardown below touches editor/observer/abort state —
  // review fix, step 4a #2 ("using a payload captured before teardown").
  const payload = captureSavePayload()
  if (saveTimer) clearTimeout(saveTimer)
  for (const cell of cells.value) cell.editor?.destroy()
  cellObserver?.disconnect()
  loadAbort?.abort()
  unsubFileClosed?.()
  unsubFileReverted?.()
  unsubNotebooksChanged?.()
  unregisterPendingSave?.()
  aiCellDialog.close()
  if (hasWailsRuntime()) window.go.main.App.SetActiveNotebook('')
  flushSave(payload)
})

// Exposed for component tests (NotebookView.spec.ts) to drive cell state
// directly rather than reach through the CodeMirror mock for every
// interaction — the same pattern other Vibe views' specs use.
defineExpose({
  cells, runCell, moveCell, deleteCell, addCell, title,
  flushSave, exportNotebook,
  toggleSource, toggleCollapsed, insertCellFromEnvelope, replaceCellFromEnvelope,
  getLoadedNotebookId: () => loadedNotebookId,
  getLoadedSessionId: () => loadedSessionId,
  // Exposed for NotebookView.spec.ts's queue/replay coverage (coordinator
  // follow-up #3) — driving the real "notebooks:changed" Wails event would
  // require a live hasWailsRuntime()/EventsOn setup these tests don't run
  // under; calling handleNotebooksChanged directly exercises the exact same
  // queue-while-loading / replay-after-load logic loadNotebook itself uses.
  handleNotebooksChanged,
  handleFileReverted,
  isLoading: () => loading.value,
})
</script>

<template>
  <div class="col" style="height: 100%; gap: 0;">
    <div class="row" style="justify-content: space-between; align-items: center; padding: var(--sh-space-3) var(--sh-space-6); border-bottom: var(--sh-border);">
      <div class="row" style="gap: var(--sh-space-3); align-items: center; min-width: 0;">
        <button @click="backToList" class="btn btn-ghost btn-sm" aria-label="Back to notebooks" title="Back to notebooks"><Icon name="arrowLeft" :size="14" /></button>
        <input
          v-model="title"
          class="field-input"
          data-testid="notebook-title"
          style="font-weight: 700; font-size: var(--sh-fs-lg); border: none; background: transparent; min-width: 240px;"
          placeholder="Untitled notebook"
        />
        <span class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite);" data-testid="save-indicator">
          <template v-if="saveState === 'saving'">Saving…</template>
          <template v-else-if="saveState === 'saved'">Saved</template>
        </span>
      </div>
      <div class="row" style="gap: var(--sh-space-2);">
        <button @click="exportNotebook" class="btn btn-ghost btn-sm" data-testid="export-notebook"><Icon name="download" :size="14" /> Export</button>
        <button @click="runAll" class="btn btn-ghost btn-sm"><Icon name="play" :size="14" /> Run all</button>
      </div>
    </div>

    <div style="flex: 1; overflow: auto; padding: var(--sh-space-6);">
      <div v-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>
      <div v-else-if="notFound" class="sh-mono" style="color: var(--sh-error);">Notebook not found.</div>

      <div v-else class="col" style="gap: var(--sh-space-4); max-width: 920px; margin: 0 auto;">
        <div v-if="cells.length === 0" class="col" style="align-items: center; gap: var(--sh-space-3); margin-top: var(--sh-space-8); text-align: center;">
          <Icon name="layers" :size="28" style="color: var(--sh-graphite);" />
          <div class="sh-mono" style="color: var(--sh-graphite);">This notebook is empty. Add a cell to get started.</div>
        </div>

        <div
          v-for="cell in cells"
          :key="cell.id"
          :ref="(el) => observeCell(cell, el as Element | null)"
          class="card"
          :data-testid="`cell-${cell.type}`"
          :data-cell-id="cell.id"
          style="overflow: hidden;"
        >
          <!-- Cell toolbar -->
          <div class="row" style="justify-content: space-between; align-items: center; padding: var(--sh-space-2) var(--sh-space-3); background: var(--sh-paper-2); border-bottom: var(--sh-border-thin); flex-wrap: wrap; gap: var(--sh-space-2);">
            <div class="row" style="flex: 1 1 0; gap: var(--sh-space-2); align-items: center; min-width: 0; flex-wrap: nowrap; overflow: hidden;">
              <button
                @click="toggleCollapsed(cell)"
                class="btn btn-ghost btn-sm"
                style="padding: 2px; flex: none;"
                :aria-label="`${cell.collapsed ? 'Expand' : 'Minimize'} ${ariaTitleOf(cell)}`"
                :aria-expanded="!cell.collapsed"
                data-testid="cell-minimize"
              >
                <Icon :name="cell.collapsed ? 'chevronRight' : 'chevronDown'" :size="14" />
              </button>
              <Icon :name="CELL_ICON[cell.type]" :size="14" role="img" :aria-label="CELL_KIND[cell.type]" :title="CELL_KIND[cell.type]" style="flex: none;" />
              <template v-if="cell.collapsed">
                <span class="sh-mono" style="min-width: 0; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer;" data-testid="cell-min-title" @click="toggleCollapsed(cell)">{{ titleOf(cell) }}</span>
                <span v-if="cellMeta(cell)" class="sh-mono" data-testid="cell-min-meta" style="color: var(--sh-graphite); font-size: var(--sh-fs-xs); white-space: nowrap; flex: none;">{{ cellMeta(cell) }}</span>
              </template>
              <template v-else>
                <input
                  v-if="cell.type === 'sql'"
                  v-model="cell.label"
                  placeholder="label"
                  class="sh-mono"
                  title="Label"
                  style="border: none; background: transparent; font-size: var(--sh-fs-xs); color: var(--sh-graphite); flex: 1 1 110px; min-width: 40px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;"
                />
                <span
                  v-if="isStale(cell)"
                  class="badge"
                  style="background: var(--vibe-highlight); color: var(--vibe-highlight-fg); white-space: nowrap; flex: none;"
                  data-testid="stale-badge"
                >EDITED · RUN TO UPDATE</span>
              </template>
            </div>
            <div class="row" style="gap: var(--sh-space-1); flex: none;">
              <template v-if="!cell.collapsed">
                <template v-if="cell.type === 'sql'">
                  <button
                    v-for="v in VIEWS"
                    :key="v"
                    @click="pickView(cell, v)"
                    class="btn btn-ghost btn-sm"
                    :class="{ 'row-selected': cell.view === v }"
                    :style="shapeOf(cell) && !viewFit(v, shapeOf(cell)!).ok ? { opacity: 0.45 } : {}"
                    :title="shapeOf(cell) && !viewFit(v, shapeOf(cell)!).ok ? viewFit(v, shapeOf(cell)!).why : VIEW_HINTS[v]"
                    :data-testid="`view-${v}`"
                  >{{ VIEW_LABELS[v] }}</button>
                  <button @click="runCell(cell)" :disabled="cell.running" class="btn btn-primary btn-sm" data-testid="run-cell">
                    <Icon name="play" :size="12" /> {{ cell.running ? 'Running…' : 'Run' }}
                  </button>
                </template>
                <template v-else-if="cell.type === 'markdown'">
                  <button @click="toggleMdPreview(cell)" class="btn btn-ghost btn-sm">{{ cell.mdPreview ? 'Edit' : 'Preview' }}</button>
                </template>
                <button
                  v-if="AI_CELL_ENABLED && (cell.type === 'sql' || cell.type === 'markdown')"
                  @click="openAiCellEdit(cell)"
                  class="btn btn-ghost btn-sm"
                  title="Edit with AI"
                  aria-label="Edit with AI"
                  :data-testid="`ai-cell-edit-${cell.id}`"
                ><Icon name="sparkle" :size="14" /></button>
              </template>
              <button @click="moveCell(cell, -1)" class="btn btn-ghost btn-sm" title="Move up" aria-label="Move cell up"><Icon name="chevronUp" :size="14" /></button>
              <button @click="moveCell(cell, 1)" class="btn btn-ghost btn-sm" title="Move down" aria-label="Move cell down"><Icon name="chevronDown" :size="14" /></button>
              <button @click="deleteCell(cell)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);" title="Delete cell" aria-label="Delete cell"><Icon name="trash" :size="14" /></button>
            </div>
          </div>

          <div v-show="!cell.collapsed">
            <!-- SQL cell -->
            <template v-if="cell.type === 'sql'">
              <CellDrawer :open="cell.sourceOpen" testid="sql-drawer" @toggle="toggleSource(cell)">
                <template #summary>
                  <template v-if="cell.sourceOpen">Hide SQL</template>
                  <template v-else>{{ summaryOf(cell) }}</template>
                </template>
                <div :ref="(el) => { cell.editorEl = el as HTMLElement }" style="min-height: 90px; border-top: var(--sh-border-thin); border-bottom: var(--sh-border-thin);"></div>
                <div
                  v-if="AI_CELL_ENABLED && !cell.source.trim()"
                  class="sh-mono"
                  style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); padding: var(--sh-space-1) var(--sh-space-3); display: flex; align-items: center; gap: 6px;"
                >
                  write SQL or
                  <button @click="openAiCellEdit(cell, 'Generate with AI')" class="btn btn-ghost btn-sm" style="padding: 2px 6px;" :data-testid="`ai-cell-generate-empty-${cell.id}`">
                    <Icon name="sparkle" :size="12" /> generate it with AI
                  </button>
                </div>
                <!-- Standing explanation of the selected view (review fix,
                     step 4a round 2 #6 — ported from notebook.js's
                     nb-cell__viewhint), now inside the drawer alongside the
                     editor it explains. -->
                <div class="sh-mono" data-testid="view-hint" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); padding: var(--sh-space-1) var(--sh-space-3);">
                  {{ VIEW_LABELS[cell.view] }} · {{ VIEW_HINTS[cell.view] }}<template v-if="!cell.viewPinned"> · auto</template>
                </div>
                <div class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); padding: 0 var(--sh-space-3) var(--sh-space-1);">
                  Notebook cells are read-only queries — use the Query editor to change data. (Ctrl/Cmd+Enter to run.)
                </div>
              </CellDrawer>
              <div style="padding: var(--sh-space-3);">
                <div v-if="cell.result?.error" class="sh-mono" style="color: var(--sh-error); font-size: var(--sh-fs-sm);" data-testid="cell-error">
                  {{ cell.result.error }}
                </div>
                <template v-else-if="cell.result">
                  <div v-if="mismatchInfo(cell)" class="sh-mono" style="font-size: var(--sh-fs-xs); margin-bottom: var(--sh-space-2);" data-testid="mismatch-note">
                    <div style="font-weight: 700; color: var(--sh-error);">{{ mismatchInfo(cell)!.label }} doesn't fit — wants {{ mismatchInfo(cell)!.hint }}</div>
                    <div style="color: var(--sh-graphite); margin-top: 2px;">{{ mismatchInfo(cell)!.shapeLine }}</div>
                    <div v-if="mismatchInfo(cell)!.fits.length" class="row" style="gap: var(--sh-space-1); align-items: center; margin-top: var(--sh-space-2);">
                      <span style="color: var(--sh-graphite);">SHOWS AS</span>
                      <button
                        v-for="v in mismatchInfo(cell)!.fits"
                        :key="v"
                        @click="pickView(cell, v)"
                        class="btn btn-ghost btn-sm"
                        :data-testid="`shows-as-${v}`"
                      >{{ VIEW_LABELS[v] }}</button>
                    </div>
                  </div>
                  <A2UIPage v-else-if="synthesizedPage(cell)" :page="synthesizedPage(cell)!" :signal="loadAbort?.signal" />
                </template>
                <div v-else class="sh-mono" style="color: var(--sh-graphite); font-size: var(--sh-fs-xs);">Not run yet.</div>
              </div>
            </template>

            <!-- Markdown cell -->
            <template v-else-if="cell.type === 'markdown'">
              <div v-if="cell.mdPreview" class="prose vibe-content vibe-md-content" style="padding: var(--sh-space-4);" v-html="renderSafeMarkdown(cell.source)"></div>
              <div v-else :ref="(el) => { cell.editorEl = el as HTMLElement }" style="min-height: 90px;"></div>
            </template>

            <!-- A2UI cell (lazy-mounted: review fix, step 4a #1 — the page,
                 and every live query its data-bound components fire, is not
                 rendered until this cell has scrolled into view) -->
            <template v-else-if="cell.type === 'a2ui'">
              <div style="padding: var(--sh-space-4); min-height: 48px;">
                <A2UIPage v-if="cell.page && cell.visible" :page="cell.page" :signal="loadAbort?.signal" />
                <div v-else class="sh-mono" style="color: var(--sh-graphite); font-size: var(--sh-fs-xs);">Scroll into view to render…</div>
              </div>
              <div style="border-top: var(--sh-border-thin);">
                <CellDrawer :open="cell.sourceOpen" testid="a2ui-drawer" @toggle="toggleSource(cell)">
                  <template #summary>Advanced: card definition</template>
                  <!-- Kept behind v-if (rather than always rendering) so the
                       view doesn't stringify every a2ui page on every
                       keystroke re-render — the cost is that closing this
                       one snaps instead of sliding, which goes away once
                       #88 makes the body an editor instead of a <pre>. -->
                  <pre v-if="cell.sourceOpen" class="sh-mono" style="font-size: var(--sh-fs-xs); padding: var(--sh-space-3); overflow: auto; background: var(--sh-paper-2); margin: 0;">{{ JSON.stringify(cell.page, null, 2) }}</pre>
                </CellDrawer>
              </div>
            </template>
          </div>
        </div>

        <!-- Add cell toolbar -->
        <div class="row" style="gap: var(--sh-space-2); justify-content: center; padding: var(--sh-space-4);">
          <button @click="addCell('sql')" class="btn btn-ghost btn-sm" data-testid="add-sql-cell"><Icon name="terminal" :size="14" /> SQL</button>
          <button @click="addCell('markdown')" class="btn btn-ghost btn-sm" data-testid="add-markdown-cell"><Icon name="fileText" :size="14" /> Markdown</button>
          <button @click="addCell('a2ui')" class="btn btn-ghost btn-sm" data-testid="add-a2ui-cell"><Icon name="barChart" :size="14" /> A2UI</button>
          <button v-if="AI_CELL_ENABLED" @click="openAiCellCreate" class="btn btn-ghost btn-sm" data-testid="ai-cell-open-create"><Icon name="sparkle" :size="14" /> Generate with AI</button>
          <button
            v-else
            class="btn btn-ghost btn-sm"
            disabled
            style="opacity: 0.4; cursor: not-allowed;"
            title="Agent cell authoring lands in a later step (#66 part B)"
          ><Icon name="sparkle" :size="14" /> Generate with AI</button>
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="error-toast" @click="errorMessage = ''">{{ errorMessage }}</div>
    <div v-if="infoMessage" class="info-toast" data-testid="ai-cell-info-toast" @click="infoMessage = ''">{{ infoMessage }}</div>
    <div v-if="statusMessage" class="status-toast" data-testid="notebook-status" @click="statusMessage = ''">{{ statusMessage }}</div>

    <AiCellDialog v-if="AI_CELL_ENABLED" :dialog="aiCellDialog" @applied="onAiCellApplied" />
  </div>
</template>
