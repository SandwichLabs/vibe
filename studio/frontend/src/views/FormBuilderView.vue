<script setup lang="ts">
// FormBuilderView — the Access-style Form *authoring* tool (parity plan W2).
//
// This is the builder, NOT the runtime player. FormView.vue stays the App-Mode
// form renderer (reachable at /forms); this lives on the builderOnly route
// /forms/:id/edit and is locked out of App Mode by the router guard.
//
// Layout translates tmp/design/hifi/hifi-create.jsx "Screen 4: Form Builder"
// into Vue + the SchoolHouse `.split-page` shell:
//   left  (.col-pane)        — the field list / palette (select + add fields)
//   center (.center)         — a live preview that re-renders per archetype
//   right (.col-pane.right)  — properties for the selected field + Ask agent
//
// Wiring:
//   GetForm   — load the form definition being edited
//   SaveForm  — Publish (persists the full spec; bumps the stored version)
//   AgentChat — the per-field "Ask agent" affordance (real LLM round-trip, W0)
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'

const route = useRoute() ?? ({ params: {}, query: {} } as ReturnType<typeof useRoute>)
const router = useRouter()

const spec = ref<FormSpec | null>(null)
// JSON snapshot of the last-saved spec, used to drive the Unsaved badge.
const savedJSON = ref('')
const selectedIdx = ref<number | null>(null)

const loading = ref(true)
const publishing = ref(false)
const errorMessage = ref('')
const statusMessage = ref('')

// Per-field agent affordance state.
const agentPrompt = ref('')
const agentResponse = ref('')
const agentBusy = ref(false)
const agentError = ref('')

// The four Access form archetypes the segmented control switches between.
const ARCHETYPES = [
  { key: 'columnar', label: 'Columnar', icon: 'layout' },
  { key: 'continuous', label: 'Continuous', icon: 'layers' },
  { key: 'datasheet', label: 'Datasheet', icon: 'tableIcon' },
  { key: 'master_detail', label: 'Master-Detail', icon: 'form' },
]

const formId = computed(() => {
  const p = route.params.id
  const id = Array.isArray(p) ? p[0] : p
  return id ? String(id) : ''
})

const fields = computed(() => spec.value?.fields ?? [])
const archetype = computed(() => spec.value?.archetype ?? 'columnar')
const selectedField = computed<FormFieldSpec | null>(() =>
  selectedIdx.value != null ? (fields.value[selectedIdx.value] ?? null) : null,
)

const dirty = computed(() => spec.value != null && JSON.stringify(spec.value) !== savedJSON.value)

// Friendly field-type vocabulary for the palette (Access "field type" labels),
// mapping the stored control_type to a short uppercase tag.
function typeLabel(control_type: string): string {
  switch (control_type) {
    case 'textarea': return 'Long text'
    case 'number': case 'currency': return 'Number'
    case 'date': case 'datetime': return 'Date'
    case 'select': case 'lookup': return 'Pick'
    case 'email': case 'url': return 'Text'
    default: return 'Text'
  }
}

// controlInputType maps a stored control_type to an <input type> for the preview.
function controlInputType(t: string): string {
  switch (t) {
    case 'number': case 'currency': return 'number'
    case 'date': return 'date'
    case 'datetime': return 'datetime-local'
    case 'email': return 'email'
    case 'url': return 'url'
    default: return 'text'
  }
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    if (!formId.value) {
      errorMessage.value = 'No form selected to edit.'
      loading.value = false
      return
    }
    const s = await window.go.main.App.GetForm(formId.value)
    spec.value = s
    savedJSON.value = JSON.stringify(s)
    if ((s?.fields?.length ?? 0) > 0) selectedIdx.value = 0
  } catch (e: any) {
    errorMessage.value = 'Failed to open form: ' + (e?.message || e)
  }
  loading.value = false
}

function selectField(i: number) {
  selectedIdx.value = i
  // Reset the per-field agent affordance when the selection changes.
  agentPrompt.value = ''
  agentResponse.value = ''
  agentError.value = ''
}

function setArchetype(key: string) {
  if (spec.value) spec.value.archetype = key
}

