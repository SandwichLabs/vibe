<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'

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
    <div class="w-56 border-r-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex flex-col">
      <div class="px-4 py-3 border-b-2 border-[var(--sh-ink)] flex items-center justify-between">
        <h2 class="sh-label" style="margin: 0;">Queries</h2>
        <button @click="newQuery" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 sh-mono text-[var(--sh-graphite)]">Loading…</div>
        <div v-else-if="queries.length === 0" class="p-4 sh-mono text-[var(--sh-graphite)] text-center">No saved queries</div>
        <div
          v-for="q in queries"
          :key="q.id"
          @click="selectQuery(q.id)"
          class="px-4 py-2 cursor-pointer border-b border-[rgba(31,35,48,0.12)] flex items-center justify-between group text-sm"
          :class="selectedId === q.id ? 'row-selected' : 'hover:bg-[rgba(31,35,48,0.04)]'"
        >
          <div class="min-w-0">
            <div class="truncate font-semibold">{{ q.title }}</div>
          </div>
          <button @click.stop="deleteQuery(q.id)" class="hidden group-hover:block btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
        </div>
      </div>
    </div>

    <!-- Editor area -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Toolbar -->
      <div class="page-subhead" style="padding: 12px 24px;">
        <span class="sh-label" style="margin: 0;">Query Explorer</span>
        <span class="flex-1"></span>
        <button @click="showSaveModal = true" class="btn btn-sm"><Icon name="save" :size="14" /> Save</button>
        <button @click="runQuery" :disabled="running" class="btn btn-primary btn-sm">
          <Icon name="play" :size="14" /> {{ running ? 'Running…' : 'Run · Ctrl+Enter' }}
        </button>
      </div>

      <!-- SQL editor -->
      <div class="border-b-2 border-[var(--sh-ink)] editor">
        <textarea
          v-model="sqlText"
          @keydown="handleKeydown"
          class="w-full h-40 resize-none focus:outline-none"
          style="background: var(--sh-ink); color: var(--sh-paper); font-family: var(--sh-font-mono); font-size: 13px; line-height: 1.8; padding: 18px;"
          placeholder="Enter SQL query…"
          spellcheck="false"
        ></textarea>
      </div>

      <!-- Results -->
      <div class="flex-1 overflow-auto min-h-0">
        <div v-if="error" class="p-4 text-sm font-semibold" style="color: var(--sh-error);">{{ error }}</div>
        <div v-else-if="running" class="p-4 sh-mono text-[var(--sh-graphite)]">Running query…</div>
        <div v-else-if="result" class="flex flex-col h-full">
          <div class="result-bar">
            <span><span class="big ok">{{ result.row_count }}</span> rows</span>
            <span><span class="big">{{ result.duration }}</span></span>
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
        <div v-else class="empty-hero">
          <div class="glyph"><Icon name="terminal" :size="48" /></div>
          <h2>Write a query, press Run</h2>
          <p class="lead">Use <code>[engine=duckdb]</code> for analytics or <code>[engine=sqlite]</code> for CRUD.</p>
        </div>
      </div>
    </div>

    <!-- Save modal -->
    <div v-if="showSaveModal" class="modal-overlay" @click.self="showSaveModal = false">
      <div class="card card-pad w-full max-w-sm">
        <h3 class="card-title mb-4">Save query</h3>
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
