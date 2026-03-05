<script setup lang="ts">
import { ref, onMounted } from 'vue'

const query = ref('')
const result = ref<AgentResult | null>(null)
const status = ref<Record<string, any>>({})
const loading = ref(false)

async function loadStatus() {
  try {
    status.value = await window.go.main.App.GetAgentStatus()
  } catch { /* ignore */ }
}

async function sendQuery() {
  if (!query.value.trim()) return
  loading.value = true
  result.value = null
  try {
    result.value = await window.go.main.App.SendAgentQuery(query.value)
  } catch (e: any) {
    result.value = { error: e.message || String(e) }
  }
  loading.value = false
}

function handleKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault()
    sendQuery()
  }
}

onMounted(loadStatus)
</script>

<template>
  <div class="p-6 max-w-4xl">
    <h1 class="text-2xl font-bold mb-4">Agent</h1>
    <p class="text-sm text-muted mb-4">Enter a SQL query to analyze your data.</p>

    <div class="mb-4">
      <textarea
        v-model="query"
        @keydown="handleKeydown"
        class="field-input h-32 resize-none font-mono"
        placeholder="SELECT * FROM your_table LIMIT 10"
        spellcheck="false"
      ></textarea>
    </div>

    <div class="flex items-center gap-4 mb-6">
      <button @click="sendQuery" :disabled="loading" class="btn btn-primary">
        {{ loading ? 'Running...' : 'Analyze (Ctrl+Enter)' }}
      </button>
      <span class="badge badge-black">Mode: {{ status.mode || 'unknown' }}</span>
    </div>

    <!-- Results -->
    <div v-if="result">
      <div v-if="result.error" class="p-4 bg-error/10 border-3 border-error text-error text-sm font-semibold mb-4">
        {{ result.error }}
      </div>

      <div v-if="result.sql" class="mb-4">
        <div class="field-label">SQL</div>
        <pre class="p-3 bg-black text-yellow text-sm font-mono overflow-x-auto border-3 border-black">{{ result.sql }}</pre>
      </div>

      <div v-if="result.analysis" class="mb-4">
        <div class="field-label">Analysis</div>
        <div class="card p-3 text-sm">{{ result.analysis }}</div>
      </div>

      <div v-if="result.columns && result.rows" class="overflow-auto">
        <table class="brutal-table">
          <thead>
            <tr>
              <th v-for="col in result.columns" :key="col">{{ col }}</th>
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
  </div>
</template>
