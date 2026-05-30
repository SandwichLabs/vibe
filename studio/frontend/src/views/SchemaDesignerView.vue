<script setup lang="ts">
// P2.9 — SchemaDesignerView.
// The field-property editor that completes §2.2: it lists tables, shows each
// table's columns + Access-style field metadata, lets the builder edit a
// field's properties (caption / required / control type / default / tip /
// validation) and persist them via SetFieldMeta, and edits the column
// structure (add column, drop column, add index) via the schema-designer DDL
// bindings. SchoolHouse design system; no React.
import { ref, onMounted } from 'vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'

const COLUMN_TYPES = ['TEXT', 'VARCHAR', 'INTEGER', 'BIGINT', 'REAL', 'DOUBLE', 'DECIMAL', 'BOOLEAN', 'DATE', 'TIMESTAMP', 'TIME', 'BLOB']
const CONTROL_TYPES = ['textbox', 'combo', 'checkbox', 'date']

const tables = ref<string[]>([])
const selectedTable = ref<string | null>(null)
const tableInfo = ref<TableInfo | null>(null)
const fieldMeta = ref<Record<string, FieldMeta>>({})
const loading = ref(true)
const errorMessage = ref('')

// The column currently selected for property editing, plus a working copy of
// its meta so edits are not committed until Save.
const selectedColumn = ref<string | null>(null)
const editMeta = ref<FieldMeta | null>(null)
const saving = ref(false)

// Add-column inline form.
const showAddCol = ref(false)
const addColName = ref('')
const addColType = ref('VARCHAR')

// Drop-column confirmation.
const showConfirmDrop = ref(false)
const pendingDropCol = ref<string | null>(null)

function emptyMeta(table: string, column: string): FieldMeta {
  return {
    table_name: table,
    column_name: column,
    caption: '',
    control_type: 'textbox',
    default_value: '',
    required: false,
    format_code: '',
    validation_rule: '',
    validation_message: '',
    input_mask: '',
    lookup_sql: '',
    display_col: '',
    value_col: '',
    control_tip: '',
    sort_order: 0,
  }
}

async function loadTables() {
  loading.value = true
  try {
    tables.value = (await window.go.main.App.ListTables()) || []
  } catch { /* ignore */ }
  loading.value = false
}

async function selectTable(name: string) {
  selectedTable.value = name
  selectedColumn.value = null
  editMeta.value = null
  showAddCol.value = false
  try {
    tableInfo.value = await window.go.main.App.DescribeTable(name)
  } catch { tableInfo.value = null }
  await loadFieldMeta(name)
}

async function loadFieldMeta(table: string) {
  fieldMeta.value = {}
  let metas: FieldMeta[] = []
  try {
    metas = (await window.go.main.App.GetFieldMeta(table)) || []
  } catch { metas = [] }
  const map: Record<string, FieldMeta> = {}
  for (const m of metas) map[m.column_name] = m
  fieldMeta.value = map
}

function selectField(column: string) {
  if (!selectedTable.value) return
  selectedColumn.value = column
  const existing = fieldMeta.value[column]
  // Work on a copy so cancelling/navigating away discards uncommitted edits.
  editMeta.value = existing
    ? { ...existing }
    : emptyMeta(selectedTable.value, column)
}

async function saveField() {
  if (!selectedTable.value || !selectedColumn.value || !editMeta.value) return
  saving.value = true
  try {
    await window.go.main.App.SetFieldMeta(selectedTable.value, selectedColumn.value, editMeta.value)
    fieldMeta.value[selectedColumn.value] = { ...editMeta.value }
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + (e.message || e)
  } finally {
    saving.value = false
  }
}

function openAddCol() {
  addColName.value = ''
  addColType.value = 'VARCHAR'
  showAddCol.value = true
}

async function confirmAddCol() {
  if (!selectedTable.value || !addColName.value.trim()) return
  try {
    await window.go.main.App.AddColumn(selectedTable.value, addColName.value.trim(), addColType.value)
    showAddCol.value = false
    await selectTable(selectedTable.value)
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + (e.message || e)
  }
}

function dropColumn(column: string) {
  pendingDropCol.value = column
  showConfirmDrop.value = true
}

async function confirmDropColumn() {
  const col = pendingDropCol.value
  showConfirmDrop.value = false
  pendingDropCol.value = null
  if (!col || !selectedTable.value) return
  try {
    await window.go.main.App.DropColumn(selectedTable.value, col)
    if (selectedColumn.value === col) {
      selectedColumn.value = null
      editMeta.value = null
    }
    await selectTable(selectedTable.value)
  } catch (e: any) {
    errorMessage.value = 'Failed: ' + (e.message || e)
  }
}

onMounted(loadTables)
</script>

