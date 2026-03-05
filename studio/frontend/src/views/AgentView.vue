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
    <p class="text-sm text-gray-500 mb-4">Enter a SQL query to analyze your data.</p>

    <div class="mb-4">
      <textarea
        v-model="query"
        @keydown="handleKeydown"
        class="w-full h-32 p-4 border border-gray-300 rounded-lg font-mono text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500"
        placeholder="SELECT * FROM your_table LIMIT 10"
        spellcheck="false"
      ></textarea>
    </div>

    <div class="flex items-center gap-4 mb-6">
      <button @click="sendQuery" :disabled="loading" class="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">
        {{ loading ? 'Running...' : 'Analyze (Ctrl+Enter)' }}
      </button>
      <span class="text-sm text-gray-400">Mode: {{ status.mode || 'unknown' }}</span>
    </div>

    <!-- Results -->
    <div v-if="result">
      <div v-if="result.error" class="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm mb-4">
        {{ result.error }}
      </div>

      <div v-if="result.sql" class="mb-4">
        <div class="text-xs font-medium text-gray-500 mb-1">SQL</div>
        <pre class="p-3 bg-gray-100 rounded-lg text-sm font-mono overflow-x-auto">{{ result.sql }}</pre>
      </div>

      <div v-if="result.analysis" class="mb-4">
        <div class="text-xs font-medium text-gray-500 mb-1">Analysis</div>
        <div class="p-3 bg-gray-50 rounded-lg text-sm">{{ result.analysis }}</div>
      </div>

      <div v-if="result.columns && result.rows" class="overflow-auto">
        <table class="w-full text-sm border-collapse">
          <thead>
            <tr>
              <th v-for="col in result.columns" :key="col" class="px-3 py-2 bg-gray-100 border border-gray-200 text-left font-medium">
                {{ col }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in result.rows" :key="i" class="hover:bg-gray-50">
              <td v-for="(cell, j) in row" :key="j" class="px-3 py-1.5 border border-gray-100">{{ cell ?? '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
