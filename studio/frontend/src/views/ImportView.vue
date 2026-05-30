<script setup lang="ts">
import { ref } from 'vue'
import Icon from '../components/Icon.vue'

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
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="headrow">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Import</div>
        <h1>Import CSV</h1>
      </div>
    </div>

    <div class="card card-pad" style="max-width: 36rem;">
      <div class="col">
        <div>
          <label class="field-label">CSV file</label>
          <div class="row">
            <input :value="csvPath" readonly class="field-input" style="flex: 1; background: var(--sh-paper-2);" placeholder="No file selected" />
            <button @click="selectFile" class="btn btn-sm"><Icon name="fileText" :size="14" /> Browse</button>
          </div>
        </div>

        <div>
          <label class="field-label">Table name</label>
          <input v-model="tableName" class="field-input" placeholder="my_table" />
        </div>

        <button
          @click="importCSV"
          :disabled="loading || !csvPath || !tableName"
          class="btn btn-primary"
          style="width: 100%; justify-content: center;"
        >
          <Icon name="download" :size="16" /> {{ loading ? 'Importing…' : 'Import' }}
        </button>

        <div v-if="error" class="banner attn">
          <div class="ico"><Icon name="alert" :size="22" /></div>
          <div class="body"><div class="ttl">Import failed</div><div class="sub">{{ error }}</div></div>
        </div>

        <div v-if="result" class="banner teal">
          <div class="ico"><Icon name="check" :size="22" /></div>
          <div class="body"><div class="ttl">Import complete</div><div class="sub">Imported {{ result.rows_imported }} rows into table "{{ result.table_name }}".</div></div>
        </div>
      </div>
    </div>
  </div>
</template>
