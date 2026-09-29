<script setup lang="ts">
// The Import wizard (#82): one card that covers CSV and xlsx. Pick a file,
// review the full-file type inference and suggested primary key, and either
// create a new table or choose how to reconcile with one that already
// exists (update matching rows, replace, add rows, or start a new table).
// Every option change re-runs a debounced dry run so the summary line and
// bad-value list always describe what Import is about to do before it does
// it.
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import { errorText } from '../lib/errors'
import { IMPORT_TYPES, KEY_AUTO, KEY_NONE, typeLabel, deriveTableName, nextFreeName, summarySentence } from '../lib/importTypes'

const router = useRouter()

const path = ref('')
const sheet = ref('')
const hasHeader = ref(true)
const tableName = ref('')
const mode = ref<ImportMode | ''>('')
const key = ref<string>(KEY_AUTO)
const typeOverrides = ref<Record<string, ImportColType>>({})

const tables = ref<string[]>([])
const inspection = ref<ImportInspection | null>(null)
const summary = ref<ImportSummary | null>(null)
const successSummary = ref<ImportSummary | null>(null)

const inspecting = ref(false)
const dryRunning = ref(false)
const importing = ref(false)
const error = ref('')
// Distinguishes a failure while previewing (file pick, sheet/header change,
// or a dry run) from an actual failed Import click, so the error banner's
// title doesn't claim "Import failed" before the user has clicked Import.
const errorContext = ref<'preview' | 'import'>('preview')

// #91 R-F2 guard: disable Import once these exact options have already been
// imported, until something changes.
const importedKey = ref('')

// Lower-cased compare: DuckDB's own quoted-identifier catalog lookup is
// case-insensitive (a table stored as "Customers" IS the table named by
// "customers"), so a case-sensitive `includes` here disagreed with the
// backend — the wizard showed no mode group, sent mode "" (create), and
// got stuck on a Blocked banner ("pass mode upsert|replace|append") for a
// table that, from the backend's point of view, plainly already existed.
const tableExists = computed(
  () => !!tableName.value && tables.value.some((t) => t.toLowerCase() === tableName.value.toLowerCase()),
)
const keyCandidates = computed(() => inspection.value?.columns.filter((c) => c.candidate) ?? [])
// The key picker only makes sense when a schema is being decided: a brand
// new table, or a replace (which recreates the schema from scratch).
const showKeySelect = computed(() => !tableExists.value || mode.value === 'replace')

const MODE_LABELS: Record<'upsert' | 'replace' | 'append', string> = {
  upsert: 'Update matching rows',
  replace: 'Replace table',
  append: 'Add rows',
}
const MODE_DISABLED_REASON: Record<'upsert' | 'replace' | 'append', string> = {
  upsert: 'Needs a primary key in both the table and the file.',
  // Worded direction-neutral: the table can be blocked by a foreign key it
  // holds itself (outgoing) or one another table holds against it
  // (incoming) — "this table's foreign keys" would be wrong for the
  // incoming case.
  replace: 'A foreign key links this table to another — replacing would break it. Use Update matching rows instead.',
  append: 'Not available for this table.',
}
const MODE_ORDER: ('upsert' | 'replace' | 'append')[] = ['upsert', 'replace', 'append']

const mismatchText = computed(() => {
  const parts: string[] = []
  if (summary.value?.missing?.length) parts.push(`The file is missing: ${summary.value.missing.join(', ')}.`)
  if (summary.value?.extra?.length) parts.push(`The file has extra: ${summary.value.extra.join(', ')}.`)
  return parts.join(' ')
})

// An append with no primary key still succeeds (ok:true) even when some
// file rows already match a table row exactly — it's worth calling out
// (attn styling, same banner as a blocker), not a silent plain-meta line.
const hasExactDupeWarning = computed(() =>
  !!summary.value && summary.value.mode === 'append' && summary.value.exact_dupes > 0,
)

