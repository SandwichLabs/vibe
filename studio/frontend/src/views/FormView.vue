<script setup lang="ts">
// FormView — Access-style master-detail form (plan §1.5/§1.6).
//
// Translates the Form Builder / master-detail mockup (tmp/design hifi-create.jsx
// "Screen 4: Form Builder") into Vue 3 + the SchoolHouse design system. It binds
// a stored FormSpec (_vibe_forms) to its table, renders a columnar single-record
// header form plus an FK-linked subform datasheet, and drives an Access record-
// navigation bar (first / prev / next / last / new / save / delete + Find Record).
//
// Wiring:
//   ListForms / GetForm     — load form definitions
//   GetTableData            — fetch header records (for nav) and child rows
//   SubmitForm              — atomic header + line write-back
//   DeleteRow               — delete the current header record
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'

// The switchboard (P4.7) binds a nav button to a specific form and navigates
// here with ?id=<formId>. Honor that query so the bound form opens, rather than
// always defaulting to the first form. The route may be absent when mounted
// without a router (unit tests).
const route = useRoute() ?? ({ query: {} } as ReturnType<typeof useRoute>)

const forms = ref<FormSummary[]>([])
const selectedFormId = ref<string | null>(null)
const spec = ref<FormSpec | null>(null)

// Header records loaded for the bound table, plus the cursor into them.
const headerCols = ref<string[]>([])
const headerRows = ref<any[][]>([])
const cursor = ref(0) // index into headerRows; -1 means a new (unsaved) record

// The working copy of the current header record (column -> value).
const headerValues = ref<Record<string, any>>({})
// The working copy of the subform's child rows for the current master.
const childRows = ref<Record<string, any>[]>([])

const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const findText = ref('')
const showConfirmDelete = ref(false)

const isNew = computed(() => cursor.value < 0)
const recordLabel = computed(() => {
  if (isNew.value) return 'New record'
  if (headerRows.value.length === 0) return 'No records'
  return `Record ${cursor.value + 1} of ${headerRows.value.length}`
})

const headerFields = computed(() => spec.value?.fields ?? [])
const subform = computed(() => spec.value?.subform ?? null)
const subformFields = computed(() => subform.value?.fields ?? [])

function colIndex(col: string): number {
  return headerCols.value.indexOf(col)
}

// The primary-key column for nav/delete. We treat the first header column as the
// record key (Access convention: the bound table's PK leads the field list).
const pkCol = computed(() => headerCols.value[0] ?? 'id')

function queryFormId(): string | null {
  const q = route.query.id
  const id = Array.isArray(q) ? q[0] : q
  return id ? String(id) : null
}

async function loadForms() {
  loading.value = true
  try {
    forms.value = await window.go.main.App.ListForms()
    // Prefer a form bound via the switchboard (?id=…); otherwise default to the
    // first form so the view is never empty.
    const wanted = queryFormId()
    if (wanted && forms.value.some((f) => f.id === wanted)) {
      await selectForm(wanted)
    } else if (forms.value.length > 0 && !selectedFormId.value) {
      await selectForm(forms.value[0].id)
    }
  } catch (e: any) {
    errorMessage.value = 'Failed to list forms: ' + (e.message || e)
  }
  loading.value = false
}

async function selectForm(id: string) {
  selectedFormId.value = id
  try {
    spec.value = await window.go.main.App.GetForm(id)
    await loadHeaderRecords()
  } catch (e: any) {
    errorMessage.value = 'Failed to open form: ' + (e.message || e)
  }
}

async function loadHeaderRecords() {
  if (!spec.value) return
  try {
    const data = await window.go.main.App.GetTableData(spec.value.table, 1, 500, '', '')
    headerCols.value = data.columns
    headerRows.value = data.rows
    cursor.value = headerRows.value.length > 0 ? 0 : -1
    await bindCurrentRecord()
  } catch (e: any) {
    errorMessage.value = 'Failed to load records: ' + (e.message || e)
  }
}

