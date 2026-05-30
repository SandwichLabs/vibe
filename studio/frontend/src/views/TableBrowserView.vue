<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'

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

const tables = ref<string[]>([])
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
const newTableName = ref('')
const newTableCols = ref<ColumnInfo[]>([{ name: '', type: 'TEXT', not_null: false, pk: false }])
const newRowValues = ref<Record<string, string>>({})
const errorMessage = ref('')
const showConfirmDrop = ref(false)
const pendingDropTable = ref<string | null>(null)
const showConfirmDeleteRow = ref(false)
const pendingDeleteRow = ref<any[] | null>(null)

// Inline edit-in-place state. editing identifies the cell currently being
// edited by row index + column index; editValue holds the in-flight string.
const editing = ref<{ row: number; col: number } | null>(null)
const editValue = ref('')
const savingCell = ref(false)

// Field metadata for the selected table, keyed by column name. Drives the
// choice of inline editor (combo / checkbox / date / text) per P1.4 parity.
// FieldMeta is declared globally in env.d.ts (mirrors internal/schema.FieldMeta).
const fieldMeta = ref<Record<string, FieldMeta>>({})
// Lookup options for combo editors, keyed by column name. Each option carries
// the hidden stored value (PK / value_col) and the human-readable label.
const lookupOptions = ref<Record<string, { value: any; label: string }[]>>({})

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
const cellError = ref<{ row: number; col: number; message: string } | null>(null)

// editorType resolves the inline editor to use for a column from its field
// meta control_type, falling back to the table's declared SQL type. Returns
// one of: 'combo' | 'checkbox' | 'date' | 'text'.
function editorType(colName: string): 'combo' | 'checkbox' | 'date' | 'text' {
  const meta = fieldMeta.value[colName]
  if (meta) {
    const ct = (meta.control_type || '').toLowerCase()
    if (ct === 'combo' || ct === 'combobox' || ct === 'lookup') return 'combo'
    if (ct === 'checkbox' || ct === 'boolean') return 'checkbox'
    if (ct === 'date' || ct === 'datepicker') return 'date'
    if (ct === 'textbox' || ct === 'text') return 'text'
  }
  // Fall back to the declared column type for sensible defaults.
  const col = tableInfo.value?.columns.find(c => c.name === colName)
  const t = (col?.type || '').toUpperCase()
  if (t.includes('BOOL')) return 'checkbox'
  if (t.includes('DATE') || t.includes('TIME')) return 'date'
  return 'text'
}

// loadFieldMeta fetches Access-style field metadata for the table and, for any
// combo/lookup column, eagerly resolves its option list via ExecuteQuery so the
// inline combo can show labels while storing hidden values (value_col).
async function loadFieldMeta(table: string) {
  fieldMeta.value = {}
  lookupOptions.value = {}
  let metas: FieldMeta[] = []
  try {
    metas = (await window.go.main.App.GetFieldMeta(table)) || []
  } catch { metas = [] }
  const map: Record<string, FieldMeta> = {}
  for (const m of metas) map[m.column_name] = m
  fieldMeta.value = map

  for (const m of metas) {
    if (editorType(m.column_name) !== 'combo') continue
    if (!m.lookup_sql) continue
    try {
      const res = await window.go.main.App.ExecuteQuery(m.lookup_sql)
      const cols: string[] = (res?.columns || []).map((c: any) => c.name ?? c)
      const valIdx = m.value_col ? cols.indexOf(m.value_col) : 0
      const dispIdx = m.display_col ? cols.indexOf(m.display_col) : (cols.length > 1 ? 1 : 0)
      lookupOptions.value[m.column_name] = (res?.rows || []).map((r: any[]) => ({
        value: r[valIdx >= 0 ? valIdx : 0],
        label: String(r[dispIdx >= 0 ? dispIdx : 0] ?? ''),
      }))
    } catch { /* leave combo without options; falls back to free text */ }
  }
}

// comboLabel renders the human-readable label for a stored combo value.
function comboLabel(colName: string, value: any): string {
  const opts = lookupOptions.value[colName]
  if (opts) {
    const hit = opts.find(o => String(o.value) === String(value))
    if (hit) return hit.label
  }
  return value == null ? '' : String(value)
}

