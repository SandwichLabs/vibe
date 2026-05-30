<script setup lang="ts">
// MacroBuilderView (Phase 5 / P5.9) — the Macro builder, the Studio-only surface
// that translates Access's macro builder into the SchoolHouse Vue system.
//
// It lists the container's stored macros (ListMacro), lets the builder pick one,
// then add / reorder / remove ordered actions. Each action has a kind picker and
// — for object-bound kinds — a catalog-backed target picker (ListNavTargets).
// The edited spec is persisted via SaveMacro(spec) and test-run via RunMacro(id).
//
// It is BUILDER-ONLY: the /macros route is meta.builderOnly so it is unreachable
// in App Mode, and the App-Mode chrome omits the sidebar link entirely.
import { ref, computed, onMounted } from 'vue'
import Icon from '../components/Icon.vue'

interface MacroAction {
  kind: string
  args?: Record<string, string>
}

interface MacroSpec {
  id: string
  title: string
  version: number
  actions: MacroAction[]
}

interface NavTarget {
  kind: string
  id: string
  label: string
}

// The §5.1 action vocabulary surfaced in the builder's kind picker.
const ACTION_KINDS = [
  'OpenForm',
  'OpenReport',
  'GoToRecord',
  'FindRecord',
  'RunQuery',
  'RunSQL',
  'SetValue',
  'SetFilter',
  'MsgBox',
  'Export',
  'EmailObject',
  'RunMacro',
  'OpenSwitchboard',
]

// Kinds that bind to a catalog object (a nav target id).
const TARGET_KINDS = new Set(['OpenForm', 'OpenReport'])

const macros = ref<MacroSpec[]>([])
const navTargets = ref<NavTarget[]>([])
const selectedID = ref('')
const actions = ref<MacroAction[]>([])
const errorMessage = ref('')
const lastRun = ref('')

const selectedMacro = computed(() => macros.value.find((m) => m.id === selectedID.value))

function targetsForKind(kind: string): NavTarget[] {
  // Match the catalog's nav-action kind. ListNavTargets stamps each target with
  // either the canonical nav kind (open_form / open_report) or a plain object
  // kind (form / report) — accept both so the picker stays catalog-backed.
  if (kind === 'OpenForm') {
    return navTargets.value.filter((t) => t.kind === 'open_form' || t.kind === 'form')
  }
  if (kind === 'OpenReport') {
    return navTargets.value.filter((t) => t.kind === 'open_report' || t.kind === 'report')
  }
  return navTargets.value
}

async function loadMacros() {
  try {
    macros.value = (await window.go.main.App.ListMacro()) || []
  } catch (e: any) {
    errorMessage.value = 'Could not list macros: ' + (e.message || e)
  }
}

async function loadCatalog() {
  try {
    navTargets.value = (await window.go.main.App.ListNavTargets()) || []
  } catch (e: any) {
    errorMessage.value = 'Could not load catalog: ' + (e.message || e)
  }
}

// onSelect loads the chosen macro's actions into the editable working copy.
function onSelect() {
  const m = selectedMacro.value
  actions.value = m ? JSON.parse(JSON.stringify(m.actions || [])) : []
}

function addAction() {
  actions.value.push({ kind: 'OpenForm', args: {} })
}

function removeAction(i: number) {
  actions.value.splice(i, 1)
}

function moveUp(i: number) {
  if (i <= 0) return
  const arr = actions.value
  ;[arr[i - 1], arr[i]] = [arr[i], arr[i - 1]]
}

function moveDown(i: number) {
  const arr = actions.value
  if (i >= arr.length - 1) return
  ;[arr[i + 1], arr[i]] = [arr[i], arr[i + 1]]
}

function setArg(i: number, key: string, value: string) {
  const a = actions.value[i]
  if (!a.args) a.args = {}
  a.args[key] = value
}

async function save() {
  const m = selectedMacro.value
  if (!m) return
  const spec: MacroSpec = {
    id: m.id,
    title: m.title,
    version: m.version || 1,
    actions: JSON.parse(JSON.stringify(actions.value)),
  }
  try {
    await window.go.main.App.SaveMacro(spec)
    await loadMacros()
  } catch (e: any) {
    errorMessage.value = 'Save failed: ' + (e.message || e)
  }
}

async function run() {
  if (!selectedID.value) return
  try {
    const res = await window.go.main.App.RunMacro(selectedID.value)
    const intents = (res && (res.intents || res.Intents)) || []
    lastRun.value = intents.length
      ? intents.map((i: any) => i.kind || i.Kind).join(', ')
      : 'No UI intents (server-side actions ran)'
  } catch (e: any) {
    errorMessage.value = 'Run failed: ' + (e.message || e)
  }
}

onMounted(() => {
  loadMacros()
  loadCatalog()
})
</script>

