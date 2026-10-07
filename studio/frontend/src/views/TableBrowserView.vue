<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import { useModalKeys } from '../composables/useModal'
import { isReadOnly, workspaceInfo } from '../composables/useWorkspace'
import { connect } from '../composables/useConnections'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { errorText } from '../lib/errors'
import { formatCell, cellClass, cellEditText, isNumericType } from '../lib/formatCell'
import { hasWailsRuntime } from '../lib/wails'

// #82: opening /tables?table=orders (e.g. "Open table →" after an import)
// pre-selects that table once the table list has loaded.
const route = useRoute()
const router = useRouter()

// Autofocus + select directive for the inline cell editor. select() only exists
// on text inputs, so guard it for checkbox/date/select editors.
const vFocus = {
  mounted: (el: HTMLInputElement | HTMLSelectElement) => {
    el.focus()
    if (typeof (el as HTMLInputElement).select === 'function' &&
        (el as HTMLInputElement).type !== 'checkbox') {
      try { (el as HTMLInputElement).select() } catch { /* not selectable */ }
    }
  },
}

// Every table across the workspace's attached connections, as qualified names
// (connection.schema.table) the datasheet bindings accept, plus the groups the
// list renders (grouped by connection). A connection that is not connected yet is
// listed without tables and is never dialled from here.
// Opened read-only (another program owns the database): nothing here can write,
// and the controls say why instead of failing later.
const readOnlyReason = computed(() => `Opened read-only: ${workspaceInfo.value?.holder?.text || 'another program'} is using this workspace's database. Take over the workspace to make changes.`)
// #117: why the selected table can't be edited (a read-only connection, a view,
// a locked file), from GetTableCapabilities. Null means editable.
const caps = ref<TableCapabilities | null>(null)
const locked = computed(() => isReadOnly.value || (caps.value !== null && !caps.value.editable))
const lockText = computed(() => {
  if (caps.value && !caps.value.editable) return caps.value.text
  return readOnlyReason.value
})
const tables = ref<string[]>([])
// base table or view, for the Drop table button (a view is dropped elsewhere)
const selectedKind = computed(() => {
  for (const g of groups.value) for (const t of g.tables) if (t.qualified === selectedTable.value) return t.kind
  return 'table'
})
const groups = ref<CatalogGroup[]>([])
// Connections that could not be listed (locked by another program, an error); a
// plain "not connected yet" is not a problem.
const unavailable = computed(() => groups.value.filter((g) => g.status !== 'ok' && g.status !== 'not-connected'))
const selectedTable = ref<string | null>(null)
const tableInfo = ref<TableInfo | null>(null)
const tableData = ref<TableData | null>(null)
const loading = ref(true)
const dataLoading = ref(false)
const currentPage = ref(1)
const pageSize = ref(50)
const sortCol = ref('')
const sortDir = ref('ASC')
const showNewTableModal = ref(false)
const showAddRowModal = ref(false)
const newTableEl = ref<HTMLElement | null>(null)
const addRowEl = ref<HTMLElement | null>(null)
useModalKeys(showNewTableModal, () => { showNewTableModal.value = false }, newTableEl)
useModalKeys(showAddRowModal, () => { showAddRowModal.value = false }, addRowEl)
const newTableName = ref('')
const newTableCols = ref<ColumnInfo[]>([{ name: '', type: 'TEXT', not_null: false, pk: false }])
const newRowValues = ref<Record<string, string>>({})
const errorMessage = ref('')
const statusMessage = ref('')
const showConfirmDrop = ref(false)
const pendingDropTable = ref<string | null>(null)
const showConfirmDeleteRow = ref(false)
const pendingDeleteRow = ref<any[] | null>(null)

// Inline edit-in-place state. editing identifies the cell currently being
// edited by row index + column index; editValue holds the in-flight string.
const editing = ref<{ row: number; col: number } | null>(null)
const editValue = ref('')
const savingCell = ref(false)

// --- P2.9 datasheet visual querying ---
// activeFilter holds the currently applied structured FilterSpec (null = no
// filter). When set, loadData routes through GetTableDataFiltered so both the
// rows AND the record-count bar reflect the filtered total.
const activeFilter = ref<FilterSpec | null>(null)
// filteredTotal is the backend-reported total for the active filter; it drives
// the record-count bar (distinct from the unfiltered total_rows).
const filteredTotal = ref<number | null>(null)
// Filter-by-Form panel state: a list of editable condition rows.
const showFbF = ref(false)
const fbfCombinator = ref<'AND' | 'OR'>('AND')
const fbfConditions = ref<{ column: string; op: string; value: string }[]>([])
const FILTER_OPS = [
  { value: 'eq', label: '=' },
  { value: 'neq', label: '≠' },
  { value: 'lt', label: '<' },
  { value: 'gt', label: '>' },
  { value: 'lte', label: '≤' },
  { value: 'gte', label: '≥' },
  { value: 'like', label: 'like' },
  { value: 'is_null', label: 'is null' },
  { value: 'is_not_null', label: 'is not null' },
]