async function loadTables() {
  loading.value = true
  try {
    tables.value = (await window.go.main.App.ListTables()) || []
  } catch { /* ignore */ }
  loading.value = false
}

async function selectTable(name: string) {
  selectedTable.value = name
  currentPage.value = 1
  sortCol.value = ''
  sortDir.value = 'ASC'
  clearFilter()
  try {
    tableInfo.value = await window.go.main.App.DescribeTable(name)
  } catch { tableInfo.value = null }
  await loadFieldMeta(name)
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
  try {
    await window.go.main.App.SaveFilterAsQuery(
      selectedTable.value, activeFilter.value, columns, `${selectedTable.value} (filtered)`
    )
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + (e.message || e)
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
    errorMessage.value = 'Failed: ' + (e.message || e)
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
    errorMessage.value = 'Failed: ' + (e.message || e)
  }
}

function openAddRow() {
  if (!tableInfo.value) return
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
    errorMessage.value = 'Failed: ' + (e.message || e)
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
    errorMessage.value = 'Failed: ' + (e.message || e)
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
  if (!tableData.value) return
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
  if (cellError.value && (cellError.value.row !== rowIdx || cellError.value.col !== colIdx)) {
    cellError.value = null
  }
  const cell = tableData.value.rows[rowIdx][colIdx]
  editValue.value = cell == null ? '' : String(cell)
  editing.value = { row: rowIdx, col: colIdx }
}