// bindCurrentRecord copies the header row under the cursor into headerValues and
// loads the FK-linked child rows for the subform.
async function bindCurrentRecord() {
  const values: Record<string, any> = {}
  if (!isNew.value && headerRows.value[cursor.value]) {
    const row = headerRows.value[cursor.value]
    headerCols.value.forEach((c, i) => { values[c] = row[i] })
  } else {
    // New record: blank the bound fields.
    for (const f of headerFields.value) values[f.name] = ''
  }
  headerValues.value = values
  await loadChildRows()
}

async function loadChildRows() {
  childRows.value = []
  const sf = subform.value
  if (!sf || isNew.value) return
  const masterVal = headerValues.value[sf.parent_key]
  if (masterVal === undefined || masterVal === null) return
  try {
    const data = await window.go.main.App.GetTableData(sf.child_table, 1, 500, '', '')
    const keyIdx = data.columns.indexOf(sf.child_key)
    const rows: Record<string, any>[] = []
    for (const r of data.rows) {
      if (keyIdx >= 0 && String(r[keyIdx]) !== String(masterVal)) continue
      const obj: Record<string, any> = {}
      data.columns.forEach((c, i) => { obj[c] = r[i] })
      rows.push(obj)
    }
    childRows.value = rows
  } catch (e: any) {
    errorMessage.value = 'Failed to load subform rows: ' + (e.message || e)
  }
}

// --- Record navigation ---
async function gotoFirst() { if (headerRows.value.length) { cursor.value = 0; await bindCurrentRecord() } }
async function gotoPrev() { if (cursor.value > 0) { cursor.value--; await bindCurrentRecord() } }
async function gotoNext() { if (cursor.value < headerRows.value.length - 1) { cursor.value++; await bindCurrentRecord() } }
async function gotoLast() { if (headerRows.value.length) { cursor.value = headerRows.value.length - 1; await bindCurrentRecord() } }

async function newRecord() {
  cursor.value = -1
  await bindCurrentRecord()
}

// findRecord jumps to the first header record whose values contain the search
// text (Access "Find Record"). Case-insensitive substring across columns.
async function findRecord() {
  const q = findText.value.trim().toLowerCase()
  if (!q) return
  const idx = headerRows.value.findIndex((row) =>
    row.some((cell) => String(cell ?? '').toLowerCase().includes(q)),
  )
  if (idx >= 0) {
    cursor.value = idx
    await bindCurrentRecord()
  } else {
    errorMessage.value = `No record matches "${findText.value}"`
  }
}

function addChildRow() {
  const obj: Record<string, any> = {}
  for (const f of subformFields.value) obj[f.name] = ''
  childRows.value.push(obj)
}

function removeChildRow(i: number) {
  childRows.value.splice(i, 1)
}

// saveRecord commits the header + all subform child rows in a single atomic
// SubmitForm transaction. The child rows are stamped with the master key so the
// FK link is preserved.
async function saveRecord() {
  if (!spec.value) return
  saving.value = true
  errorMessage.value = ''
  try {
    const parent: FormSubmitRow = { table: spec.value.table, values: { ...headerValues.value } }
    const sf = subform.value
    const children: FormSubmitRow[] = []
    if (sf) {
      const masterVal = headerValues.value[sf.parent_key]
      for (const row of childRows.value) {
        children.push({
          table: sf.child_table,
          values: { ...row, [sf.child_key]: masterVal },
        })
      }
    }
    await window.go.main.App.SubmitForm(parent, children)
    await loadHeaderRecords()
  } catch (e: any) {
    errorMessage.value = 'Save failed: ' + (e.message || e)
  }
  saving.value = false
}

function deleteRecord() {
  if (isNew.value) { newRecord(); return }
  showConfirmDelete.value = true
}

