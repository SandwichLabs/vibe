<script setup lang="ts">
// QueryEditorView — the Query Explorer (parity W5; mockup #8 hifi-power).
//
// Left rail: the saved-query list (the schema tree moved to the global
// Connections rail, #117). Right: the SQL editor over a result bar carrying run-status metadata
// (success badge, row count, duration, DuckDB engine label, last-run time)
// and the result grid. The subhead exposes Save, CSV/Parquet export (DuckDB
// COPY via ExportQueryResults), an Ask-agent jump (opens the W0 panel), and
// Run.
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ConnectionSelect from '../components/ConnectionSelect.vue'
import { connectionList, defaultConnectionName, refreshConnections, openPromptFor, startConnectionTracking } from '../composables/useConnections'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import ConflictBanner from '../components/ConflictBanner.vue'
import { useModalKeys, shortcutLabel } from '../composables/useModal'
import { errorText } from '../lib/errors'
import { formatCell, cellClass, isNumericType } from '../lib/formatCell'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { register as registerPendingSave } from '../composables/usePendingSaves'
import { hasWailsRuntime } from '../lib/wails'

const queries = ref<QuerySummary[]>([])
const selectedId = ref<string | null>(null)
// The store's revision of the selected query as loaded: sent back on save so a
// copy that changed on disk meanwhile is a conflict, not an overwrite.
const selectedRev = ref('')
const sqlText = ref('')
const savedSQL = ref('')
const result = ref<QueryResult | null>(null)
const loading = ref(true)
const running = ref(false)
const error = ref('')
const showSaveModal = ref(false)
const saveModalEl = ref<HTMLElement | null>(null)
useModalKeys(showSaveModal, () => { showSaveModal.value = false }, saveModalEl)
const saveTitle = ref('')
const errorMessage = ref('')
const statusMessage = ref('')
const showConfirmDelete = ref(false)
const pendingDeleteId = ref<string | null>(null)
const hoveredId = ref<string | null>(null)

// The saved query's file changed on disk while there are unsaved edits (a git
// pull, another editor, the agent): ConflictBanner offers reload / keep mine /
// save as a copy. Typing is never blocked while it shows.
const conflict = ref<{ deleted: boolean } | null>(null)
const conflictBusy = ref(false)
// The revision on disk when the banner was raised: "Keep mine" only overwrites that one.
let conflictRev = ''
function raiseConflict(deleted: boolean) {
  conflict.value = { deleted }
  conflictRev = ''
  const id = selectedId.value
  if (deleted || !id) return
  window.go.main.App.GetQuery(id).then((q: any) => { if (conflict.value && selectedId.value === id) conflictRev = q?.rev ?? '' }).catch(() => { /* falls back to an unconditional keep */ })
}
const isConflict = (e: unknown) => errorText(e).startsWith('conflict:')

// Run-status metadata (W5): success/error badge + wall-clock of the last run.
const lastRunAt = ref('')
const lastRunOk = ref<boolean | null>(null)

// The "Run on" choice of THIS editor (SPEC 08): per editor, saved with a saved
// query, never session-global. '' is the workspace default.
const route = useRoute()
const router = useRouter()
const conn = ref('')
const savedConn = ref('')
const tabId = Math.random().toString(36).slice(2, 8)

const dirty = computed(() => sqlText.value.trim() !== '' && (sqlText.value !== savedSQL.value || (selectedId.value !== null && conn.value !== savedConn.value)))
const currentTitle = computed(() => {
  const q = queries.value.find((x) => x.id === selectedId.value)
  return q?.title || 'untitled_query.sql'
})
// #91 P-F7: the grid only ever holds the first queryGridRowCap rows
// (RunQuery caps server-side); total_rows carries the true count so the
// cap bar can say what's missing.
const truncated = computed(() => !!result.value && result.value.total_rows > result.value.row_count)

// One formatCell call per cell: precomputed once here rather than called
// 3x per cell (class/title/text) from the template.
const formattedRows = computed(() => {
  const r = result.value
  if (!r) return []
  return r.rows.map((row) => row.map((cell, j) => formatCell(cell, r.columns[j]?.type)))
})

async function loadQueries() {
  loading.value = true
  try {
    queries.value = await window.go.main.App.ListQueries()
  } catch { /* ignore */ }
  loading.value = false
}

