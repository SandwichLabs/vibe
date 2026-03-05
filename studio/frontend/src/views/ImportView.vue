<script setup lang="ts">
import { ref } from 'vue'

const csvPath = ref('')
const tableName = ref('')
const result = ref<ImportResult | null>(null)
const loading = ref(false)
const error = ref('')

async function selectFile() {
  try {
    const path = await window.go.main.App.SelectCSVFile()
    if (path) {
      csvPath.value = path
      // Auto-generate table name from filename.
      const name = path.split('/').pop()?.replace(/\.csv$/i, '') || ''
      tableName.value = name.replace(/[^a-zA-Z0-9_]/g, '_').toLowerCase()
    }
  } catch (e: any) {
    error.value = e.message || String(e)
  }
}

async function importCSV() {
  if (!csvPath.value || !tableName.value) return
  loading.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await window.go.main.App.ImportCSV(csvPath.value, tableName.value)
  } catch (e: any) {
    error.value = e.message || String(e)
  }
  loading.value = false
}
</script>

<template>
  <div class="p-6 max-w-xl">
    <h1 class="text-2xl font-bold mb-6">Import</h1>

    <div class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">CSV File</label>
        <div class="flex gap-3">
          <input :value="csvPath" readonly class="flex-1 px-3 py-2 border rounded-lg bg-gray-50 text-sm" placeholder="No file selected" />
          <button @click="selectFile" class="px-4 py-2 bg-gray-200 rounded-lg hover:bg-gray-300 text-sm">Browse</button>
        </div>
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Table Name</label>
        <input v-model="tableName" class="w-full px-3 py-2 border rounded-lg text-sm" placeholder="my_table" />
      </div>

      <button
        @click="importCSV"
        :disabled="loading || !csvPath || !tableName"
        class="w-full py-2 px-4 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50"
      >
        {{ loading ? 'Importing...' : 'Import' }}
      </button>

      <div v-if="error" class="p-3 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">{{ error }}</div>

      <div v-if="result" class="p-3 bg-green-50 border border-green-200 rounded-lg text-green-700 text-sm">
        Imported {{ result.rows_imported }} rows into table "{{ result.table_name }}"
      </div>
    </div>
  </div>
</template>