// True while the table already exists and no mode has been chosen yet —
// the mode group is showing (it needs summary.existing_rows/modes, which
// the dry run still fetches with mode:"" — see requestKey's watcher), but
// there's nothing to preview until the user picks one. A mode:"" dry run
// gets back the backend's own Blocked sentence ('table "x" already exists
// (N rows); pass mode replace|append') — CLI wording never meant for this
// banner — alongside summarySentence's unrelated, contradictory "Will
// import N rows" (summarySentence has no case for mode "", so it fell
// through to its create-mode phrasing). Both are suppressed for this case
// (hasProblems below, and the template's mode-prompt taking priority over
// summary-line/problem-banner) in favor of one neutral prompt.
const modeNotChosen = computed(() => !!path.value && tableExists.value && !mode.value)

const hasProblems = computed(() => {
  const s = summary.value
  if (!s || modeNotChosen.value) return false
  return !!s.blocked || !!(s.bad_values && s.bad_values.length) || !!mismatchText.value || hasExactDupeWarning.value
})

const canImport = computed(() =>
  !!path.value &&
  !!tableName.value &&
  !inspecting.value &&
  !dryRunning.value &&
  !importing.value &&
  !!summary.value?.ok &&
  requestKey.value !== importedKey.value,
)

function isKeyColumn(name: string): boolean {
  return showKeySelect.value ? key.value === name : summary.value?.table_key === name
}

function typeSelectDisabled(): boolean {
  return tableExists.value && (mode.value === 'append' || mode.value === 'upsert')
}

function setColumnType(name: string, t: ImportFriendlyType) {
  const current = typeOverrides.value[name]
  typeOverrides.value = {
    ...typeOverrides.value,
    [name]: { type: t, scale: t === 'decimal' ? (current?.type === 'decimal' ? current.scale : 2) : undefined },
  }
}

function buildOptions(dryRun: boolean): ImportOptions {
  return {
    path: path.value,
    sheet: sheet.value,
    has_header: hasHeader.value,
    table: tableName.value,
    mode: mode.value,
    key: key.value,
    types: typeOverrides.value,
    dry_run: dryRun,
  }
}

async function loadTables() {
  try {
    tables.value = (await window.go.main.App.ListTables()) || []
  } catch { /* leave the existing list; the dry run will still be accurate */ }
}

function resetAll() {
  path.value = ''
  sheet.value = ''
  hasHeader.value = true
  tableName.value = ''
  mode.value = ''
  key.value = KEY_AUTO
  typeOverrides.value = {}
  inspection.value = null
  summary.value = null
  successSummary.value = null
  importedKey.value = ''
  error.value = ''
}

async function selectFile() {
  error.value = ''
  errorContext.value = 'preview'
  try {
    const p = await window.go.main.App.SelectImportFile()
    if (!p) return
    resetAll()
    path.value = p
    tableName.value = deriveTableName(p)
    await loadTables()
    await inspect()
  } catch (e: any) {
    error.value = errorText(e)
  }
}

async function inspect() {
  if (!path.value) return
  inspecting.value = true
  error.value = ''
  errorContext.value = 'preview'
  try {
    const insp = await window.go.main.App.InspectImport({
      path: path.value,
      sheet: sheet.value,
      has_header: hasHeader.value,
    })
    inspection.value = insp
    if (insp.sheet && !sheet.value) sheet.value = insp.sheet
    key.value = insp.suggested_key || KEY_AUTO
    typeOverrides.value = Object.fromEntries(insp.columns.map((c) => [c.name, { ...c.inferred }]))
  } catch (e: any) {
    error.value = errorText(e)
    inspection.value = null
  }
  inspecting.value = false
}

function chooseNewTable() {
  tableName.value = nextFreeName(tableName.value, tables.value)
}

// pickDefaultMode chooses the wizard's pre-selected mode from what the
// backend reports as usable: upsert (idempotent, never duplicates) — and
// ONLY upsert. Every other case (the file has no id column, so upsert isn't
// offered — exactly the shape of a re-imported people.csv) returns '', so
// no radio is pre-checked and the user must choose explicitly. Append used
// to be the second-choice default, but Append is only idempotent when the
// table's key IS in the file (upsert's own precondition); without a key to
// compare against, a re-import of the same file duplicates every row
// silently — the append pre-flight's exact-duplicate check now catches and
// warns about that, but a SILENT default that reproduces it every time is
// the bug, not the warning. Replace is never auto-picked either, since it
// drops the table's current rows.
function pickDefaultMode(modes: ImportMode[]): ImportMode | '' {
  return modes.includes('upsert') ? 'upsert' : ''
}

