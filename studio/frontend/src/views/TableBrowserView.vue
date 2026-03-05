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
    <div class="w-56 border-r border-gray-200 bg-white flex flex-col">
      <div class="p-3 border-b border-gray-200 flex items-center justify-between">
        <h2 class="font-semibold text-sm">Tables</h2>
        <button @click="showNewTableModal = true" class="text-blue-600 hover:text-blue-700 text-sm font-medium">+ New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 text-gray-500 text-sm">Loading...</div>
        <div v-else-if="tables.length === 0" class="p-4 text-gray-400 text-sm text-center">No tables</div>
        <div
          v-for="t in tables"
          :key="t"
          @click="selectTable(t)"
          class="px-3 py-2 cursor-pointer border-b border-gray-100 hover:bg-gray-50 flex items-center justify-between group text-sm"
          :class="{ 'bg-blue-50 border-l-2 border-l-blue-500': selectedTable === t }"
        >
          <span class="truncate">{{ t }}</span>
          <button @click.stop="dropTable(t)" class="hidden group-hover:block text-xs text-red-500 hover:text-red-700">Drop</button>
        </div>
      </div>
    </div>

    <!-- Data area -->
    <div class="flex-1 overflow-auto flex flex-col">
      <div v-if="!selectedTable" class="flex-1 flex items-center justify-center text-gray-400">Select a table</div>
      <template v-else>
        <!-- Toolbar -->
        <div class="px-4 py-2 border-b border-gray-200 bg-white flex items-center justify-between">
          <div class="font-semibold text-sm">{{ selectedTable }}</div>
          <div class="flex items-center gap-3">
            <span v-if="tableData" class="text-xs text-gray-500">{{ tableData.total_rows }} rows</span>
            <button @click="openAddRow" class="text-sm text-blue-600 hover:text-blue-700">+ Add Row</button>
          </div>
        </div>

        <!-- Table -->
        <div v-if="dataLoading" class="p-4 text-gray-500 text-sm">Loading...</div>
        <div v-else-if="tableData" class="flex-1 overflow-auto">
          <table class="w-full text-sm border-collapse">
            <thead class="sticky top-0">
              <tr>
                <th
                  v-for="col in tableData.columns"
                  :key="col"
                  @click="toggleSort(col)"
                  class="px-3 py-2 bg-gray-100 border-b border-gray-200 text-left font-medium cursor-pointer hover:bg-gray-200"
                >
                  {{ col }}
                  <span v-if="sortCol === col" class="text-xs">{{ sortDir === 'ASC' ? ' ↑' : ' ↓' }}</span>
                </th>
                <th class="px-3 py-2 bg-gray-100 border-b border-gray-200 w-16"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in tableData.rows" :key="i" class="hover:bg-gray-50">
                <td v-for="(cell, j) in row" :key="j" class="px-3 py-1.5 border-b border-gray-100">
                  {{ cell ?? '' }}
                </td>
                <td class="px-3 py-1.5 border-b border-gray-100">
                  <button @click="deleteRow(row)" class="text-xs text-red-500 hover:text-red-700">Del</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Pagination -->
        <div v-if="tableData && tableData.total_pages > 1" class="px-4 py-2 border-t border-gray-200 bg-white flex items-center justify-between text-sm">
          <button @click="prevPage" :disabled="currentPage <= 1" class="px-3 py-1 bg-gray-200 rounded disabled:opacity-50">&larr; Prev</button>
          <span class="text-gray-500">Page {{ currentPage }} of {{ tableData.total_pages }}</span>
          <button @click="nextPage" :disabled="currentPage >= tableData.total_pages" class="px-3 py-1 bg-gray-200 rounded disabled:opacity-50">Next &rarr;</button>
        </div>
      </template>
    </div>

    <!-- New table modal -->
    <div v-if="showNewTableModal" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50" @click.self="showNewTableModal = false">
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-lg">
        <h3 class="text-lg font-semibold mb-4">Create Table</h3>
        <input v-model="newTableName" placeholder="Table name" class="w-full px-3 py-2 border rounded-lg mb-4" />
        <div class="space-y-2 mb-4">
          <div v-for="(col, i) in newTableCols" :key="i" class="flex gap-2">
            <input v-model="col.name" placeholder="Column name" class="flex-1 px-2 py-1 border rounded text-sm" />
            <select v-model="col.type" class="px-2 py-1 border rounded text-sm">
              <option>TEXT</option>
              <option>INTEGER</option>
              <option>REAL</option>
              <option>BLOB</option>
            </select>
            <label class="flex items-center gap-1 text-xs"><input type="checkbox" v-model="col.pk" /> PK</label>
          </div>
          <button @click="addColumnDef" class="text-sm text-blue-600">+ Add column</button>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="showNewTableModal = false" class="px-4 py-2 text-gray-600">Cancel</button>
          <button @click="createTable" class="px-4 py-2 bg-blue-600 text-white rounded-lg">Create</button>
        </div>
      </div>
    </div>

    <!-- Add row modal -->
    <div v-if="showAddRowModal && tableInfo" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50" @click.self="showAddRowModal = false">
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-lg max-h-[80vh] overflow-auto">
        <h3 class="text-lg font-semibold mb-4">Add Row to {{ selectedTable }}</h3>
        <div class="space-y-3 mb-4">
          <div v-for="col in tableInfo.columns" :key="col.name">
            <label class="block text-sm font-medium text-gray-700 mb-1">{{ col.name }} <span class="text-gray-400">({{ col.type }})</span></label>
            <input v-model="newRowValues[col.name]" class="w-full px-3 py-2 border rounded-lg text-sm" />
          </div>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="showAddRowModal = false" class="px-4 py-2 text-gray-600">Cancel</button>
          <button @click="insertRow" class="px-4 py-2 bg-blue-600 text-white rounded-lg">Insert</button>
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

    <div v-if="errorMessage" class="fixed bottom-4 right-4 bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg text-sm z-50">
      {{ errorMessage }}
    </div>
  </div>
</template>