async function confirmDelete() {
  showConfirmDelete.value = false
  if (!spec.value || isNew.value) return
  try {
    const pkVal = headerValues.value[pkCol.value]
    await window.go.main.App.DeleteRow(spec.value.table, pkCol.value, pkVal)
    await loadHeaderRecords()
  } catch (e: any) {
    errorMessage.value = 'Delete failed: ' + (e.message || e)
  }
}

function controlType(t: string): string {
  switch (t) {
    case 'number': case 'currency': return 'number'
    case 'date': return 'date'
    case 'datetime': return 'datetime-local'
    case 'email': return 'email'
    case 'url': return 'url'
    default: return 'text'
  }
}

onMounted(loadForms)

// Re-select when the switchboard navigates to a different bound form while the
// view is already mounted (route.query.id changes without a remount).
watch(() => route.query.id, (q) => {
  const id = Array.isArray(q) ? q[0] : q
  if (id && forms.value.some((f) => f.id === String(id)) && selectedFormId.value !== String(id)) {
    selectForm(String(id))
  }
})

watch(errorMessage, (msg) => { if (msg) setTimeout(() => { errorMessage.value = '' }, 4000) })
</script>

<template>
  <div class="row" style="height: 100%; gap: 0; align-items: stretch;">
    <!-- Form list -->
    <div class="col" style="width: 224px; gap: 0; border-right: var(--sh-border); background: var(--sh-paper-2);">
      <div class="row" style="justify-content: space-between; padding: 12px 16px; border-bottom: var(--sh-border);">
        <h2 class="sh-label" style="margin: 0;">Forms</h2>
      </div>
      <div style="flex: 1; overflow: auto;">
        <div v-if="loading" class="sh-mono" style="padding: 16px; color: var(--sh-graphite);">Loading…</div>
        <div v-else-if="forms.length === 0" class="sh-mono" style="padding: 16px; color: var(--sh-graphite); text-align: center;">No forms yet</div>
        <div
          v-for="f in forms"
          :key="f.id"
          @click="selectForm(f.id)"
          class="row"
          :style="[
            'justify-content: space-between; padding: 8px 16px; cursor: pointer; border-bottom: 1px solid rgba(31,35,48,0.12); font-size: 14px;',
            selectedFormId === f.id ? 'background: var(--sh-teal-8); box-shadow: inset 3px 0 0 var(--sh-teal);' : ''
          ]"
        >
          <div style="min-width: 0;">
            <div style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600;">{{ f.title || f.id }}</div>
            <div class="sh-mono" style="font-size: 10px; color: var(--sh-graphite); text-transform: uppercase; letter-spacing: 0.06em;">{{ f.archetype }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- Form area -->
    <div class="col" style="flex: 1; gap: 0; min-width: 0;">
      <div class="page-subhead" style="padding: 12px 24px;">
        <span class="sh-label" style="margin: 0;">{{ spec?.title || 'Form' }}</span>
        <span class="spacer"></span>
        <span class="sh-mono" style="font-size: 11px; color: var(--sh-graphite);">{{ recordLabel }}</span>
      </div>

      <div v-if="errorMessage" style="padding: 10px 24px; font-size: 14px; font-weight: 600; color: var(--sh-error);">{{ errorMessage }}</div>

      <div v-if="!spec" class="empty-hero">
        <div class="glyph"><Icon name="form" :size="48" /></div>
        <h2>Select a form</h2>
        <p class="lead">Forms bind a table to bound controls and a master-detail subform.</p>
      </div>

      <div v-else style="flex: 1; overflow: auto; min-height: 0; padding: 24px;">
        <div class="card card-pad" style="max-width: 880px;">
          <!-- Columnar header fields -->
          <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
            <div v-for="field in headerFields" :key="field.name" class="col" style="gap: 4px;">
              <label class="sh-label" :for="'fld-' + field.name" style="margin: 0;">{{ field.caption || field.name }}</label>
              <textarea
                v-if="field.control_type === 'textarea'"
                :id="'fld-' + field.name"
                v-model="headerValues[field.name]"
                rows="3"
                style="width: 100%; resize: vertical; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-family: inherit; font-size: 14px;"
              ></textarea>
              <input
                v-else
                :id="'fld-' + field.name"
                v-model="headerValues[field.name]"
                :type="controlType(field.control_type)"
                style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-size: 14px;"
              />
            </div>
          </div>

          <!-- Master-detail subform -->
          <template v-if="subform">
            <div class="row" style="justify-content: space-between; align-items: center; margin-top: 22px;">
              <h3 class="card-title" style="margin: 0;">{{ subform.child_table }} · subform</h3>
              <button @click="addChildRow" :disabled="isNew" class="btn btn-sm"><Icon name="plus" :size="14" /> Add row</button>
            </div>
            <table class="table" style="margin-top: 8px;">
              <thead>
                <tr>
                  <th v-for="sff in subformFields" :key="sff.name">{{ sff.caption || sff.name }}</th>
                  <th style="width: 40px;"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="childRows.length === 0">
                  <td :colspan="subformFields.length + 1" class="sh-mono" style="color: var(--sh-graphite); text-align: center;">No line items</td>
                </tr>
                <tr v-for="(row, i) in childRows" :key="i">
                  <td v-for="sff in subformFields" :key="sff.name">
                    <input
                      v-model="row[sff.name]"
                      :type="controlType(sff.control_type)"
                      style="width: 100%; padding: 4px 6px; border: 1px solid rgba(31,35,48,0.18); border-radius: 4px; font-size: 13px;"
                    />
                  </td>
                  <td>
                    <button @click="removeChildRow(i)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
                  </td>
                </tr>
              </tbody>
            </table>
          </template>

          <!-- Record-navigation bar (first/prev/next/last + new/save/delete + Find Record) -->
          <div class="vibe-record-nav row" role="toolbar" aria-label="Record navigation" style="margin-top: 18px; gap: 6px; align-items: center;">
            <button @click="gotoFirst" :disabled="isNew || cursor <= 0" class="btn btn-sm" title="First record" aria-label="First record">|&lt;</button>
            <button @click="gotoPrev" :disabled="isNew || cursor <= 0" class="btn btn-sm" title="Previous record" aria-label="Previous record"><Icon name="arrowLeft" :size="14" /></button>
            <button @click="gotoNext" :disabled="isNew || cursor >= headerRows.length - 1" class="btn btn-sm" title="Next record" aria-label="Next record"><Icon name="arrowRight" :size="14" /></button>
            <button @click="gotoLast" :disabled="isNew || cursor >= headerRows.length - 1" class="btn btn-sm" title="Last record" aria-label="Last record">&gt;|</button>

            <span style="width: 12px;"></span>
            <button @click="newRecord" class="btn btn-sm" title="New record"><Icon name="plus" :size="14" /> New</button>
            <button @click="saveRecord" :disabled="saving" class="btn btn-primary btn-sm" title="Save record"><Icon name="save" :size="14" /> {{ saving ? 'Saving…' : 'Save' }}</button>
            <button @click="deleteRecord" class="btn btn-sm" style="color: var(--sh-error);" title="Delete record"><Icon name="trash" :size="14" /> Delete</button>

            <span class="spacer"></span>
            <input
              v-model="findText"
              type="search"
              placeholder="Find Record"
              aria-label="Find Record"
              @keydown.enter="findRecord"
              style="width: 160px; padding: 6px 10px; border: var(--sh-border); border-radius: 6px; font-size: 13px;"
            />
            <button @click="findRecord" class="btn btn-sm" title="Find Record"><Icon name="filter" :size="14" /> Find</button>
          </div>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDelete"
      title="Delete record?"
      message="This permanently deletes the current record. This cannot be undone."
      @confirm="confirmDelete"
      @cancel="showConfirmDelete = false"
    />
  </div>
</template>