// requestKey identifies the exact set of options a dry run (or Import)
// would run with. It drives the debounced dry run below and, unchanged,
// the #91 re-click guard after a successful import.
const requestKey = computed(() => JSON.stringify({
  path: path.value,
  sheet: sheet.value,
  has_header: hasHeader.value,
  table: tableName.value,
  exists: tableExists.value, // a dry run's meaning changes the instant the table starts/stops existing
  mode: mode.value,
  key: key.value,
  types: typeOverrides.value,
}))

let dryRunTimer: ReturnType<typeof setTimeout> | null = null
let dryRunSeq = 0

async function runDryRun() {
  const seq = ++dryRunSeq
  dryRunning.value = true
  try {
    const s = await window.go.main.App.ImportFile(buildOptions(true))
    if (seq !== dryRunSeq) return // a newer dry run has already started; ignore this stale response
    summary.value = s
    // The wizard always pre-selects the safest available mode so the
    // default never duplicates rows AND never drops the table (Replace is
    // only ever chosen explicitly — see pickDefaultMode).
    if (s.exists && s.modes?.length && !s.modes.includes(mode.value as ImportMode)) {
      mode.value = pickDefaultMode(s.modes)
    }
  } catch (e: any) {
    if (seq !== dryRunSeq) return
    errorContext.value = 'preview'
    error.value = errorText(e)
    summary.value = null
  } finally {
    if (seq === dryRunSeq) dryRunning.value = false
  }
}

watch(requestKey, () => {
  summary.value = null
  if (dryRunTimer) clearTimeout(dryRunTimer)
  if (!path.value || !tableName.value) return
  // Still dry-runs with mode:"" when the table exists and none is chosen
  // yet — that response is what carries existing_rows/modes for the mode
  // group itself (Exists/ExistingRows/Modes are set before the backend's
  // own Blocked check). Only the DISPLAY of that response's Blocked
  // sentence and summary line are suppressed while modeNotChosen — see
  // hasProblems and the template below.
  dryRunTimer = setTimeout(runDryRun, 300)
})

// Whenever the table name starts or stops matching an existing table, the
// previously selected mode is no longer meaningful — let the next dry run
// pick a fresh default.
watch(tableExists, () => { mode.value = '' })

async function runImport() {
  if (!canImport.value) return
  importing.value = true
  error.value = ''
  errorContext.value = 'import'
  const key0 = requestKey.value
  try {
    const s = await window.go.main.App.ImportFile(buildOptions(false))
    summary.value = s
    if (s.ok) {
      successSummary.value = s
      importedKey.value = key0
      await loadTables()
    } else {
      error.value = s.blocked || 'Import could not be completed; see the details below.'
    }
  } catch (e: any) {
    error.value = errorText(e)
  }
  importing.value = false
}

function openTable() {
  router.push({ path: '/tables', query: { table: successSummary.value?.table ?? tableName.value } })
}

function importAnother() {
  resetAll()
}

onMounted(loadTables)
</script>

