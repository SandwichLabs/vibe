<script setup lang="ts">
import { ref } from 'vue'
import Icon from '../components/Icon.vue'

// --- CSV (legacy single-file import) ---------------------------------------
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

// --- Spreadsheet wizard (P6.4) ---------------------------------------------
interface SpreadsheetColumn {
  name: string
  type: string
}
interface SpreadsheetPreview {
  sheets: string[]
  sheet: string
  columns: SpreadsheetColumn[]
  rows: any[][]
}
interface ColumnMap {
  source: string
  target: string
  type: string
}

const DUCKDB_TYPES = ['VARCHAR', 'BIGINT', 'DOUBLE', 'BOOLEAN', 'DATE', 'TIMESTAMP']

const xlsxPath = ref('')
const sheets = ref<string[]>([])
const selectedSheet = ref('')
const hasHeader = ref(true)
const previewColumns = ref<SpreadsheetColumn[]>([])
const previewRows = ref<any[][]>([])
const mappings = ref<ColumnMap[]>([])
const xlsxTableName = ref('')
const importMode = ref<'create' | 'append'>('create')
const xlsxResult = ref<ImportResult | null>(null)
const xlsxLoading = ref(false)
const xlsxError = ref('')

function deriveTableName(path: string): string {
  const base = path.split('/').pop()?.replace(/\.(xlsx|xls)$/i, '') || ''
  return base.replace(/[^a-zA-Z0-9_]/g, '_').toLowerCase()
}

function applyPreview(p: SpreadsheetPreview) {
  sheets.value = p.sheets || []
  selectedSheet.value = p.sheet || (p.sheets && p.sheets[0]) || ''
  previewColumns.value = p.columns || []
  previewRows.value = p.rows || []
  mappings.value = (p.columns || []).map((c) => ({
    source: c.name,
    target: c.name,
    type: c.type,
  }))
}

async function selectXlsx() {
  xlsxError.value = ''
  try {
    const path = await window.go.main.App.SelectSpreadsheetFile()
    if (!path) return
    xlsxPath.value = path
    xlsxTableName.value = deriveTableName(path)
    selectedSheet.value = ''
    await refreshPreview()
  } catch (e: any) {
    xlsxError.value = e.message || String(e)
  }
}

async function refreshPreview() {
  if (!xlsxPath.value) return
  xlsxError.value = ''
  try {
    const p: SpreadsheetPreview = await window.go.main.App.PreviewSpreadsheet(
      xlsxPath.value,
      selectedSheet.value,
      hasHeader.value,
    )
    applyPreview(p)
  } catch (e: any) {
    xlsxError.value = e.message || String(e)
  }
}

function colTypeFor(source: string): string {
  return mappings.value.find((m) => m.source === source)?.type || 'VARCHAR'
}

function setColType(source: string, type: string) {
  const m = mappings.value.find((mm) => mm.source === source)
  if (m) m.type = type
}

async function importXlsx() {
  if (!xlsxPath.value) return
  xlsxLoading.value = true
  xlsxError.value = ''
  xlsxResult.value = null
  try {
    xlsxResult.value = await window.go.main.App.ImportSpreadsheet({
      path: xlsxPath.value,
      sheet: selectedSheet.value,
      has_header: hasHeader.value,
      table_name: xlsxTableName.value || deriveTableName(xlsxPath.value),
      mode: importMode.value,
      columns: mappings.value.map((m) => ({ source: m.source, target: m.target, type: m.type })),
    })
  } catch (e: any) {
    xlsxError.value = e.message || String(e)
  }
  xlsxLoading.value = false
}
</script>

<template>
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="headrow">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Import</div>
        <h1>Import data</h1>
      </div>
    </div>

    <!-- Spreadsheet wizard -->
    <div class="card card-pad" style="max-width: 60rem; margin-bottom: 24px;">
      <h2 class="card-title" style="margin: 0 0 var(--sh-space-4);">Import spreadsheet</h2>
      <div class="col">
        <div>
          <label class="field-label">Workbook (.xlsx)</label>
          <div class="row">
            <input
              :value="xlsxPath"
              readonly
              class="field-input"
              style="flex: 1; background: var(--sh-paper-2);"
              placeholder="No workbook selected"
            />
            <button data-testid="select-xlsx" @click="selectXlsx" class="btn btn-sm">
              <Icon name="fileText" :size="14" /> Browse
            </button>
          </div>
        </div>

        <div v-if="xlsxPath" class="row" style="gap: 24px; flex-wrap: wrap;">
          <div>
            <label class="field-label">Sheet</label>
            <select
              data-testid="sheet-select"
              v-model="selectedSheet"
              @change="refreshPreview"
              class="field-input"
            >
              <option v-for="s in sheets" :key="s" :value="s">{{ s }}</option>
            </select>
          </div>
          <div>
            <label class="field-label">First row is header</label>
            <input
              data-testid="header-toggle"
              type="checkbox"
              v-model="hasHeader"
              @change="refreshPreview"
            />
          </div>
          <div>
            <label class="field-label">Table name</label>
            <input v-model="xlsxTableName" class="field-input" placeholder="my_table" />
          </div>
          <div>
            <label class="field-label">Mode</label>
            <select data-testid="mode-create" v-model="importMode" class="field-input">
              <option value="create">Create new table</option>
              <option value="append">Append to existing</option>
            </select>
          </div>
        </div>

        <div v-if="previewColumns.length" data-testid="preview-grid" class="card" style="overflow-x: auto;">
          <table style="border-collapse: collapse; width: 100%;">
            <thead>
              <tr>
                <th
                  v-for="c in previewColumns"
                  :key="c.name"
                  style="text-align: left; padding: 8px; border-bottom: 1px solid var(--sh-line);"
                >
                  <div>{{ c.name }}</div>
                  <select
                    :data-testid="`coltype-${c.name}`"
                    :value="colTypeFor(c.name)"
                    @change="setColType(c.name, ($event.target as HTMLSelectElement).value)"
                    class="field-input"
                    style="margin-top: 4px;"
                  >
                    <option v-for="t in DUCKDB_TYPES" :key="t" :value="t">{{ t }}</option>
                  </select>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, ri) in previewRows" :key="ri">
                <td
                  v-for="(cell, ci) in row"
                  :key="ci"
                  style="padding: 8px; border-bottom: 1px solid var(--sh-line);"
                >
                  {{ cell }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <button
          v-if="xlsxPath"
          data-testid="import-btn"
          @click="importXlsx"
          :disabled="xlsxLoading"
          class="btn btn-primary"
          style="justify-content: center;"
        >
          <Icon name="download" :size="16" /> {{ xlsxLoading ? 'Importing…' : 'Import' }}
        </button>

        <div v-if="xlsxError" class="banner attn">
          <div class="ico"><Icon name="alert" :size="22" /></div>
          <div class="body"><div class="ttl">Import failed</div><div class="sub">{{ xlsxError }}</div></div>
        </div>

        <div v-if="xlsxResult" class="banner teal">
          <div class="ico"><Icon name="check" :size="22" /></div>
          <div class="body">
            <div class="ttl">Import complete</div>
            <div class="sub">
              Imported {{ xlsxResult.rows_imported }} rows into table "{{ xlsxResult.table_name }}".
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- CSV import -->
    <div class="card card-pad" style="max-width: 36rem;">
      <h2 class="card-title" style="margin: 0 0 var(--sh-space-4);">Import CSV</h2>
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
