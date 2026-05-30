<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'

// Autofocus + select directive for the inline cell editor.
const vFocus = {
  mounted: (el: HTMLInputElement) => { el.focus(); el.select() },
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
  try {
    tableInfo.value = await window.go.main.App.DescribeTable(name)
  } catch { tableInfo.value = null }
  await loadData()
}

async function loadData() {
  if (!selectedTable.value) return
  dataLoading.value = true
  try {
    tableData.value = await window.go.main.App.GetTableData(
      selectedTable.value, currentPage.value, pageSize.value, sortCol.value, sortDir.value
    )
  } catch { tableData.value = null }
  dataLoading.value = false
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
  const pk = pkColumn()
  if (!pk) {
    errorMessage.value = 'Cannot edit: no primary key column found'
    return
  }
  if (tableData.value.columns[colIdx] === pk.name) return
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
  // Optimistic update: reflect the new value immediately, then persist.
  row[cur.col] = next === '' ? null : next
  editing.value = null
  try {
    await window.go.main.App.UpdateRow(
      selectedTable.value, pk.name, row[pkIdx], { [colName]: next }
    )
    await loadData()
  } catch (e: any) {
    row[cur.col] = original // revert optimistic change
    errorMessage.value = 'Failed: ' + (e.message || e)
  } finally {
    savingCell.value = false
  }
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
          <span v-if="tableData" class="badge badge--teal">{{ tableData.total_rows }} rows</span>
          <button @click="openAddRow" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> Add row</button>
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
                    @dblclick="startEdit(i, j)"
                    :class="{ 'cell-editing': editing && editing.row === i && editing.col === j }"
                  >
                    <input
                      v-if="editing && editing.row === i && editing.col === j"
                      v-model="editValue"
                      class="cell-input"
                      :disabled="savingCell"
                      @keydown.enter.prevent="saveEdit"
                      @keydown.esc.prevent="cancelEdit"
                      @blur="saveEdit"
                      v-focus
                    />
                    <template v-else>{{ cell ?? '' }}</template>
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
</style>