<template>
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="headrow">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Import</div>
        <h1>Import data</h1>
      </div>
    </div>

    <div class="card card-pad" style="max-width: 60rem;">
      <h2 class="card-title" style="margin: 0 0 var(--sh-space-4);">Import a file</h2>
      <div class="col">
        <!-- Once an import has succeeded, the success banner below is the
             whole story: showing the wizard underneath it would keep
             re-running dry runs against a table that just changed shape
             (and briefly render the previous, now-stale summary). -->
        <template v-if="!successSummary">
        <div>
          <label class="field-label">File (.csv, .tsv, .xlsx)</label>
          <div class="row">
            <span
              v-if="path"
              style="flex: 1; font-weight: 600;"
              :title="path"
              data-testid="file-name"
            >{{ path.split(/[/\\]/).pop() }}</span>
            <input
              v-else
              readonly
              class="field-input"
              style="flex: 1; background: var(--sh-paper-2);"
              placeholder="No file selected"
            />
            <button data-testid="select-file" @click="selectFile" class="btn btn-sm">
              <Icon name="fileText" :size="14" /> Browse
            </button>
          </div>
        </div>

        <div v-if="path" class="row" style="gap: 24px; flex-wrap: wrap;">
          <div v-if="inspection?.sheets?.length">
            <label class="field-label">Sheet</label>
            <select data-testid="sheet-select" v-model="sheet" @change="inspect" class="field-input">
              <option v-for="s in inspection.sheets" :key="s" :value="s">{{ s }}</option>
            </select>
          </div>
          <div>
            <label class="field-label">First row is headers</label>
            <input data-testid="header-toggle" type="checkbox" v-model="hasHeader" @change="inspect" />
          </div>
          <div>
            <label class="field-label">Table name</label>
            <input data-testid="table-name" v-model="tableName" class="field-input" placeholder="my_table" />
          </div>
          <div v-if="inspection" class="sh-meta" style="align-self: flex-end;">{{ inspection.row_count }} row(s)</div>
        </div>

        <div v-if="tableExists && summary" class="col" data-testid="mode-group" style="gap: var(--sh-space-3); padding: var(--sh-space-4); border: 1.5px dashed var(--vibe-border-subtle); border-radius: var(--sh-radius-md);">
          <div class="field-label" style="padding: 0;">
            {{ tableName }} already exists ({{ summary.existing_rows }} row{{ summary.existing_rows === 1 ? '' : 's' }})
          </div>
          <label
            v-for="m in MODE_ORDER"
            :key="m"
            class="row"
            style="align-items: flex-start; gap: 10px; cursor: pointer;"
          >
            <input
              type="radio"
              name="import-mode"
              :value="m"
              v-model="mode"
              :disabled="!summary.modes?.includes(m)"
              :data-testid="`mode-${m}`"
            />
            <span>
              <span style="font-weight: 600; display: block;">{{ MODE_LABELS[m] }}</span>
              <span v-if="!summary.modes?.includes(m)" class="sh-meta">{{ MODE_DISABLED_REASON[m] }}</span>
            </span>
          </label>
          <label class="row" style="align-items: flex-start; gap: 10px; cursor: pointer;">
            <input type="radio" name="import-mode" :checked="false" data-testid="mode-new" @change="chooseNewTable" />
            <span style="font-weight: 600;">Import as a new table</span>
          </label>
        </div>

        <div v-if="path && showKeySelect">
          <label class="field-label">Primary key</label>
          <select data-testid="key-select" v-model="key" class="field-input" style="min-width: 260px;">
            <option v-for="c in keyCandidates" :key="c.name" :value="c.name">
              {{ c.name }}{{ c.name === inspection?.suggested_key ? ' (suggested)' : '' }}
            </option>
            <option :value="KEY_AUTO">Add an auto-number id column</option>
            <option :value="KEY_NONE">No key: rows can't be edited in the datasheet</option>
          </select>
        </div>
        <div v-else-if="path && summary">
          <label class="field-label">Primary key</label>
          <div class="field-input" style="background: var(--sh-paper-2);">
            Key: {{ summary.table_key || 'none' }} (from the table)
          </div>
        </div>

        <!-- The action area (result, problems, Import) sits ABOVE the
             preview grid, not below it: the grid can run to 50 rows, and a
             reviewer caught this exact case — at a modest window height the
             summary/banner/button were only reachable by scrolling past the
             whole grid. The grid gets its own capped, independently
             scrollable box below instead. -->
        <div v-if="modeNotChosen" class="sh-meta" data-testid="mode-prompt">
          Choose what to do with the existing table above.
        </div>

        <div v-else-if="summary && !hasProblems" class="sh-meta" data-testid="summary-line">
          {{ summarySentence(summary) }}
        </div>

        <div v-else-if="hasProblems" class="banner attn" data-testid="problem-banner">
          <div class="ico"><Icon name="alert" :size="22" /></div>
          <div class="body">
            <div class="ttl">
              {{ summary?.blocked ? "Can't import yet"
                : (summary?.bad_values?.length || mismatchText) ? 'Some values need attention'
                : 'Some rows already match exactly' }}
            </div>
            <div class="sub">
              <div v-if="summary">{{ summarySentence(summary) }}</div>
              <div v-if="summary?.blocked">{{ summary.blocked }}</div>
              <div v-if="mismatchText">{{ mismatchText }}</div>
              <table v-if="summary?.bad_values?.length" style="margin-top: 8px; border-collapse: collapse; width: 100%;">
                <thead>
                  <tr>
                    <th style="text-align: left; padding: 4px 8px 4px 0;">Row</th>
                    <th style="text-align: left; padding: 4px 8px;">Column</th>
                    <th style="text-align: left; padding: 4px 8px;">Value</th>
                    <th style="text-align: left; padding: 4px 8px;">Expected</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(b, i) in summary!.bad_values" :key="i">
                    <td style="padding: 4px 8px 4px 0;">{{ b.row }}</td>
                    <td style="padding: 4px 8px;">{{ b.column }}</td>
                    <td style="padding: 4px 8px; white-space: pre;">{{ b.value }}</td>
                    <td style="padding: 4px 8px;">{{ b.want }}</td>
                  </tr>
                </tbody>
              </table>
              <div v-if="summary && summary.bad_count > (summary.bad_values?.length || 0)">
                and {{ summary.bad_count - (summary.bad_values?.length || 0) }} more
              </div>
            </div>
          </div>
        </div>

        <button
          v-if="path"
          data-testid="import-btn"
          @click="runImport"
          :disabled="!canImport"
          class="btn btn-primary"
          style="justify-content: center;"
        >
          <Icon name="download" :size="16" /> {{ importing ? 'Importing…' : 'Import' }}
        </button>

        <div v-if="error" class="banner attn" data-testid="error-banner">
          <div class="ico"><Icon name="alert" :size="22" /></div>
          <div class="body">
            <div class="ttl">{{ errorContext === 'import' ? 'Import failed' : "Couldn't preview this file" }}</div>
            <div class="sub">{{ error }}</div>
          </div>
        </div>

        <!-- min-width: 0 overrides the flex item default (min-width: auto),
             which would otherwise let the wide table push this whole flex
             column wider instead of scrolling within its own box.
             max-height + overflow-y caps the grid's own height so a 50-row
             preview never pushes the action area above out of view. -->
        <div
          v-if="inspection?.columns?.length"
          data-testid="preview-grid"
          class="card"
          style="overflow: auto; min-width: 0; max-height: 22rem;"
        >
          <table style="border-collapse: collapse; width: 100%;">
            <thead>
              <tr>
                <th
                  v-for="c in inspection.columns"
                  :key="c.name"
                  style="text-align: left; padding: 8px; border-bottom: 1px solid var(--sh-line); vertical-align: top; position: sticky; top: 0; background: var(--sh-surface);"
                  :style="summary?.extra?.includes(c.name) ? { borderColor: 'var(--sh-coral)' } : {}"
                >
                  <div class="row" style="gap: 4px; align-items: center; margin-bottom: 4px; white-space: nowrap;">
                    <Icon v-if="isKeyColumn(c.name)" name="key" :size="12" />
                    <span>{{ c.name }}</span>
                  </div>
                  <select
                    :data-testid="`type-select-${c.name}`"
                    class="field-input"
                    :style="{ minWidth: '160px', fontSize: '12px', opacity: typeSelectDisabled() ? 0.55 : 1, cursor: typeSelectDisabled() ? 'not-allowed' : 'auto' }"
                    :disabled="typeSelectDisabled()"
                    :value="typeOverrides[c.name]?.type"
                    @change="setColumnType(c.name, ($event.target as HTMLSelectElement).value as ImportFriendlyType)"
                  >
                    <option v-for="t in IMPORT_TYPES" :key="t" :value="t">{{ typeLabel({ type: t }) }}</option>
                  </select>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, ri) in inspection.rows" :key="ri">
                <td
                  v-for="(cell, ci) in row"
                  :key="ci"
                  style="padding: 8px; border-bottom: 1px solid var(--sh-line); white-space: pre;"
                >{{ cell }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        </template>

        <div v-if="successSummary" class="banner teal" data-testid="success-banner">
          <div class="ico"><Icon name="check" :size="22" /></div>
          <div class="body">
            <div class="ttl">Imported into {{ successSummary.table }}</div>
            <div class="sub">{{ summarySentence(successSummary, true) }}</div>
            <div class="row" style="margin-top: 10px; gap: 12px;">
              <button data-testid="open-table-btn" @click="openTable" class="btn btn-sm btn-primary">
                Open table <Icon name="arrowRight" :size="14" />
              </button>
              <button data-testid="import-another-btn" @click="importAnother" class="btn btn-sm">
                Import another file
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