// Cell-level validation feedback: which cell failed and the message to show.
// This is independent of the global errorMessage toast so the user sees WHICH
// cell rejected without the change being persisted.
//
// Keyed by table + primary key + column name (#91 M-F16), not row/col index:
// a row index is only stable within one page of one table's current sort —
// switching tables, re-sorting, or paging used to leave a stale-looking red
// cell pointing at whatever now happened to sit at that index.
const cellError = ref<{ table: string; pk: string; col: string; message: string } | null>(null)

// isCellError reports whether the cell at (row, j) of the currently loaded
// page is the one cellError refers to.
function isCellError(row: any[], j: number): boolean {
  if (!cellError.value || !selectedTable.value || !tableData.value) return false
  const pk = pkColumn()
  if (!pk) return false
  const pkIdx = tableData.value.columns.indexOf(pk.name)
  if (pkIdx < 0) return false
  return cellError.value.table === selectedTable.value &&
    cellError.value.pk === String(row[pkIdx]) &&
    cellError.value.col === tableData.value.columns[j]
}

// editorType resolves the inline editor to use for a column from its
// declared SQL type (#68 dropped the Access-style field-meta control_type/
// combo-lookup layer). Returns one of: 'checkbox' | 'date' | 'text'.
//
// Only DATE gets the native date input. TIME/TIMESTAMP[TZ] used to get
// one too, which opens empty (a date input can't hold a time-of-day or a
// timestamp) and silently drops the time on save — they now get a plain
// text input seeded with cellEditText's DuckDB-faithful text
// ("13:45:00", "2025-04-06 13:45:00").
function editorType(colName: string): 'checkbox' | 'date' | 'text' {
  const col = tableInfo.value?.columns.find(c => c.name === colName)
  const t = (col?.type || '').toUpperCase()
  if (t.includes('BOOL')) return 'checkbox'
  if (t === 'DATE') return 'date'
  return 'text'
}

// colType returns the DuckDB type name for a column of the currently loaded
// table's data (ScanRows-sourced, e.g. "DECIMAL(18,4)"), for formatCell/
// cellEditText — distinct from editorType's DDL-sourced tableInfo type,
// which drives which input control to show.
function colType(colIdx: number): string | undefined {
  return tableData.value?.types?.[colIdx]
}

// One formatCell call per cell: computed once here and indexed
// (formattedRows[i][j]) from the template, rather than called repeatedly
// (class/title/display text) per cell per render. The
// template still reads the raw cell value directly where it needs it
// (checkbox checked-state, filter-by-selection's exact-match value,
// startEdit's editor seed) — formatCell's job is display only.
const formattedRows = computed(() => {
  const td = tableData.value
  if (!td) return []
  return td.rows.map((row) => row.map((cell, j) => formatCell(cell, colType(j))))
})

async function loadTables() {
  loading.value = true
  try {
    groups.value = (await window.go.main.App.ListCatalogGroups()) || []
    // A deep link (/tables?table=prod_pg.public.orders) to a connection that has
    // not been used yet connects it first (the rail does the same on expand).
    const want = route.query.table
    const cn = typeof want === 'string' && want.includes('.') ? want.split('.')[0] : ''
    if (cn && groups.value.some((g) => g.connection === cn && g.status === 'not-connected')) {
      await connect(cn)
      groups.value = (await window.go.main.App.ListCatalogGroups()) || []
    }
    tables.value = groups.value.flatMap((g) => g.tables.map((t) => t.qualified))
  } catch { /* ignore */ }
  loading.value = false
  applyRouteTable()
}

// applyRouteTable selects the table named by ?table= (once it's in the
// loaded list), without disturbing a selection the user already made.
function applyRouteTable() {
  const t = route.query.table
  if (typeof t !== 'string' || !t) return
  if (t === selectedTable.value) return
  // A bare name (e.g. "Open table →" after an import) means the default
  // connection's table of that name.
  const qualified = tables.value.includes(t)
    ? t
    : groups.value.find((g) => g.default)?.tables.find((x) => x.name === t)?.qualified
  if (qualified && qualified !== selectedTable.value) selectTable(qualified)
}