async function selectQuery(id: string) {
  selectedId.value = id
  try {
    const q = await window.go.main.App.GetQuery(id)
    selectedRev.value = q.rev ?? ''
    sqlText.value = q.sql
    savedSQL.value = q.sql
    conn.value = (q as { connection?: string }).connection ?? ''
    savedConn.value = conn.value
    saveTitle.value = q.title
  } catch (e: any) {
    error.value = errorText(e)
  }
}

async function runQuery() {
  if (!sqlText.value.trim()) return
  running.value = true
  error.value = ''
  result.value = null
  try {
    ranOn.value = ranOnText(sqlText.value)
    result.value = await window.go.main.App.RunQuery({ sql: sqlText.value, connection: conn.value, tabId })
    lastRunOk.value = true
  } catch (e: any) {
    error.value = errorText(e)
    lastRunOk.value = false
    // A connection the statement names may need a sign-in or an extension: say
    // so with the prompt, not only with an error line.
    await refreshConnections()
    const needy = connectionList.value.find((c) => ['needs-credentials', 'extension-missing'].includes(c.status) && (c.name === conn.value || mentions(sqlText.value, c.name)))
    if (needy) openPromptFor(needy)
  }
  lastRunAt.value = new Date().toLocaleTimeString()
  running.value = false
}

// mentions: the statement names a connection in qualifier position (name.).
function mentions(sql: string, name: string): boolean {
  return new RegExp(`(^|[^A-Za-z0-9_"])"?${name.replace(/[^A-Za-z0-9_]/g, '')}"?\\s*\\.`).test(sql)
}
// ranOnText says where the last run went: the chosen connection plus any other
// a qualified name pulled in ("DuckDB · ran across local + prod_pg").
const ranOn = ref('')
function ranOnText(sql: string): string {
  const base = conn.value || defaultConnectionName.value
  const also = connectionList.value.filter((c) => c.name !== base && mentions(sql, c.name)).map((c) => c.name)
  return also.length ? `ran across ${[base, ...also].join(' + ')}` : `ran on ${base}`
}

async function saveQuery() {
  if (!saveTitle.value.trim()) return
  try {
    const saved = await window.go.main.App.SaveQueryOn({ id: selectedId.value || '', title: saveTitle.value, sql: sqlText.value, description: '', rev: selectedRev.value, connection: conn.value })
    savedConn.value = conn.value
    selectedId.value = saved.id
    // The save changed the file: its new revision goes with the next save.
    selectedRev.value = saved.rev ?? ''
    savedSQL.value = sqlText.value
    showSaveModal.value = false
    statusMessage.value = `Saved "${saveTitle.value}"`
    conflict.value = null
    await loadQueries()
  } catch (e: any) {
    if (isConflict(e)) {
      showSaveModal.value = false
      raiseConflict(false)
    } else {
      errorMessage.value = 'Save failed: ' + errorText(e)
    }
  }
}

async function resolveConflict(action: 'reload' | 'overwrite' | 'saveCopy') {
  const id = selectedId.value
  if (!id) { conflict.value = null; return }
  if (action === 'reload' && conflict.value?.deleted) {
    conflict.value = null
    newQuery()
    await loadQueries()
    return
  }
  conflictBusy.value = true
  try {
    const res = await window.go.main.App.ResolveConflict({
      kind: 'query', id, action,
      data: { title: saveTitle.value || currentTitle.value.replace('.sql', ''), sql: sqlText.value, description: '' },
      rev: conflictRev,
      connection: conn.value,
    })
    conflict.value = null
    if (action === 'overwrite') {
      selectedRev.value = res.rev
      savedSQL.value = sqlText.value
      statusMessage.value = 'Kept your version.'
    } else {
      await selectQuery(id)
      if (action === 'saveCopy') statusMessage.value = `Saved your version as "${res.title}".`
    }
    await loadQueries()
  } catch (e: any) {
    if (isConflict(e)) {
      raiseConflict(false)
      errorMessage.value = 'The file changed on disk again, so nothing was overwritten. Choose again.'
    } else {
      errorMessage.value = 'Failed: ' + errorText(e)
    }
  } finally {
    conflictBusy.value = false
  }
}