<template>
  <div class="flex h-full">
    <!-- Table list -->
    <div class="w-56 border-r-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex flex-col">
      <div class="px-4 py-3 border-b-2 border-[var(--sh-ink)]">
        <h2 class="sh-label" style="margin: 0;">Tables</h2>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 sh-mono text-[var(--sh-graphite)]">Loading…</div>
        <div v-else-if="tables.length === 0" class="p-4 sh-mono text-[var(--sh-graphite)] text-center">No tables</div>
        <div
          v-for="t in tables"
          :key="t"
          @click="selectTable(t)"
          class="px-4 py-2 cursor-pointer border-b border-[rgba(31,35,48,0.12)] flex items-center gap-2 text-sm font-medium"
          :class="selectedTable === t ? 'row-selected' : 'hover:bg-[rgba(31,35,48,0.04)]'"
        >
          <Icon name="database" :size="14" /> <span class="truncate">{{ t }}</span>
        </div>
      </div>
    </div>

    <!-- Fields list -->
    <div class="flex-1 overflow-auto flex flex-col border-r-2 border-[var(--sh-ink)]">
      <div v-if="!selectedTable" class="flex-1 flex items-center justify-center text-[var(--sh-graphite)] sh-mono">
        Select a table to design its schema
      </div>
      <template v-else>
        <div class="page-subhead" style="padding: 14px 24px;">
          <h1 style="font-size: 22px;">{{ selectedTable }}</h1>
          <span class="flex-1"></span>
          <button @click="openAddCol" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> Add column</button>
        </div>

        <div v-if="showAddCol" class="px-6 py-3 border-b-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex items-end gap-2">
          <div class="flex-1">
            <label class="field-label">Name</label>
            <input v-model="addColName" class="field-input addcol-name" placeholder="column_name" />
          </div>
          <div>
            <label class="field-label">Type</label>
            <select v-model="addColType" class="field-input addcol-type w-32">
              <option v-for="t in COLUMN_TYPES" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <button @click="confirmAddCol" class="btn btn-primary">Add</button>
          <button @click="showAddCol = false" class="btn btn-ghost">Cancel</button>
        </div>

        <div class="flex-1 overflow-auto">
          <table class="brutal-table">
            <thead class="sticky top-0">
              <tr>
                <th>Field</th>
                <th>Type</th>
                <th>Control</th>
                <th>Required</th>
                <th class="w-12"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="col in (tableInfo?.columns || [])"
                :key="col.name"
                data-field-row
                @click="selectField(col.name)"
                class="cursor-pointer"
                :class="selectedColumn === col.name ? 'row-selected' : 'hover:bg-[rgba(31,35,48,0.04)]'"
              >
                <td class="font-medium flex items-center gap-2">
                  <Icon v-if="col.pk" name="key" :size="13" /> {{ col.name }}
                </td>
                <td class="sh-mono text-xs">{{ col.type }}</td>
                <td class="sh-mono text-xs">{{ fieldMeta[col.name]?.control_type || 'textbox' }}</td>
                <td>
                  <input type="checkbox" :checked="fieldMeta[col.name]?.required || col.not_null" disabled style="accent-color: var(--sh-teal);" />
                </td>
                <td>
                  <button @click.stop="dropColumn(col.name)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="13" /></button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>

    <!-- Field property editor -->
    <div class="w-80 overflow-auto bg-[var(--sh-paper-2)] flex flex-col">
      <div class="px-4 py-3 border-b-2 border-[var(--sh-ink)]">
        <h2 class="sh-label" style="margin: 0;">Field properties</h2>
      </div>
      <div v-if="!editMeta" class="flex-1 flex items-center justify-center p-4 text-center text-[var(--sh-graphite)] sh-mono text-sm">
        Select a field to edit its properties
      </div>
      <div v-else class="p-4 space-y-4">
        <div>
          <label class="field-label">Field</label>
          <div class="sh-mono font-medium">{{ selectedColumn }}</div>
        </div>
        <div>
          <label class="field-label">Caption</label>
          <input v-model="editMeta.caption" class="field-input fp-caption" />
        </div>
        <div>
          <label class="field-label">Control type</label>
          <select v-model="editMeta.control_type" class="field-input fp-control">
            <option v-for="ct in CONTROL_TYPES" :key="ct" :value="ct">{{ ct }}</option>
          </select>
        </div>
        <div>
          <label class="field-label">Default value</label>
          <input v-model="editMeta.default_value" class="field-input fp-default" />
        </div>
        <label class="flex items-center gap-2 text-sm font-medium">
          <input type="checkbox" v-model="editMeta.required" class="fp-required" style="accent-color: var(--sh-teal);" />
          Required
        </label>
        <div>
          <label class="field-label">Control tip</label>
          <input v-model="editMeta.control_tip" class="field-input fp-tip" />
        </div>
        <div>
          <label class="field-label">Validation rule</label>
          <input v-model="editMeta.validation_rule" class="field-input fp-validation" />
        </div>
        <div>
          <label class="field-label">Validation message</label>
          <input v-model="editMeta.validation_message" class="field-input fp-validation-msg" />
        </div>
        <div class="flex justify-end pt-2">
          <button @click="saveField" :disabled="saving" class="btn btn-primary">Save</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDrop"
      title="Drop Column"
      :message="`Drop column '${pendingDropCol}'? This cannot be undone.`"
      @confirm="confirmDropColumn"
      @cancel="showConfirmDrop = false; pendingDropCol = null"
    />

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>