watch(() => route.query.table, applyRouteTable)

// Re-fetch the table list (and the currently open table's rows, if any) when
// the data underneath changes — an agent-driven create_table/create_record/
// import, another tab, etc. Previously this view only ever loaded once on
// mount, so a table the agent just created stayed invisible until a manual
// reload (review fix #6, mirroring DashboardView's identical listener).
async function onDataChanged() {
  await loadTables()
  if (selectedTable.value) {
    if (!tables.value.includes(selectedTable.value)) {
      // The table the agent dropped/renamed out from under us.
      selectedTable.value = null
      tableInfo.value = null
      tableData.value = null
      return
    }
    await loadData()
  }
}

async function selectTable(name: string) {
  selectedTable.value = name
  currentPage.value = 1
  sortCol.value = ''
  sortDir.value = 'ASC'
  cellError.value = null
  clearFilter()
  try {
    tableInfo.value = await window.go.main.App.DescribeTable(name)
  } catch { tableInfo.value = null }
  try {
    caps.value = await window.go.main.App.GetTableCapabilities(name)
  } catch { caps.value = null }
  await loadData()
}

async function loadData() {
  if (!selectedTable.value) return
  dataLoading.value = true
  try {
    if (activeFilter.value) {
      tableData.value = await window.go.main.App.GetTableDataFiltered(
        selectedTable.value, activeFilter.value, currentPage.value, pageSize.value, sortCol.value, sortDir.value
      )
      filteredTotal.value = tableData.value?.total_rows ?? null
    } else {
      tableData.value = await window.go.main.App.GetTableData(
        selectedTable.value, currentPage.value, pageSize.value, sortCol.value, sortDir.value
      )
    }
  } catch { tableData.value = null }
  dataLoading.value = false
}

// applyFilter sets the active FilterSpec and reloads from page 1 so the rows
// and the record-count bar both reflect the filtered result.
async function applyFilter(spec: FilterSpec) {
  activeFilter.value = spec
  currentPage.value = 1
  await loadData()
}

// clearFilter removes any active filter and reverts to the unfiltered view.
function clearFilter() {
  activeFilter.value = null
  filteredTotal.value = null
  showFbF.value = false
  fbfConditions.value = []
}

// filterBySelection applies a single eq condition on the clicked cell's column
// and value (Access Filter-by-Selection).
async function filterBySelection(colName: string, value: any) {
  await applyFilter({
    combinator: 'AND',
    conditions: [{ column: colName, op: 'eq', value }],
  })
}

// openFbF opens the Filter-by-Form panel, seeding one empty condition row per
// the first table column so the builder can pick column/op/value.
function openFbF() {
  if (!tableInfo.value) return
  const cols = tableInfo.value.columns
  fbfCombinator.value = 'AND'
  fbfConditions.value = [
    { column: cols[0]?.name ?? '', op: 'eq', value: '' },
    { column: cols[1]?.name ?? cols[0]?.name ?? '', op: 'eq', value: '' },
  ]
  showFbF.value = true
}

function addFbFCondition() {
  const cols = tableInfo.value?.columns ?? []
  fbfConditions.value.push({ column: cols[0]?.name ?? '', op: 'eq', value: '' })
}

// applyFbF builds a FilterSpec from the panel's condition rows, dropping any
// rows with no column or (for value-bearing ops) no value.
async function applyFbF() {
  const conditions = fbfConditions.value
    .filter(c => c.column && (c.op === 'is_null' || c.op === 'is_not_null' || c.value !== ''))
    .map(c => (c.op === 'is_null' || c.op === 'is_not_null')
      ? { column: c.column, op: c.op }
      : { column: c.column, op: c.op, value: c.value })
  if (conditions.length === 0) return
  showFbF.value = false
  await applyFilter({ combinator: fbfCombinator.value, conditions })
}

// saveFilterAsQuery persists the active filter as a named query (P2.6).
async function saveFilterAsQuery() {
  if (!activeFilter.value || !selectedTable.value) return
  const columns = tableData.value?.columns ?? []
  const title = `${selectedTable.value} (filtered)`
  try {
    await window.go.main.App.SaveFilterAsQuery(
      selectedTable.value, activeFilter.value, columns, title
    )
    statusMessage.value = `Saved query "${title}". Find it under Queries.`
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + errorText(e)
  }
}

function toggleSort(col: string) {
  if (sortCol.value === col) {
    sortDir.value = sortDir.value === 'ASC' ? 'DESC' : 'ASC'
  } else {
    sortCol.value = col
    sortDir.value = 'ASC'
  }
  loadData()
}

