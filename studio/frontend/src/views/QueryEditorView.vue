<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'

const queries = ref<QuerySummary[]>([])
const selectedId = ref<string | null>(null)
const sqlText = ref('')
const engineName = ref('sqlite')
const result = ref<QueryResult | null>(null)
const loading = ref(true)
const running = ref(false)
const error = ref('')
const showSaveModal = ref(false)
const saveTitle = ref('')
const errorMessage = ref('')
const showConfirmDelete = ref(false)
const pendingDeleteId = ref<string | null>(null)

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
    sqlText.value = q.sql
    engineName.value = q.engine
    saveTitle.value = q.title
  } catch (e: any) {
    error.value = e.message || String(e)
  }
}

async function runQuery() {
  if (!sqlText.value.trim()) return
  running.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await window.go.main.App.ExecuteQuery(sqlText.value, engineName.value)
  } catch (e: any) {
    error.value = e.message || String(e)
  }
  running.value = false
}

async function saveQuery() {
  if (!saveTitle.value.trim()) return
  try {
    await window.go.main.App.SaveQuery(selectedId.value || '', saveTitle.value, sqlText.value, engineName.value, '')
    showSaveModal.value = false
    await loadQueries()
  } catch (e: any) {
    errorMessage.value = 'Save failed: ' + (e.message || e)
  }
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
    }
    await loadQueries()
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + (e.message || e)
  }
}

function newQuery() {
  selectedId.value = null
  sqlText.value = ''
  engineName.value = 'sqlite'
  result.value = null
  error.value = ''
  saveTitle.value = ''
}

function handleKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault()
    runQuery()
  }
}

onMounted(loadQueries)

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="flex h-full">
    <!-- Query list -->
    <div class="w-56 border-r border-gray-200 bg-white flex flex-col">
      <div class="p-3 border-b border-gray-200 flex items-center justify-between">
        <h2 class="font-semibold text-sm">Queries</h2>
        <button @click="newQuery" class="text-blue-600 hover:text-blue-700 text-sm font-medium">+ New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 text-gray-500 text-sm">Loading...</div>
        <div v-else-if="queries.length === 0" class="p-4 text-gray-400 text-sm text-center">No saved queries</div>
        <div
          v-for="q in queries"
          :key="q.id"
          @click="selectQuery(q.id)"
          class="px-3 py-2 cursor-pointer border-b border-gray-100 hover:bg-gray-50 flex items-center justify-between group text-sm"
          :class="{ 'bg-blue-50 border-l-2 border-l-blue-500': selectedId === q.id }"
        >
          <div class="min-w-0">
            <div class="truncate font-medium">{{ q.title }}</div>
            <div class="text-xs text-gray-400">{{ q.engine }}</div>
          </div>
          <button @click.stop="deleteQuery(q.id)" class="hidden group-hover:block text-xs text-red-500">Del</button>
        </div>
      </div>
    </div>

    <!-- Editor area -->
    <div class="flex-1 flex flex-col">
      <!-- Toolbar -->
      <div class="px-4 py-2 border-b border-gray-200 bg-white flex items-center gap-3">
        <select v-model="engineName" class="px-2 py-1 border rounded text-sm">
          <option value="sqlite">SQLite</option>
          <option value="duckdb">DuckDB</option>
        </select>
        <button @click="runQuery" :disabled="running" class="px-4 py-1.5 bg-green-600 text-white rounded text-sm hover:bg-green-700 disabled:opacity-50">
          {{ running ? 'Running...' : 'Run (Ctrl+Enter)' }}
        </button>
        <button @click="showSaveModal = true" class="px-4 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700">
          Save Query
        </button>
      </div>

      <!-- SQL editor -->
      <div class="border-b border-gray-200">
        <textarea
          v-model="sqlText"
          @keydown="handleKeydown"
          class="w-full h-40 p-4 font-mono text-sm resize-none focus:outline-none"
          placeholder="Enter SQL query..."
          spellcheck="false"
        ></textarea>
      </div>

      <!-- Results -->
      <div class="flex-1 overflow-auto">
        <div v-if="error" class="p-4 text-red-600 text-sm">{{ error }}</div>
        <div v-else-if="running" class="p-4 text-gray-500 text-sm">Running query...</div>
        <div v-else-if="result" class="flex flex-col h-full">
          <div class="px-4 py-1.5 bg-gray-100 text-xs text-gray-500 border-b border-gray-200">
            {{ result.row_count }} rows &middot; {{ result.duration }}
          </div>
          <div class="flex-1 overflow-auto">
            <table class="w-full text-sm border-collapse">
              <thead class="sticky top-0">
                <tr>
                  <th v-for="col in result.columns" :key="col.name" class="px-3 py-2 bg-gray-100 border-b border-gray-200 text-left font-medium">
                    {{ col.name }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, i) in result.rows" :key="i" class="hover:bg-gray-50">
                  <td v-for="(cell, j) in row" :key="j" class="px-3 py-1.5 border-b border-gray-100">{{ cell ?? '' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div v-else class="p-6 text-gray-400 text-center">Write a query and press Run</div>
      </div>
    </div>

    <!-- Save modal -->
    <div v-if="showSaveModal" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50" @click.self="showSaveModal = false">
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-sm">
        <h3 class="text-lg font-semibold mb-4">Save Query</h3>
        <input v-model="saveTitle" placeholder="Query name" class="w-full px-3 py-2 border rounded-lg mb-4" @keyup.enter="saveQuery" autofocus />
        <div class="flex gap-3 justify-end">
          <button @click="showSaveModal = false" class="px-4 py-2 text-gray-600">Cancel</button>
          <button @click="saveQuery" class="px-4 py-2 bg-blue-600 text-white rounded-lg">Save</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDelete"
      title="Delete Query"
      message="Delete this query?"
      @confirm="confirmDeleteQuery"
      @cancel="showConfirmDelete = false; pendingDeleteId = null"
    />

    <div v-if="errorMessage" class="fixed bottom-4 right-4 bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg text-sm z-50">
      {{ errorMessage }}
    </div>
  </div>
</template>