// flushPending is registered with usePendingSaves so switching workspaces,
// closing and quitting save an edited saved query first. A conflict, or a
// scratch buffer that was never saved, makes it fail so the caller can ask.
async function flushPending() {
  if (conflict.value) throw new Error('this query changed on disk and your edit is not saved yet')
  if (!dirty.value) return
  const id = selectedId.value
  if (!id) throw new Error('this query was never saved (use Save query)')
  try {
    const saved = await window.go.main.App.SaveQueryOn({ id, title: saveTitle.value || currentTitle.value.replace('.sql', ''), sql: sqlText.value, description: '', rev: selectedRev.value, connection: conn.value })
    selectedRev.value = saved.rev ?? ''
    savedSQL.value = sqlText.value
    savedConn.value = conn.value
  } catch (e: any) {
    if (isConflict(e)) raiseConflict(false)
    throw new Error(errorText(e))
  }
}
let unregisterPending: (() => void) | null = null

// onDiskChange: another program changed queries/. With nothing unsaved the
// editor quietly takes the new version; with unsaved edits the banner shows.
function onDiskChange(p: { kind: string; id: string; op: string }) {
  if (p?.kind !== 'query' || p.id !== selectedId.value) return
  if (p.op === 'deleted') {
    if (!dirty.value) { newQuery(); statusMessage.value = 'That query was deleted on disk.' } else raiseConflict(true)
    return
  }
  if (!dirty.value) {
    void selectQuery(p.id).then(() => { statusMessage.value = `"${currentTitle.value}" changed on disk and was reloaded.` })
    return
  }
  raiseConflict(false)
}

// exportResults re-runs the current SQL server-side through DuckDB COPY and
// writes CSV/Parquet to a user-picked path (empty dst = native save dialog).
async function exportResults(format: 'csv' | 'parquet') {
  if (!sqlText.value.trim()) return
  try {
    const path = await window.go.main.App.ExportQueryResults(sqlText.value, format, '')
    if (path) statusMessage.value = `Exported ${format.toUpperCase()} → ${path}`
  } catch (e: any) {
    errorMessage.value = 'Export failed: ' + errorText(e)
  }
}

// askAgent opens the W0 wizard panel (AppShell owns it; this event is the
// cross-component trigger) so the user can ask about the query they're writing.
function askAgent() {
  window.dispatchEvent(new CustomEvent('vibe:open-agent'))
}

function deleteQuery(id: string) {
  pendingDeleteId.value = id
  showConfirmDelete.value = true
}

async function confirmDeleteQuery() {
  const id = pendingDeleteId.value
  showConfirmDelete.value = false
  pendingDeleteId.value = null
  if (!id) return
  try {
    await window.go.main.App.DeleteQuery(id)
    if (selectedId.value === id) {
      selectedId.value = null
      sqlText.value = ''
      savedSQL.value = ''
    }
    await loadQueries()
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + errorText(e)
  }
}

function newQuery() {
  selectedId.value = null
  sqlText.value = ''
  conn.value = ''
  savedConn.value = ''
  savedSQL.value = ''
  result.value = null
  error.value = ''
  saveTitle.value = ''
  lastRunOk.value = null
  lastRunAt.value = ''
}

function handleKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault()
    runQuery()
  }
}

// Re-fetch saved queries (and schema row counts) when the data underneath
// changes — an agent's save_query tool call, another tab, etc. Mirrors
// TableBrowserView's identical listener (#91: an agent-saved query used to
// stay invisible in the rail until a manual reload).
async function onDataChanged() {
  await loadQueries()
}
let unsubDataChanged: (() => void) | null = null
let unsubDisk: (() => void) | null = null

onMounted(() => {
  unregisterPending = registerPendingSave(() => 'The query editor', flushPending)
  loadQueries()
  startConnectionTracking()
  applyRouteIntent()
  if (hasWailsRuntime()) {
    unsubDataChanged = EventsOn('data:changed', onDataChanged)
    unsubDisk = EventsOn('workspace:changed-on-disk', onDiskChange)
  }
})
onUnmounted(() => { unsubDataChanged?.(); unsubDisk?.(); unregisterPending?.() })

// "New query here" / "New query on this table" in the rail open a fresh editor on
// that connection: ?conn=prod_pg&sql=SELECT…&new=<stamp>.
function applyRouteIntent() {
  const c = route.query.conn
  if (typeof c !== 'string' || !route.query.new) return
  newQuery()
  conn.value = c
  const q = route.query.sql
  if (typeof q === 'string') sqlText.value = q
  void router.replace({ path: route.path })
}
watch(() => route.query.new, applyRouteIntent)

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 4000)
})
watch(statusMessage, (msg) => {
  if (msg) setTimeout(() => { statusMessage.value = '' }, 4000)
})
</script>