function prevPage() {
  if (currentPage.value > 1) {
    currentPage.value--
    loadData()
  }
}

function nextPage() {
  if (tableData.value && currentPage.value < tableData.value.total_pages) {
    currentPage.value++
    loadData()
  }
}

async function createTable() {
  const cols = newTableCols.value.filter(c => c.name.trim())
  if (!newTableName.value.trim() || cols.length === 0) return
  try {
    await window.go.main.App.CreateTable(newTableName.value, cols)
    showNewTableModal.value = false
    newTableName.value = ''
    newTableCols.value = [{ name: '', type: 'TEXT', not_null: false, pk: false }]
    await loadTables()
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + errorText(e)
  }
}

function addColumnDef() {
  newTableCols.value.push({ name: '', type: 'TEXT', not_null: false, pk: false })
}

function dropTable(name: string) {
  pendingDropTable.value = name
  showConfirmDrop.value = true
}

async function confirmDropTable() {
  const name = pendingDropTable.value
  showConfirmDrop.value = false
  pendingDropTable.value = null
  if (!name) return
  try {
    await window.go.main.App.DropTable(name)
    if (selectedTable.value === name) {
      selectedTable.value = null
      tableData.value = null
      tableInfo.value = null
    }
    await loadTables()
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + errorText(e)
  }
}

function openAddRow() {
  if (!tableInfo.value || locked.value) return
  newRowValues.value = {}
  for (const col of tableInfo.value.columns) {
    newRowValues.value[col.name] = ''
  }
  showAddRowModal.value = true
}

async function insertRow() {
  if (!selectedTable.value) return
  const vals: Record<string, any> = {}
  for (const [k, v] of Object.entries(newRowValues.value)) {
    if (v !== '') vals[k] = v
  }
  try {
    await window.go.main.App.InsertRow(selectedTable.value, vals)
    showAddRowModal.value = false
    await loadData()
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + errorText(e)
  }
}

function deleteRow(row: any[]) {
  if (!selectedTable.value || !tableInfo.value) return
  const pkCol = tableInfo.value.columns.find(c => c.pk)
  if (!pkCol) {
    errorMessage.value = 'Cannot delete: no primary key column found'
    return
  }
  pendingDeleteRow.value = row
  showConfirmDeleteRow.value = true
}

async function confirmDeleteRow() {
  const row = pendingDeleteRow.value
  showConfirmDeleteRow.value = false
  pendingDeleteRow.value = null
  if (!row || !selectedTable.value || !tableInfo.value) return
  const pkCol = tableInfo.value.columns.find(c => c.pk)
  if (!pkCol) return
  const pkIdx = tableData.value?.columns.indexOf(pkCol.name) ?? -1
  if (pkIdx < 0) return
  try {
    await window.go.main.App.DeleteRow(selectedTable.value, pkCol.name, row[pkIdx])
    await loadData()
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + errorText(e)
  }
}

// pkColumn returns the primary-key ColumnInfo for the selected table, if any.
function pkColumn(): ColumnInfo | undefined {
  return tableInfo.value?.columns.find(c => c.pk)
}

// startEdit opens an inline editor for a cell. Editing is only allowed when the
// table has a primary key (so UpdateRow has something to target) and the cell
// being edited is not itself the primary-key column (PKs are immutable here).
function startEdit(rowIdx: number, colIdx: number) {
  if (!tableData.value || locked.value) return
  // If already editing this exact cell, keep the in-flight value (avoids a
  // single-click resetting an edit the user started with double-click).
  if (editing.value && editing.value.row === rowIdx && editing.value.col === colIdx) return
  const pk = pkColumn()
  if (!pk) {
    errorMessage.value = 'Cannot edit: no primary key column found'
    return
  }
  if (tableData.value.columns[colIdx] === pk.name) return
  // Clear any stale cell error when (re)opening an editor on a different cell.
  if (cellError.value && !isCellError(tableData.value.rows[rowIdx], colIdx)) {
    cellError.value = null
  }
  const cell = tableData.value.rows[rowIdx][colIdx]
  editValue.value = cellEditText(cell, colType(colIdx))
  editing.value = { row: rowIdx, col: colIdx }
}

function cancelEdit() {
  editing.value = null
  editValue.value = ''
  cellError.value = null
}