function cancelEdit() {
  editing.value = null
  editValue.value = ''
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
  // No change → just close the editor; avoid a needless round-trip.
  if ((original == null ? '' : String(original)) === next) { cancelEdit(); return }

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
    const msg = (e && (e.message || e)) ? String(e.message || e) : 'Update rejected'
    cellError.value = { row: rowIdx, col: colIdx, message: msg }
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

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="flex h-full">
    <!-- Table list -->
    <div class="w-56 border-r-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex flex-col">
      <div class="px-4 py-3 border-b-2 border-[var(--sh-ink)] flex items-center justify-between">
        <h2 class="sh-label" style="margin: 0;">Tables</h2>
        <button @click="showNewTableModal = true" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 sh-mono text-[var(--sh-graphite)]">Loading…</div>
        <div v-else-if="tables.length === 0" class="p-4 sh-mono text-[var(--sh-graphite)] text-center">No tables</div>
        <div
          v-for="t in tables"
          :key="t"
          @click="selectTable(t)"
          class="px-4 py-2 cursor-pointer border-b border-[rgba(31,35,48,0.12)] flex items-center justify-between group text-sm font-medium"
          :class="selectedTable === t ? 'row-selected' : 'hover:bg-[rgba(31,35,48,0.04)]'"
        >
          <span class="truncate flex items-center gap-2"><Icon name="database" :size="14" /> {{ t }}</span>
          <button @click.stop="dropTable(t)" class="hidden group-hover:block btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
        </div>
      </div>
    </div>

    <!-- Data area -->
    <div class="flex-1 overflow-auto flex flex-col">
      <div v-if="!selectedTable && tables.length === 0 && !loading" class="flex-1 flex flex-col items-center justify-center gap-4">
        <div class="empty-hero">
          <div class="glyph"><Icon name="database" :size="48" /></div>
          <h2>No tables yet</h2>
          <p class="lead">Create a table or import a CSV to get started.</p>
          <button @click="showNewTableModal = true" class="btn btn-primary"><Icon name="plus" :size="16" /> Create table</button>
        </div>
      </div>
      <div v-else-if="!selectedTable" class="flex-1 flex items-center justify-center text-[var(--sh-graphite)] sh-mono">Select a table</div>
      <template v-else>
        <!-- Toolbar -->
        <div class="page-subhead" style="padding: 14px 24px;">
          <h1 style="font-size: 22px;">{{ selectedTable }}</h1>
          <span class="flex-1"></span>
          <button @click="openFbF" class="btn btn-sm"><Icon name="filter" :size="14" /> Filter by Form</button>
          <button v-if="activeFilter" @click="saveFilterAsQuery" class="btn btn-sm"><Icon name="save" :size="14" /> Save as query</button>
          <button v-if="activeFilter" @click="(clearFilter(), loadData())" class="btn btn-ghost btn-sm"><Icon name="x" :size="14" /> Clear filter</button>
          <button @click="openAddRow" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> Add row</button>
        </div>

        <!-- Record-count bar -->
        <div class="record-count-bar px-6 py-1.5 border-b border-[rgba(31,35,48,0.12)] sh-mono text-xs text-[var(--sh-graphite)] flex items-center gap-2">
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
                  v-for="col in tableData.columns"
                  :key="col"
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
                    @click="startEdit(i, j)"
                    @dblclick="startEdit(i, j)"
                    :title="fieldMeta[tableData.columns[j]]?.control_tip || ''"
                    :class="{
                      'cell-editing': editing && editing.row === i && editing.col === j,
                      'cell-error': cellError && cellError.row === i && cellError.col === j,
                    }"
                  >
                    <template v-if="editing && editing.row === i && editing.col === j">
                      <!-- Combo / lookup editor: shows labels, stores hidden value. -->
                      <select
                        v-if="editorType(tableData.columns[j]) === 'combo'"
                        class="cell-input"
                        :value="editValue"
                        :disabled="savingCell"
                        @change="commitValue(($event.target as HTMLSelectElement).value)"
                        @keydown.esc.prevent="cancelEdit"
                        v-focus
                      >
                        <!-- Preserve the current value if it is not in the option
                             list (e.g. an orphaned FK) so it is not silently lost. -->
                        <option
                          v-if="!(lookupOptions[tableData.columns[j]] || []).some(o => String(o.value) === editValue)"
                          :value="editValue"
                        >{{ comboLabel(tableData.columns[j], editValue) }}</option>
                        <option
                          v-for="opt in (lookupOptions[tableData.columns[j]] || [])"
                          :key="String(opt.value)"
                          :value="String(opt.value)"
                        >{{ opt.label }}</option>
                      </select>
                      <!-- Boolean checkbox editor. -->
                      <input
                        v-else-if="editorType(tableData.columns[j]) === 'checkbox'"
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
                      <span v-if="editorType(tableData.columns[j]) === 'combo'">{{ comboLabel(tableData.columns[j], cell) }}</span>
                      <input
                        v-else-if="editorType(tableData.columns[j]) === 'checkbox'"
                        type="checkbox"
                        :checked="cell === true || cell === 'true' || cell === 1 || cell === '1'"
                        disabled
                        class="cell-checkbox"
                      />
                      <span v-else>{{ cell ?? '' }}</span>
                      <!-- Filter-by-Selection affordance: a funnel that filters
                           the datasheet to rows where this column equals this
                           cell's value (Access right-click "Filter by Selection"). -->
                      <button
                        class="filter-selection-btn"
                        data-filter-selection
                        :title="`Filter by ${tableData.columns[j]} = ${cell}`"
                        @click.stop="filterBySelection(tableData.columns[j], cell)"
                      ><Icon name="filter" :size="11" /></button>
                    </template>
                    <div
                      v-if="cellError && cellError.row === i && cellError.col === j"
                      class="cell-error-msg"
                    >{{ cellError.message }}</div>
                  </td>
                  <td>
                    <button @click="deleteRow(row)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
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
    <div v-if="showNewTableModal" class="modal-overlay" @click.self="showNewTableModal = false">
      <div class="card card-pad w-full max-w-lg">
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
    <div v-if="showAddRowModal && tableInfo" class="modal-overlay" @click.self="showAddRowModal = false">
      <div class="card card-pad w-full max-w-lg max-h-[80vh] overflow-auto">
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
      title="Drop Table"
      :message="`Drop table '${pendingDropTable}'? This cannot be undone.`"
      @confirm="confirmDropTable"
      @cancel="showConfirmDrop = false; pendingDropTable = null"
    />

    <ConfirmDialog
      :visible="showConfirmDeleteRow"
      title="Delete Row"
      message="Delete this row?"
      @confirm="confirmDeleteRow"
      @cancel="showConfirmDeleteRow = false; pendingDeleteRow = null"
    />

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
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
  background: rgba(220, 38, 38, 0.06);
}
.cell-error-msg {
  font-size: 11px;
  color: var(--sh-error);
  margin-top: 2px;
  font-family: var(--sh-mono, monospace);
}
</style>
