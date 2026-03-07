<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'

const queries = ref<QuerySummary[]>([])
const selectedId = ref<string | null>(null)
const sqlText = ref('')
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
    result.value = await window.go.main.App.ExecuteQuery(sqlText.value)
  } catch (e: any) {
    error.value = e.message || String(e)
  }
  running.value = false
}

async function saveQuery() {
  if (!saveTitle.value.trim()) return
  try {
    await window.go.main.App.SaveQuery(selectedId.value || '', saveTitle.value, sqlText.value, '')
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
    <div class="w-56 border-r-3 border-black bg-white flex flex-col">
      <div class="p-3 border-b-3 border-black flex items-center justify-between">
        <h2 class="font-bold text-sm uppercase font-mono tracking-wide">Queries</h2>
        <button @click="newQuery" class="btn btn-primary btn-sm">+ New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 text-muted font-mono text-sm">Loading...</div>
        <div v-else-if="queries.length === 0" class="p-4 text-muted text-sm text-center font-mono">No saved queries</div>
        <div
          v-for="q in queries"
          :key="q.id"
          @click="selectQuery(q.id)"
          class="px-3 py-2 cursor-pointer border-b-2 border-black/10 flex items-center justify-between group text-sm"
          :class="selectedId === q.id ? 'bg-yellow border-l-4 border-l-black' : 'hover:bg-cream'"
        >
          <div class="min-w-0">
            <div class="truncate font-bold">{{ q.title }}</div>
          </div>
          <button @click.stop="deleteQuery(q.id)" class="hidden group-hover:block btn btn-ghost btn-sm text-error">Del</button>
        </div>
      </div>
    </div>

    <!-- Editor area -->
    <div class="flex-1 flex flex-col">
      <!-- Toolbar -->
      <div class="px-4 py-2 border-b-3 border-black bg-white flex items-center gap-3">
        <button @click="runQuery" :disabled="running" class="btn btn-primary">
          {{ running ? 'Running...' : 'Run (Ctrl+Enter)' }}
        </button>
        <button @click="showSaveModal = true" class="btn">Save Query</button>
      </div>

      <!-- SQL editor -->
      <div class="border-b-3 border-black">
        <textarea
          v-model="sqlText"
          @keydown="handleKeydown"
          class="w-full h-40 p-4 font-mono text-sm resize-none focus:outline-none bg-white"
          placeholder="Enter SQL query..."
          spellcheck="false"
        ></textarea>
      </div>

      <!-- Results -->
      <div class="flex-1 overflow-auto">
        <div v-if="error" class="p-4 text-error text-sm font-semibold">{{ error }}</div>
        <div v-else-if="running" class="p-4 text-muted font-mono text-sm">Running query...</div>
        <div v-else-if="result" class="flex flex-col h-full">
          <div class="px-4 py-1.5 bg-yellow text-xs font-bold font-mono uppercase tracking-wide border-b-3 border-black">
            {{ result.row_count }} rows &middot; {{ result.duration }}
          </div>
          <div class="flex-1 overflow-auto">
            <table class="brutal-table">
              <thead class="sticky top-0">
                <tr>
                  <th v-for="col in result.columns" :key="col.name">{{ col.name }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, i) in result.rows" :key="i">
                  <td v-for="(cell, j) in row" :key="j">{{ cell ?? '' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div v-else class="p-6 text-muted text-center font-mono">Write a query and press Run</div>
      </div>
    </div>

    <!-- Save modal -->
    <div v-if="showSaveModal" class="modal-overlay" @click.self="showSaveModal = false">
      <div class="card w-full max-w-sm p-6">
        <h3 class="text-lg font-bold mb-4">Save Query</h3>
        <input v-model="saveTitle" placeholder="Query name" class="field-input mb-4" @keyup.enter="saveQuery" autofocus />
        <div class="flex gap-3 justify-end">
          <button @click="showSaveModal = false" class="btn btn-ghost">Cancel</button>
          <button @click="saveQuery" class="btn btn-primary">Save</button>
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

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
  </div>
</template>