async function saveEdit() {
  const cur = editing.value
  if (!cur || !selectedTable.value || !tableData.value) return
  const pk = pkColumn()
  if (!pk) { cancelEdit(); return }
  const pkIdx = tableData.value.columns.indexOf(pk.name)
  if (pkIdx < 0) { cancelEdit(); return }

  const colName = tableData.value.columns[cur.col]
  const row = tableData.value.rows[cur.row]
  const original = row[cur.col]
  const next = editValue.value
  // No change → just close the editor; avoid a needless round-trip. Compare
  // against cellEditText's rendering, not String(original) — a DECIMAL's
  // original wire number drops trailing zeros under String() (12.5 vs the
  // seeded "12.5000"), which made every DECIMAL edit look "changed" even
  // when the user left the seeded text untouched.
  if (cellEditText(original, colType(cur.col)) === next) { cancelEdit(); return }

  savingCell.value = true
  cellError.value = null
  const rowIdx = cur.row
  const colIdx = cur.col
  // Optimistic update: reflect the new value immediately, then persist.
  row[cur.col] = next === '' ? null : next
  editing.value = null
  try {
    await window.go.main.App.UpdateRow(
      selectedTable.value, pk.name, row[pkIdx], { [colName]: next }
    )
    await loadData()
  } catch (e: any) {
    // Backend validation/constraint error: revert the optimistic change so the
    // bad value is NOT persisted, and surface cell-level feedback identifying
    // exactly which cell was rejected (not just a global toast).
    row[colIdx] = original
    const msg = e ? errorText(e) : 'Update rejected'
    cellError.value = { table: selectedTable.value!, pk: String(row[pkIdx]), col: colName, message: msg }
  } finally {
    savingCell.value = false
  }
}

// commitValue is the explicit-value variant used by the checkbox/combo/date
// editors, whose change event carries the chosen value directly. The save is
// deferred to a microtask so the editor element that fired the change event is
// not torn down synchronously inside its own DOM event dispatch.
function commitValue(value: string) {
  editValue.value = value
  return Promise.resolve().then(() => saveEdit())
}

onMounted(loadTables)
// Holds EventsOn's own unsubscribe function — review fix, step 4a round 2
// #2: EventsOff('data:changed') would remove DashboardView's listener too.
let unsubDataChanged: (() => void) | null = null
onMounted(() => { if (hasWailsRuntime()) unsubDataChanged = EventsOn('data:changed', onDataChanged) })
onUnmounted(() => { unsubDataChanged?.() })

// Escape clears a standing cell error when no editor is open — useModalKeys
// (used by the New table / Add row modals) stops propagation at document,
// so an open modal's Escape never reaches this (#91 M-F16).
function handleGlobalKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && !editing.value) {
    cellError.value = null
  }
}
onMounted(() => window.addEventListener('keydown', handleGlobalKeydown))
onUnmounted(() => window.removeEventListener('keydown', handleGlobalKeydown))

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
watch(statusMessage, (msg) => {
  if (msg) setTimeout(() => { statusMessage.value = '' }, 4000)
})
</script>