<template>
  <div class="col" style="height: 100%; gap: 0;">
    <!-- Subhead: title, dirty badge, save/export/agent/run actions -->
    <div class="page-subhead" style="padding: 12px 24px;">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Query Explorer</div>
        <div class="row" style="gap: 10px; margin-top: 4px;">
          <h1 style="font-size: 22px; margin: 0; font-family: var(--sh-font-mono);">{{ currentTitle }}</h1>
          <span v-if="dirty" class="badge badge--warn" data-testid="query-dirty">Unsaved</span>
        </div>
      </div>
      <div class="subhead-actions">
      <ConnectionSelect v-model="conn" />
      <button @click="saveTitle = saveTitle || currentTitle.replace('.sql',''), showSaveModal = true" class="btn btn-sm">
        <Icon name="save" :size="14" /> Save query
      </button>
      <button
        class="btn btn-sm"
        data-testid="export-csv"
        :disabled="!sqlText.trim()"
        @click="exportResults('csv')"
      ><Icon name="download" :size="14" /> CSV</button>
      <button
        class="btn btn-sm"
        data-testid="export-parquet"
        :disabled="!sqlText.trim()"
        @click="exportResults('parquet')"
      ><Icon name="download" :size="14" /> Parquet</button>
      <button class="btn btn-ghost btn-sm" data-testid="ask-agent" @click="askAgent">
        <Icon name="sparkle" :size="14" /> Ask agent
      </button>
      <button @click="runQuery" :disabled="running" class="btn btn-primary btn-sm" data-testid="run-query">
        <Icon name="play" :size="14" /> {{ running ? 'Running…' : `Run · ${shortcutLabel('Enter')}` }}
      </button>
      </div>
    </div>

    <ConflictBanner
      v-if="conflict"
      kind="query"
      :path="`queries/${selectedId}.sql`"
      :deleted="conflict.deleted"
      :busy="conflictBusy"
      @reload="resolveConflict('reload')"
      @overwrite="resolveConflict('overwrite')"
      @save-copy="resolveConflict('saveCopy')"
    />

    <div class="row" style="flex: 1; gap: 0; align-items: stretch; min-height: 0;">
      <!-- LEFT RAIL — saved queries (the schema tree is the global Connections rail) -->
      <div class="col rail">
        <div class="row" style="justify-content: space-between; align-items: center;">
          <div class="rail-label" style="margin: 0;">Saved queries</div>
          <button @click="newQuery" class="btn btn-ghost btn-sm" title="New query"><Icon name="plus" :size="14" /></button>
        </div>
        <div style="flex: 1; overflow: auto; min-height: 0;">
          <div v-if="loading" class="sh-mono rail-empty">Loading…</div>
          <div v-else-if="queries.length === 0" class="sh-mono rail-empty">No saved queries</div>
          <div
            v-for="q in queries"
            :key="q.id"
            data-testid="saved-query"
            @click="selectQuery(q.id)"
            @mouseenter="hoveredId = q.id"
            @mouseleave="hoveredId = null"
            class="row saved-row"
            :class="{ on: selectedId === q.id }"
          >
            <div style="min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600;">{{ q.title }}</div>
            <button :class="{ 'is-on': hoveredId === q.id }" @click.stop="deleteQuery(q.id)" class="btn btn-ghost btn-sm hover-reveal" style="color: var(--sh-error);" :aria-label="`Delete query ${q.title}`" :title="`Delete ${q.title}`"><Icon name="trash" :size="14" /></button>
          </div>
        </div>
      </div>

      <!-- EDITOR + RESULTS -->
      <div class="col" style="flex: 1; gap: 0; min-width: 0;">
        <div class="editor" style="border-bottom: var(--sh-border);">
          <textarea
            v-model="sqlText"
            @keydown="handleKeydown"
            style="width: 100%; height: 180px; resize: none; outline: none; background: var(--vibe-editor-bg); color: var(--vibe-editor-fg); border: 2px solid var(--vibe-border); font-family: var(--sh-font-mono); font-size: 13px; line-height: 1.8; padding: 18px;"
            placeholder="Enter SQL query…"
            spellcheck="false"
            data-testid="sql-input"
          ></textarea>
        </div>

        <!-- Run-status metadata bar (W5) -->
        <div v-if="lastRunOk !== null" class="result-bar" data-testid="run-meta">
          <span v-if="lastRunOk" class="badge badge--success" data-testid="run-badge"><span class="dot"></span> Complete</span>
          <span v-else class="badge badge--error" data-testid="run-badge"><span class="dot"></span> Error</span>
          <span v-if="result"><span class="big ok">{{ result.total_rows.toLocaleString() }}</span> rows</span>
          <span v-if="result"><span class="big">{{ result.duration }}</span></span>
          <span data-testid="run-engine"><Icon name="zap" :size="11" style="color: var(--sh-success); vertical-align: middle;" /> DuckDB<template v-if="ranOn"> · {{ ranOn }}</template></span>
          <span class="spacer"></span>
          <span data-testid="run-last">Last run · {{ lastRunAt }}</span>
        </div>

        <div style="flex: 1; overflow: auto; min-height: 0;">
          <div v-if="error" class="sql-error" style="padding: 16px; font-size: 13px; font-weight: 600; color: var(--sh-error);">{{ error }}</div>
          <div v-else-if="running" class="sh-mono" style="padding: 16px; color: var(--sh-graphite);">Running query…</div>
          <div v-else-if="result" style="overflow: auto; height: 100%;">
            <div v-if="truncated" class="result-bar" data-testid="query-row-cap">
              Showing the first {{ result.row_count.toLocaleString() }} of {{ result.total_rows.toLocaleString() }} rows. Export CSV or Parquet to get all of them.
            </div>
            <table class="table">
              <thead>
                <tr>
                  <th
                    v-for="col in result.columns"
                    :key="col.name"
                    :class="{ 'cell-num': isNumericType(col.type) }"
                  >{{ col.name }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, i) in formattedRows" :key="i">
                  <td
                    v-for="(fc, j) in row"
                    :key="j"
                    :class="cellClass(fc, result!.columns[j]?.type)"
                    :title="fc.title"
                  >{{ fc.text }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-else class="empty-hero">
            <div class="glyph"><Icon name="terminal" :size="48" /></div>
            <h2>Write a query, press Run</h2>
            <p class="lead">Queries run on the embedded DuckDB engine — analytics and CRUD alike.</p>
          </div>
        </div>
      </div>
    </div>

    <!-- Save modal -->
    <div v-if="showSaveModal" class="modal-overlay" @click.self="showSaveModal = false" role="dialog" aria-modal="true" aria-label="Save query">
      <div ref="saveModalEl" class="card card-pad" style="width: 100%; max-width: 384px;">
        <h3 class="card-title" style="margin-bottom: 16px;">Save query</h3>
        <input v-model="saveTitle" placeholder="Query name" aria-label="Query name" class="field-input" style="margin-bottom: 16px;" @keyup.enter="saveQuery" />
        <div class="row" style="gap: 12px; justify-content: flex-end;">
          <button @click="showSaveModal = false" class="btn btn-ghost">Cancel</button>
          <button @click="saveQuery" class="btn btn-primary">Save</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDelete"
      title="Delete query"
      confirm-label="Delete"
      message="Delete this query?"
      @confirm="confirmDeleteQuery"
      @cancel="showConfirmDelete = false; pendingDeleteId = null"
    />

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
    <div v-if="statusMessage" class="status-toast" data-testid="query-status">{{ statusMessage }}</div>
  </div>
</template>

<style scoped>
.rail {
  width: 240px;
  flex: none;
  gap: 8px;
  border-right: var(--sh-border);
  background: var(--sh-paper-2);
  padding: 14px 12px;
  min-height: 0;
  overflow-y: auto;
}
.rail-label {
  font-family: var(--sh-font-mono);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
}
.rail-empty {
  padding: 8px 10px;
  color: var(--sh-graphite);
  font-size: 12px;
}
/* The shipped .schema-tree .col rows collide with the global `.col`
   (flex-column) utility — force the row direction back. */
.schema-tree :deep(.col) {
  flex-direction: row;
  align-items: center;
}
.rail-divider {
  border-top: 1.5px dashed var(--vibe-border-subtle);
  margin: 10px 0 4px;
}
.saved-row {
  justify-content: space-between;
  padding: 6px 10px;
  cursor: pointer;
  border-radius: var(--sh-radius-sm);
  font-size: 13px;
}
.saved-row:hover { background: var(--sh-hover); }
.saved-row.on {
  background: var(--sh-surface);
  border: 1.5px solid var(--vibe-border);
  box-shadow: var(--sh-shadow-xs);
}
.sql-error {
  white-space: pre-wrap;
  font-family: var(--sh-font-mono);
  font-size: 13px;
}
</style>