function addField() {
  if (!spec.value) return
  const n = spec.value.fields.length + 1
  spec.value.fields.push({
    name: `field_${n}`,
    caption: 'New field',
    control_type: 'text',
    visibility: 'Always show',
  })
  selectField(spec.value.fields.length - 1)
}

function removeSelected() {
  if (!spec.value || selectedIdx.value == null) return
  spec.value.fields.splice(selectedIdx.value, 1)
  selectedIdx.value = spec.value.fields.length ? 0 : null
}

function toggleRequired() {
  const f = selectedField.value
  if (f) f.required = !f.required
}

async function publish() {
  if (!spec.value) return
  publishing.value = true
  errorMessage.value = ''
  statusMessage.value = ''
  try {
    await window.go.main.App.SaveForm(spec.value)
    savedJSON.value = JSON.stringify(spec.value)
    statusMessage.value = 'Published'
  } catch (e: any) {
    errorMessage.value = 'Publish failed: ' + (e?.message || e)
  }
  publishing.value = false
}

// previewInApp opens the runtime player (FormView) for this form — the actual
// App-Mode renderer — so the builder's "Preview in App Mode" is a real round
// trip, not a mock.
function previewInApp() {
  if (!spec.value) return
  router?.push({ path: '/forms', query: { id: spec.value.id } })
}

// askAgent sends the per-field prompt to the real agent (AgentChat → internal/
// llm), pinning the field as context. The response renders inline; we never
// fake it — on failure we surface the error.
async function askAgent() {
  const f = selectedField.value
  if (!f || !agentPrompt.value.trim()) return
  agentBusy.value = true
  agentError.value = ''
  agentResponse.value = ''
  const context = `Form "${spec.value?.title}" (table ${spec.value?.table}), field "${f.name}" (${f.caption}).`
  try {
    const res = await window.go.main.App.AgentChat(agentPrompt.value.trim(), context)
    agentResponse.value = res?.text || '(no response)'
  } catch (e: any) {
    agentError.value = 'Agent unavailable: ' + (e?.message || e)
  }
  agentBusy.value = false
}

onMounted(load)
</script>

