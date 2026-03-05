<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'

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

async function loadTables() {
  loading.value = true
  try {
    tables.value = await window.go.main.App.ListTables()
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

onMounted(loadTables)

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="flex h-full">
    <!-- Table list -->
    <div class="w-56 border-r-3 border-black bg-white flex flex-col">
      <div class="p-3 border-b-3 border-black flex items-center justify-between">
        <h2 class="font-bold text-sm uppercase font-mono tracking-wide">Tables</h2>
        <button @click="showNewTableModal = true" class="btn btn-primary btn-sm">+ New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 text-muted font-mono text-sm">Loading...</div>
        <div v-else-if="tables.length === 0" class="p-4 text-muted text-sm text-center font-mono">No tables</div>
        <div
          v-for="t in tables"
          :key="t"
          @click="selectTable(t)"
          class="px-3 py-2 cursor-pointer border-b-2 border-black/10 flex items-center justify-between group text-sm font-medium"
          :class="selectedTable === t ? 'bg-yellow border-l-4 border-l-black' : 'hover:bg-cream'"
        >
          <span class="truncate">{{ t }}</span>
          <button @click.stop="dropTable(t)" class="hidden group-hover:block btn btn-ghost btn-sm text-error">Drop</button>
        </div>
      </div>
    </div>

    <!-- Data area -->
    <div class="flex-1 overflow-auto flex flex-col">
      <div v-if="!selectedTable" class="flex-1 flex items-center justify-center text-muted font-mono">Select a table</div>
      <template v-else>
        <!-- Toolbar -->
        <div class="px-4 py-2 border-b-3 border-black bg-white flex items-center justify-between">
          <div class="font-bold text-sm">{{ selectedTable }}</div>
          <div class="flex items-center gap-3">
            <span v-if="tableData" class="badge">{{ tableData.total_rows }} rows</span>
            <button @click="openAddRow" class="btn btn-primary btn-sm">+ Add Row</button>
          </div>
        </div>

        <!-- Table -->
        <div v-if="dataLoading" class="p-4 text-muted font-mono text-sm">Loading...</div>
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
              <tr v-for="(row, i) in tableData.rows" :key="i">
                <td v-for="(cell, j) in row" :key="j">{{ cell ?? '' }}</td>
                <td>
                  <button @click="deleteRow(row)" class="btn btn-ghost btn-sm text-error">Del</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Pagination -->
        <div v-if="tableData && tableData.total_pages > 1" class="px-4 py-2 border-t-3 border-black bg-white flex items-center justify-between text-sm">
          <button @click="prevPage" :disabled="currentPage <= 1" class="btn btn-sm">&larr; Prev</button>
          <span class="font-mono text-xs text-muted">Page {{ currentPage }} of {{ tableData.total_pages }}</span>
          <button @click="nextPage" :disabled="currentPage >= tableData.total_pages" class="btn btn-sm">Next &rarr;</button>
        </div>
      </template>
    </div>

    <!-- New table modal -->
    <div v-if="showNewTableModal" class="modal-overlay" @click.self="showNewTableModal = false">
      <div class="card w-full max-w-lg p-6">
        <h3 class="text-lg font-bold mb-4">Create Table</h3>
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
            <label class="flex items-center gap-1 text-xs font-bold font-mono"><input type="checkbox" v-model="col.pk" class="accent-yellow" /> PK</label>
          </div>
          <button @click="addColumnDef" class="btn btn-ghost btn-sm">+ Add column</button>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="showNewTableModal = false" class="btn btn-ghost">Cancel</button>
          <button @click="createTable" class="btn btn-primary">Create</button>
        </div>
      </div>
    </div>

    <!-- Add row modal -->
    <div v-if="showAddRowModal && tableInfo" class="modal-overlay" @click.self="showAddRowModal = false">
      <div class="card w-full max-w-lg p-6 max-h-[80vh] overflow-auto">
        <h3 class="text-lg font-bold mb-4">Add Row to {{ selectedTable }}</h3>
        <div class="space-y-3 mb-4">
          <div v-for="col in tableInfo.columns" :key="col.name">
            <label class="field-label">{{ col.name }} <span class="text-muted">({{ col.type }})</span></label>
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