<template>
  <div class="flex h-full">
    <!-- Data area -->
    <div class="flex-1 overflow-auto flex flex-col">
      <div v-if="!selectedTable && tables.length === 0 && !loading && unavailable.length" class="flex-1 flex flex-col items-center justify-center gap-4" data-testid="tables-unavailable">
        <div class="empty-hero">
          <div class="glyph"><Icon name="lock" :size="48" /></div>
          <h2>Can't list these tables yet</h2>
          <p class="lead">{{ unavailable[0].connection }} is {{ unavailable[0].status === 'locked-by' ? 'in use by another program, so its tables can only be read once it lets go.' : 'not available right now.' }}</p>
          <details v-if="unavailable[0].detail && unavailable[0].status !== 'locked-by'" class="help" style="max-width: 560px;">
            <summary style="cursor: pointer; font-weight: 600;">Show details</summary>
            <div class="details" style="margin-top: 8px; overflow-wrap: anywhere;">{{ unavailable[0].detail }}</div>
          </details>
        </div>
      </div>
      <div v-else-if="!selectedTable && tables.length === 0 && !loading" class="flex-1 flex flex-col items-center justify-center gap-4">
        <div class="empty-hero">
          <div class="glyph"><Icon name="database" :size="48" /></div>
          <h2>No tables yet</h2>
          <p class="lead">Create a table or import a CSV to get started.</p>
          <button @click="showNewTableModal = true" class="btn btn-primary" :disabled="isReadOnly" :title="isReadOnly ? readOnlyReason : undefined"><Icon name="plus" :size="16" /> Create table</button>
        </div>
      </div>
      <div v-else-if="!selectedTable" class="flex-1 flex flex-col items-center justify-center gap-3 text-[var(--sh-graphite)]" data-testid="pick-from-rail">
        <span class="sh-mono">Choose a table in the Connections list on the left.</span>
        <button @click="showNewTableModal = true" class="btn btn-sm" :disabled="isReadOnly" :title="isReadOnly ? readOnlyReason : 'Create a table'" data-testid="new-table"><Icon name="plus" :size="14" /> New table</button>
      </div>
      <template v-else>
        <!-- Toolbar -->
        <div class="page-subhead" style="padding: 14px 24px;">
          <h1 style="font-size: 22px;">{{ selectedTable }}</h1>
          <span class="flex-1"></span>
          <button @click="openFbF" class="btn btn-sm"><Icon name="filter" :size="14" /> Filter by Form</button>
          <button v-if="activeFilter" @click="saveFilterAsQuery" class="btn btn-sm"><Icon name="save" :size="14" /> Save as query</button>
          <button v-if="activeFilter" @click="(clearFilter(), loadData())" class="btn btn-ghost btn-sm"><Icon name="x" :size="14" /> Clear filter</button>
          <button v-if="!locked && selectedKind === 'table'" @click="dropTable(selectedTable!)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);" data-testid="drop-table"><Icon name="trash" :size="14" /> Drop table</button>
          <span v-if="locked && caps" class="badge" data-testid="readonly-badge"><Icon name="lock" :size="11" /> Read-only</span>
          <button @click="openAddRow" class="btn btn-primary btn-sm" :disabled="locked" :title="locked ? lockText : 'Add a row'" data-testid="add-row"><Icon name="plus" :size="14" /> Add row</button>
        </div>
        <!-- Why this table can't be edited (SPEC 07 E). -->
        <div v-if="caps && !caps.editable" class="note attn" role="status" data-testid="readonly-reason" style="margin: 12px 24px 0;">
          <Icon name="lock" :size="16" />
          <div>
            <b>This table is read-only.</b>{{ ' ' }}<span>{{ caps.text }}</span>
            <span v-if="caps.reason === 'connection-read-only'" class="help" style="display: block;">To change data, turn off Read-only for the connection. For production data, use a database user that is allowed to write.</span>
          </div>
          <button v-if="caps.reason === 'connection-read-only'" class="btn btn-sm" style="margin-left: auto;" @click="router.push(`/connections/${caps.connection}`)">Edit connection</button>
        </div>

        <!-- Record-count bar -->
        <div class="record-count-bar px-6 py-1.5 border-b border-line sh-mono text-xs text-[var(--sh-graphite)] flex items-center gap-2">
          <span v-if="activeFilter" class="badge badge--teal">Filtered</span>
          <span>{{ (activeFilter ? (filteredTotal ?? 0) : (tableData?.total_rows ?? 0)) }} records</span>
        </div>

        <!-- Filter-by-Form panel -->
        <div v-if="showFbF" class="px-6 py-3 border-b-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)]">
          <div class="flex items-center gap-2 mb-2">
            <span class="sh-label" style="margin:0;">Filter by Form</span>
            <select v-model="fbfCombinator" class="field-input fbf-combinator w-24">
              <option value="AND">AND</option>
              <option value="OR">OR</option>
            </select>
          </div>
          <div v-for="(cond, i) in fbfConditions" :key="i" class="flex gap-2 mb-2">
            <select v-model="cond.column" class="field-input fbf-column flex-1">
              <option v-for="c in (tableInfo?.columns || [])" :key="c.name" :value="c.name">{{ c.name }}</option>
            </select>
            <select v-model="cond.op" class="field-input fbf-op w-28">
              <option v-for="op in FILTER_OPS" :key="op.value" :value="op.value">{{ op.label }}</option>
            </select>
            <input
              v-model="cond.value"
              class="field-input fbf-value flex-1"
              :disabled="cond.op === 'is_null' || cond.op === 'is_not_null'"
              placeholder="value"
            />
          </div>
          <div class="flex gap-2 justify-end">
            <button @click="addFbFCondition" class="btn btn-ghost btn-sm"><Icon name="plus" :size="14" /> Condition</button>
            <button @click="showFbF = false" class="btn btn-ghost btn-sm">Cancel</button>
            <button @click="applyFbF" class="btn btn-primary btn-sm">Apply</button>
          </div>
        </div>

        <!-- Table -->
        <div v-if="dataLoading" class="p-4 sh-mono text-[var(--sh-graphite)]">Loading…</div>
        <div v-else-if="tableData" class="flex-1 overflow-auto">
          <table class="brutal-table">
            <thead class="sticky top-0">
              <tr>
                <th
                  v-for="(col, ci) in tableData.columns"
                  :key="col"
                  :class="{ 'cell-num': isNumericType(colType(ci)) }"
                  @click="toggleSort(col)"
                >
                  {{ col }}
                  <span v-if="sortCol === col">{{ sortDir === 'ASC' ? ' ↑' : ' ↓' }}</span>
                </th>
                <th class="w-16"></th>
              </tr>
            </thead>
            <tbody>
              <template v-if="tableData.rows && tableData.rows.length > 0">
                <tr v-for="(row, i) in tableData.rows" :key="i">
                  <td
                    v-for="(cell, j) in row"
                    :key="j"
                    :aria-readonly="locked ? 'true' : undefined"
                    @click="startEdit(i, j)"
                    @dblclick="startEdit(i, j)"
                    :class="[
                      editorType(tableData.columns[j]) === 'checkbox' ? [] : cellClass(formattedRows[i][j], colType(j)),
                      {
                        'cell-editing': editing && editing.row === i && editing.col === j,
                        'cell-error': isCellError(row, j),
                      },
                    ]"
                    :title="editorType(tableData.columns[j]) === 'checkbox' ? undefined : formattedRows[i][j].title"
                  >
                    <template v-if="editing && editing.row === i && editing.col === j">
                      <!-- Boolean checkbox editor. -->
                      <input
                        v-if="editorType(tableData.columns[j]) === 'checkbox'"
                        type="checkbox"
                        class="cell-checkbox"
                        :checked="editValue === 'true' || editValue === '1'"
                        :disabled="savingCell"
                        @change="commitValue(($event.target as HTMLInputElement).checked ? 'true' : 'false')"
                        @keydown.esc.prevent="cancelEdit"
                        v-focus
                      />
                      <!-- Date editor. -->
                      <input
                        v-else-if="editorType(tableData.columns[j]) === 'date'"
                        type="date"
                        v-model="editValue"
                        class="cell-input"
                        :disabled="savingCell"
                        @keydown.enter.prevent="saveEdit"
                        @keydown.esc.prevent="cancelEdit"
                        @blur="saveEdit"
                        v-focus
                      />
                      <!-- Default free-text editor. -->
                      <input
                        v-else
                        v-model="editValue"
                        class="cell-input"
                        :disabled="savingCell"
                        @keydown.enter.prevent="saveEdit"
                        @keydown.esc.prevent="cancelEdit"
                        @blur="saveEdit"
                        v-focus
                      />
                    </template>
                    <template v-else>
                      <input
                        v-if="editorType(tableData.columns[j]) === 'checkbox'"
                        type="checkbox"
                        :checked="cell === true || cell === 'true' || cell === 1 || cell === '1'"
                        disabled
                        class="cell-checkbox"
                      />
                      <span v-else>{{ formattedRows[i][j].text }}</span>
                      <!-- Filter-by-Selection affordance: a funnel that filters
                           the datasheet to rows where this column equals this
                           cell's value (Access right-click "Filter by Selection").
                           Passes the raw wire value (filterBySelection needs the
                           exact value to match against, not its display text);
                           the title uses the faithfully formatted text. -->
                      <button
                        class="filter-selection-btn"
                        data-filter-selection
                        :title="`Filter by ${tableData.columns[j]} = ${formattedRows[i][j].text}`"
                        @click.stop="filterBySelection(tableData.columns[j], cell)"
                      ><Icon name="filter" :size="11" /></button>
                    </template>
                    <div
                      v-if="isCellError(row, j)"
                      class="cell-error-msg"
                      :title="cellError!.message"
                    >{{ cellError!.message.split('\n')[0] }}</div>
                  </td>
                  <td>
                    <button v-if="!locked" @click="deleteRow(row)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);" aria-label="Delete row" title="Delete row"><Icon name="trash" :size="14" /></button>
                  </td>
                </tr>
              </template>
              <tr v-else>
                <td :colspan="tableData.columns.length + 1" class="text-center text-[var(--sh-graphite)] sh-mono py-8">No rows</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Pagination -->
        <div v-if="tableData && tableData.total_pages > 1" class="px-4 py-2 border-t-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex items-center justify-between text-sm">
          <button @click="prevPage" :disabled="currentPage <= 1" class="btn btn-sm"><Icon name="arrowLeft" :size="14" /> Prev</button>
          <span class="sh-mono text-xs text-[var(--sh-graphite)]">Page {{ currentPage }} of {{ tableData.total_pages }}</span>
          <button @click="nextPage" :disabled="currentPage >= tableData.total_pages" class="btn btn-sm">Next <Icon name="arrowRight" :size="14" /></button>
        </div>
      </template>
    </div>

    <!-- New table modal -->
    <div v-if="showNewTableModal" class="modal-overlay" @click.self="showNewTableModal = false" role="dialog" aria-modal="true" aria-label="Create table">
      <div ref="newTableEl" class="card card-pad w-full max-w-lg">
        <h3 class="card-title mb-4">Create table</h3>
        <input v-model="newTableName" placeholder="Table name" class="field-input mb-4" />
        <div class="space-y-2 mb-4">
          <div v-for="(col, i) in newTableCols" :key="i" class="flex gap-2">
            <input v-model="col.name" placeholder="Column name" class="field-input flex-1" />
            <select v-model="col.type" class="field-input w-28">
              <option>TEXT</option>
              <option>INTEGER</option>
              <option>REAL</option>
              <option>BLOB</option>
            </select>
            <label class="flex items-center gap-1 text-xs font-semibold sh-mono"><input type="checkbox" v-model="col.pk" style="accent-color: var(--sh-teal);" /> PK</label>
          </div>
          <button @click="addColumnDef" class="btn btn-ghost btn-sm"><Icon name="plus" :size="14" /> Add column</button>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="showNewTableModal = false" class="btn btn-ghost">Cancel</button>
          <button @click="createTable" class="btn btn-primary">Create</button>
        </div>
      </div>
    </div>

    <!-- Add row modal -->
    <div v-if="showAddRowModal && tableInfo" class="modal-overlay" @click.self="showAddRowModal = false" role="dialog" aria-modal="true" aria-label="Add row">
      <div ref="addRowEl" class="card card-pad w-full max-w-lg max-h-[80vh] overflow-auto">
        <h3 class="card-title mb-4">Add row to {{ selectedTable }}</h3>
        <div class="space-y-3 mb-4">
          <div v-for="col in tableInfo.columns" :key="col.name">
            <label class="field-label">{{ col.name }} <span class="text-[var(--sh-graphite)]">({{ col.type }})</span></label>
            <input v-model="newRowValues[col.name]" class="field-input" />
          </div>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="showAddRowModal = false" class="btn btn-ghost">Cancel</button>
          <button @click="insertRow" class="btn btn-primary">Insert</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDrop"
      title="Drop table"
      confirm-label="Drop table"
      :message="`Drop table '${pendingDropTable}'? This cannot be undone.`"
      @confirm="confirmDropTable"
      @cancel="showConfirmDrop = false; pendingDropTable = null"
    />

    <ConfirmDialog
      :visible="showConfirmDeleteRow"
      title="Delete row"
      confirm-label="Delete row"
      message="Delete this row?"
      @confirm="confirmDeleteRow"
      @cancel="showConfirmDeleteRow = false; pendingDeleteRow = null"
    />

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
    <div v-if="statusMessage" class="status-toast" data-testid="table-status">{{ statusMessage }}</div>
  </div>