<template>
  <div class="col" style="height: 100%; min-height: 0; gap: 0;">
    <!-- Subhead: breadcrumb, archetype segmented control, Unsaved badge, actions -->
    <div class="page-subhead">
      <div>
        <div class="crumb">HVAC Ops · Forms · {{ spec?.title || formId }}</div>
        <div class="row" style="margin-top: 4px; gap: 10px; align-items: center;">
          <h1 style="font-size: 24px;">{{ spec?.title || 'Form' }} · form</h1>
          <span
            v-if="dirty"
            class="badge badge--warn"
            data-testid="fb-unsaved"
          >Unsaved</span>
          <span
            v-else-if="statusMessage"
            class="badge badge--teal"
            data-testid="fb-published"
          >{{ statusMessage }}</span>
        </div>
      </div>
      <span class="spacer"></span>

      <span class="sh-mono" style="font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite); margin-right: 6px;">Archetype:</span>
      <div class="seg" role="tablist" aria-label="Form archetype" data-testid="fb-archetype">
        <button
          v-for="a in ARCHETYPES"
          :key="a.key"
          class="seg-btn"
          :class="{ on: archetype === a.key }"
          :data-arch="a.key"
          role="tab"
          :aria-selected="archetype === a.key"
          data-testid="fb-arch"
          @click="setArchetype(a.key)"
        >
          <Icon :name="a.icon" :size="14" /> {{ a.label }}
        </button>
      </div>

      <button class="btn btn-sm" data-testid="fb-preview-btn" @click="previewInApp">
        <Icon name="eye" :size="14" /> Preview in App Mode
      </button>
      <button class="btn btn-primary btn-sm" data-testid="fb-publish" :disabled="publishing || !spec" @click="publish">
        {{ publishing ? 'Publishing…' : 'Publish' }}
      </button>
    </div>

    <div v-if="errorMessage" style="padding: 10px 24px; font-size: 14px; font-weight: 600; color: var(--sh-error);">{{ errorMessage }}</div>

    <div v-if="loading" class="sh-mono" style="padding: 24px; color: var(--sh-graphite);">Loading…</div>

    <!-- 3-pane builder -->
    <div v-else-if="spec" class="split-page" data-testid="form-builder">
      <!-- Field list / palette -->
      <div class="col-pane">
        <div class="sh-mono" style="font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite);">Fields · {{ spec.table }}</div>
        <div style="display: flex; flex-direction: column; gap: 4px; overflow: auto; flex: 1; min-height: 0;">
          <div
            v-for="(f, i) in fields"
            :key="f.name + ':' + i"
            class="field-row"
            :class="{ sel: selectedIdx === i, active: selectedIdx === i }"
            data-testid="fb-field-row"
            role="button"
            tabindex="0"
            @click="selectField(i)"
            @keydown.enter="selectField(i)"
          >
            <span class="drag"><Icon name="grip" :size="14" /></span>
            <span class="nm">{{ f.name }}</span>
            <span class="ty">{{ typeLabel(f.control_type) }}</span>
          </div>
        </div>
        <button class="btn btn-ghost btn-sm" data-testid="fb-add-field" @click="addField">
          <Icon name="plus" :size="14" /> Add field
        </button>
      </div>

      <!-- Live preview (re-renders per archetype) -->
      <div class="center" style="overflow: hidden;">
        <div class="row" style="justify-content: space-between;">
          <span class="sh-mono" style="font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite);">Live preview · {{ archetype }}</span>
          <span class="badge badge--teal"><Icon name="zap" :size="11" /> Pixel-accurate to App Mode</span>
        </div>

        <div
          class="card card-pad"
          style="background: var(--sh-paper); overflow: auto; min-height: 0;"
          data-testid="fb-preview"
          :data-archetype="archetype"
        >
          <!-- Columnar / Master-Detail header: 2-col grid of bound controls -->
          <div
            v-if="archetype === 'columnar' || archetype === 'master_detail'"
            style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;"
          >
            <div
              v-for="(f, i) in fields"
              :key="f.name"
              class="col"
              style="gap: 4px;"
              data-testid="fb-preview-field"
              :class="{ 'preview-selected': selectedIdx === i }"
            >
              <label class="sh-label" style="margin: 0;">
                {{ f.caption || f.name }}<span v-if="f.required" style="color: var(--sh-error);"> *</span>
              </label>
              <textarea v-if="f.control_type === 'textarea'" rows="2" :placeholder="f.placeholder" style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-family: inherit; font-size: 14px;"></textarea>
              <input v-else :type="controlInputType(f.control_type)" :placeholder="f.placeholder" style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-size: 14px;" />
              <span v-if="f.help" class="sh-mono" style="font-size: 11px; color: var(--sh-graphite);">{{ f.help }}</span>
            </div>
          </div>

          <!-- Continuous: stacked record cards (Access continuous form) -->
          <div v-else-if="archetype === 'continuous'" style="display: flex; flex-direction: column; gap: 10px;">
            <div
              v-for="r in 2"
              :key="r"
              class="card card-pad"
              style="display: flex; flex-wrap: wrap; gap: 14px; box-shadow: 1px 1px 0 0 var(--sh-ink);"
            >
              <div v-for="f in fields" :key="f.name" class="col" style="gap: 2px; min-width: 120px;">
                <label class="sh-mono" style="font-size: 10px; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite);">{{ f.caption || f.name }}</label>
                <input :type="controlInputType(f.control_type)" :placeholder="f.placeholder" style="padding: 6px 8px; border: 1px solid rgba(31,35,48,0.18); border-radius: 4px; font-size: 13px;" />
              </div>
            </div>
          </div>

          <!-- Datasheet: spreadsheet grid, one column per field -->
          <table v-else class="table">
            <thead>
              <tr><th v-for="f in fields" :key="f.name">{{ f.caption || f.name }}<span v-if="f.required" style="color: var(--sh-error);"> *</span></th></tr>
            </thead>
            <tbody>
              <tr v-for="r in 3" :key="r">
                <td v-for="f in fields" :key="f.name"><span class="sh-mono" style="color: var(--sh-chalk);">—</span></td>
              </tr>
            </tbody>
          </table>

          <!-- Master-Detail subform datasheet -->
          <template v-if="archetype === 'master_detail' && spec.subform">
            <div class="subform-head" style="margin-top: 22px;">
              <h3>{{ spec.subform.child_table }} · subform</h3>
            </div>
            <table class="table">
              <thead>
                <tr><th v-for="sf in spec.subform.fields" :key="sf.name">{{ sf.caption || sf.name }}</th></tr>
              </thead>
              <tbody>
                <tr v-for="r in 2" :key="r"><td v-for="sf in spec.subform.fields" :key="sf.name"><span class="sh-mono" style="color: var(--sh-chalk);">—</span></td></tr>
              </tbody>
            </table>
          </template>
        </div>
      </div>

      <!-- Properties panel for the selected field -->
      <div class="col-pane right" data-testid="fb-props">
        <template v-if="selectedField">
          <div class="sh-mono" style="font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-teal);">Selected · {{ selectedField.name }}</div>

          <div class="col" style="gap: 4px;">
            <label class="sh-label" style="margin: 0;">Label</label>
            <input v-model="selectedField.caption" data-testid="fb-prop-label" style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-size: 14px;" />
          </div>

          <div class="col" style="gap: 4px;">
            <label class="sh-label" style="margin: 0;">Placeholder</label>
            <input v-model="selectedField.placeholder" data-testid="fb-prop-placeholder" style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-size: 14px;" />
          </div>

          <div class="row" style="gap: 8px; align-items: center;">
            <span class="sh-mono" style="font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite);">Required</span>
            <span class="spacer"></span>
            <span
              class="toggle"
              :class="{ off: !selectedField.required }"
              data-testid="fb-prop-required"
              role="switch"
              :aria-checked="!!selectedField.required"
              tabindex="0"
              @click="toggleRequired"
              @keydown.enter="toggleRequired"
            ></span>
          </div>

          <div class="col" style="gap: 4px;">
            <label class="sh-label" style="margin: 0;">Visibility rule</label>
            <input v-model="selectedField.visibility" data-testid="fb-prop-visibility" placeholder="Always show" style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-size: 14px;" />
          </div>

          <div class="col" style="gap: 4px;">
            <label class="sh-label" style="margin: 0;">Validation</label>
            <input v-model="selectedField.validation" data-testid="fb-prop-validation" placeholder="e.g. Must exist in Technicians" style="width: 100%; padding: 8px 10px; border: var(--sh-border); border-radius: 6px; font-size: 14px;" />
          </div>

          <!-- Per-field Ask agent (real AgentChat round-trip, W0). -->
          <div class="ask-agent" data-testid="fb-ask-agent">
            <span class="lab"><Icon name="sparkle" :size="12" /> Ask agent</span>
            <textarea
              v-model="agentPrompt"
              class="ph"
              data-testid="fb-agent-prompt"
              placeholder="&quot;Make this hide when status is Closed&quot;"
              style="width: 100%; resize: vertical; font-family: inherit;"
            ></textarea>
            <button class="btn btn-primary btn-sm" data-testid="fb-agent-apply" :disabled="agentBusy || !agentPrompt.trim()" @click="askAgent">
              {{ agentBusy ? 'Asking…' : 'Apply with agent' }}
            </button>
            <div v-if="agentResponse" class="agent-response" data-testid="fb-agent-response" style="font-size: 13px; white-space: pre-wrap; color: var(--sh-ink);">{{ agentResponse }}</div>
            <div v-if="agentError" data-testid="fb-agent-error" style="font-size: 12px; color: var(--sh-error);">{{ agentError }}</div>
          </div>

          <span class="spacer"></span>
          <button class="btn btn-ghost btn-sm" data-testid="fb-remove-field" style="color: var(--sh-error);" @click="removeSelected">
            <Icon name="trash" :size="14" /> Remove field
          </button>
        </template>
        <div v-else class="sh-mono" style="color: var(--sh-graphite);">Select a field to edit its properties.</div>
      </div>
    </div>
  </div>
</template>
