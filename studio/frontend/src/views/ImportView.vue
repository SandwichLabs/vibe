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
        <label class="field-label">CSV File</label>
        <div class="flex gap-3">
          <input :value="csvPath" readonly class="field-input flex-1 bg-cream" placeholder="No file selected" />
          <button @click="selectFile" class="btn btn-sm">Browse</button>
        </div>
      </div>

      <div>
        <label class="field-label">Table Name</label>
        <input v-model="tableName" class="field-input" placeholder="my_table" />
      </div>

      <button
        @click="importCSV"
        :disabled="loading || !csvPath || !tableName"
        class="btn btn-primary w-full text-center"
      >
        {{ loading ? 'Importing...' : 'Import' }}
      </button>

      <div v-if="error" class="p-3 bg-error/10 border-3 border-error text-error text-sm font-semibold">{{ error }}</div>

      <div v-if="result" class="p-3 bg-yellow/20 border-3 border-black text-sm font-semibold">
        Imported {{ result.rows_imported }} rows into table "{{ result.table_name }}"
      </div>
    </div>
  </div>
</template>