</template>

<style scoped>
.brutal-table td {
  cursor: text;
}
.brutal-table td.cell-editing {
  padding: 0;
}
.cell-input {
  width: 100%;
  box-sizing: border-box;
  border: 2px solid var(--sh-teal);
  background: var(--sh-paper);
  padding: 6px 8px;
  font: inherit;
  color: var(--sh-ink);
  outline: none;
}
.cell-checkbox {
  accent-color: var(--sh-teal);
}
/* Filter-by-Selection affordance: an inline funnel button that only reveals
   itself on cell hover, so the datasheet stays clean until the user reaches
   for it. The cell text is rendered by the adjacent span; this button repeats
   the value purely so the funnel sits next to the value it filters on. */
.filter-selection-btn {
  display: none;
  align-items: center;
  gap: 3px;
  margin-left: 6px;
  padding: 0 4px;
  border: 1px solid var(--sh-teal);
  background: var(--sh-paper);
  color: var(--sh-teal);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
  border-radius: 2px;
}
.brutal-table td:hover .filter-selection-btn {
  display: inline-flex;
}
.brutal-table td.cell-error {
  outline: 2px solid var(--sh-error);
  outline-offset: -2px;
  background: var(--sh-error-8);
}
.cell-error-msg {
  font-size: 11px;
  color: var(--sh-error);
  margin-top: 2px;
  font-family: var(--sh-font-mono);
}
</style>