<template>
  <div class="macro-builder">
    <header class="mb-head">
      <div>
        <h1>Macro Builder</h1>
        <p class="subtitle">Compose declarative actions — the modern Access macro.</p>
      </div>
    </header>

    <div class="mb-toolbar">
      <label class="field">
        <span class="label">Macro</span>
        <select
          v-model="selectedID"
          data-testid="macro-select"
          class="input"
          @change="onSelect"
        >
          <option value="" disabled>Select a macro…</option>
          <option v-for="m in macros" :key="m.id" :value="m.id">{{ m.title || m.id }}</option>
        </select>
      </label>

      <div class="spacer"></div>

      <button
        v-if="selectedID"
        data-testid="run-macro"
        class="btn btn-inverted"
        @click="run"
      >
        <Icon name="play" :size="16" />
        Run
      </button>
      <button
        v-if="selectedID"
        data-testid="save-macro"
        class="btn btn-primary"
        @click="save"
      >
        <Icon name="save" :size="16" />
        Save
      </button>
    </div>

    <p v-if="lastRun" class="run-result">Last run: {{ lastRun }}</p>

    <div v-if="selectedID" class="actions card card-pad">
      <div class="actions-head">
        <h2>Actions</h2>
        <button data-testid="add-action" class="btn btn-inverted" @click="addAction">
          <Icon name="plus" :size="16" />
          Add Action
        </button>
      </div>

      <p v-if="!actions.length" class="empty">No actions yet — add one to get started.</p>

      <ol class="action-list">
        <li v-for="(action, i) in actions" :key="i" class="action-row">
          <span class="step">{{ i + 1 }}</span>

          <select
            :data-testid="`action-kind-${i}`"
            v-model="action.kind"
            class="input kind"
          >
            <option v-for="k in ACTION_KINDS" :key="k" :value="k">{{ k }}</option>
          </select>

          <!-- Object-bound kinds get a catalog-backed target picker. -->
          <select
            v-if="TARGET_KINDS.has(action.kind)"
            :data-testid="`action-target-${i}`"
            :value="action.args?.target ?? ''"
            class="input target"
            @change="setArg(i, 'target', ($event.target as HTMLSelectElement).value)"
          >
            <option value="" disabled>Select target…</option>
            <option v-for="t in targetsForKind(action.kind)" :key="t.id" :value="t.id">
              {{ t.label }} ({{ t.id }})
            </option>
          </select>

          <!-- SetFilter takes a free-form where predicate. -->
          <input
            v-else-if="action.kind === 'SetFilter'"
            :data-testid="`action-where-${i}`"
            :value="action.args?.where ?? ''"
            type="text"
            class="input where"
            placeholder="status = 'open'"
            @input="setArg(i, 'where', ($event.target as HTMLInputElement).value)"
          />

          <!-- Everything else takes a generic value arg. -->
          <input
            v-else
            :data-testid="`action-arg-${i}`"
            :value="action.args?.value ?? action.args?.message ?? action.args?.sql ?? ''"
            type="text"
            class="input arg"
            placeholder="argument"
            @input="setArg(i, 'value', ($event.target as HTMLInputElement).value)"
          />

          <div class="row-controls">
            <button
              :data-testid="`action-up-${i}`"
              class="icon-btn"
              :disabled="i === 0"
              title="Move up"
              @click="moveUp(i)"
            >
              <Icon name="chevronUp" :size="16" />
            </button>
            <button
              :data-testid="`action-down-${i}`"
              class="icon-btn"
              :disabled="i === actions.length - 1"
              title="Move down"
              @click="moveDown(i)"
            >
              <Icon name="chevronDown" :size="16" />
            </button>
            <button
              :data-testid="`action-remove-${i}`"
              class="icon-btn danger"
              title="Remove"
              @click="removeAction(i)"
            >
              <Icon name="trash" :size="16" />
            </button>
          </div>
        </li>
      </ol>
    </div>

    <div v-else class="card card-pad empty-state">
      <p>Select a macro to edit its actions.</p>
    </div>

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>

<style scoped>
.macro-builder {
  height: 100%;
  overflow: auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.mb-head h1 {
  font-family: var(--sh-font-display);
  margin: 0;
}
.subtitle {
  margin: 4px 0 0;
  color: var(--sh-graphite);
  font-size: 14px;
}
.mb-toolbar {
  display: flex;
  align-items: flex-end;
  gap: 12px;
}
.spacer {
  flex: 1;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.label {
  font-size: 12px;
  color: var(--sh-graphite);
}
.input {
  padding: 8px 10px;
  border: 1.5px solid var(--sh-ink);
  border-radius: 3px;
  font-family: var(--sh-font-mono);
  background: var(--sh-surface);
}
.actions-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.actions-head h2 {
  font-family: var(--sh-font-display);
  margin: 0;
}
.action-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.action-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.step {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1.5px solid var(--sh-ink);
  border-radius: 50%;
  font-size: 12px;
  font-family: var(--sh-font-mono);
}
.kind {
  width: 160px;
}
.target,
.where,
.arg {
  flex: 1;
}
.row-controls {
  display: flex;
  gap: 4px;
}
.icon-btn {
  border: 1.5px solid var(--sh-ink);
  background: var(--sh-surface);
  border-radius: 3px;
  padding: 4px;
  cursor: pointer;
}
.icon-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.icon-btn.danger {
  border-color: var(--sh-rust, #b3402b);
}
.empty,
.empty-state {
  color: var(--sh-graphite);
}
.run-result {
  font-family: var(--sh-font-mono);
  font-size: 13px;
  color: var(--sh-graphite);
}
</style>
