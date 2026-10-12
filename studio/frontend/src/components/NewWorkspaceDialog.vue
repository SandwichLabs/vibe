<script setup lang="ts">
import { App } from '../lib/bindings'
// NewWorkspaceDialog — SPEC 02. Name, where to put it, git, starter template.
// Opened from the launcher and from the switcher menu (Ctrl/Cmd+N). Enter in a
// text field submits when valid; Escape cancels; focus lands in Name.
import { ref, computed, watch } from 'vue'
import Icon from './Icon.vue'
import { useModalKeys } from '../composables/useModal'
import { newWorkspaceOpen, createWorkspace } from '../composables/useWorkspace'
import { useNewWorkspaceFields } from '../composables/useNewWorkspaceFields'

const TEMPLATE_COPY: Record<string, { title: string; text: string }> = {
  contacts: { title: 'Contacts', text: 'People and their notes. A small table to try things on.' },
  inventory: { title: 'Inventory', text: 'Suppliers and products, with stock levels.' },
  'order-entry': { title: 'Order entry', text: 'Customers, orders and order lines, with a starter notebook.' },
  'training-registrations': { title: 'Training registrations', text: 'Sessions and sign-ups.' },
}

const root = ref<HTMLElement | null>(null)
useModalKeys(newWorkspaceOpen, () => close(), root)

const name = ref('')
const template = ref('')
const templates = ref<TemplateInfo[]>([])
const busy = ref(false)
const error = ref('')
const dirTaken = ref(false)
const nameEl = ref<HTMLInputElement | null>(null)
const whereEl = ref<HTMLInputElement | null>(null)

const { parentDir, git, gitAvailable, slug, reset, remember, choose: chooseFolder } = useNewWorkspaceFields(name)
const valid = computed(() => name.value.trim() !== '' && parentDir.value.trim() !== '' && !dirTaken.value)

function close() {
  if (busy.value) return
  newWorkspaceOpen.value = false
}

watch(newWorkspaceOpen, async (open) => {
  if (!open) return
  name.value = ''
  template.value = ''
  error.value = ''
  dirTaken.value = false
  busy.value = false
  await reset()
  try { templates.value = (await App.ListTemplates()) || [] } catch { templates.value = [] }
})

let checkSeq = 0
async function checkDir() {
  const seq = ++checkSeq
  dirTaken.value = false
  if (!name.value.trim() || !parentDir.value.trim()) return
  try {
    const chk = await App.CheckWorkspaceDir(parentDir.value, name.value)
    if (seq === checkSeq) dirTaken.value = chk.exists && chk.notEmpty
  } catch { /* the create call reports it */ }
}
watch([name, parentDir], () => { error.value = ''; void checkDir() })

const choose = () => chooseFolder((m) => { error.value = m })

async function submit() {
  if (!valid.value || busy.value) return
  busy.value = true
  error.value = ''
  const res = await createWorkspace({
    name: name.value.trim(), dir: '', parentDir: parentDir.value.trim(),
    git: git.value && gitAvailable.value, template: template.value, inPlace: false,
  })
  busy.value = false
  if (res.ok) {
    remember()
    newWorkspaceOpen.value = false
    return
  }
  if (res.error) {
    error.value = res.error
    // Move focus to the first invalid field.
    if (/already a folder|already a workspace/i.test(res.error)) dirTaken.value = true
    whereEl.value?.focus()
  }
}

function onTemplateKey(e: KeyboardEvent, i: number, all: string[]) {
  if (e.key === 'ArrowDown' || e.key === 'ArrowRight' || e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
    e.preventDefault()
    const dir = e.key === 'ArrowDown' || e.key === 'ArrowRight' ? 1 : -1
    template.value = all[(i + dir + all.length) % all.length]
    const el = (e.currentTarget as HTMLElement).parentElement?.parentElement?.querySelectorAll<HTMLInputElement>('input[type=radio]')
    el?.[(i + dir + all.length) % all.length]?.focus()
  }
}

const templateIds = computed(() => ['', ...templates.value.map((t) => t.name)])
</script>

<template>
  <div v-if="newWorkspaceOpen" class="modal-overlay" role="presentation" @click.self="close">
    <div
      ref="root"
      class="dialog wide"
      role="dialog"
      aria-modal="true"
      aria-labelledby="nw-h"
      data-testid="new-workspace-dialog"
      @keydown.enter="(e: KeyboardEvent) => { if ((e.target as HTMLElement).tagName === 'INPUT' && (e.target as HTMLInputElement).type === 'text') { e.preventDefault(); submit() } }"
    >
      <div class="d-head"><h2 id="nw-h">New workspace</h2><p>A workspace is a folder. Vibe makes it for you and puts everything inside.</p></div>
      <div class="d-body">
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
          <div class="field">
            <label for="nw-name">Name</label>
            <input id="nw-name" ref="nameEl" v-model="name" class="field-input" :disabled="busy" aria-describedby="nw-name-h" autocomplete="off" data-testid="nw-name" />
            <div id="nw-name-h" class="help">Shown in the window title and the workspace menu.</div>
          </div>
          <div class="field">
            <label for="nw-loc">Where to put it</label>
            <div class="row" style="gap: 8px;">
              <input
                id="nw-loc"
                ref="whereEl"
                v-model="parentDir"
                class="field-input"
                :class="{ 'field-input-error': dirTaken || !!error }"
                :disabled="busy"
                :aria-invalid="dirTaken || !!error ? 'true' : undefined"
                :aria-describedby="dirTaken || error ? 'nw-err' : 'nw-loc-h'"
                autocomplete="off"
                data-testid="nw-location"
              />
              <button class="btn" type="button" style="flex: none;" :disabled="busy" @click="choose">Choose…</button>
            </div>
            <div v-if="!dirTaken && !error" id="nw-loc-h" class="help">Vibe creates a new folder here.</div>
          </div>
        </div>

        <div v-if="dirTaken" id="nw-err" class="note attn" role="alert" data-testid="nw-folder-exists">
          <Icon name="alert" :size="16" />
          <span><b>There's already a folder called {{ slug }} in {{ parentDir }}.</b> Pick another name or location. Vibe won't put a workspace inside a folder that already has files in it, unless you use Open folder instead.</span>
        </div>
        <div v-else-if="error" id="nw-err" class="note attn" role="alert" data-testid="nw-error">
          <Icon name="alert" :size="16" /><span>{{ error }}</span>
        </div>
        <div v-else class="url-tile" style="font-size: 13px;" aria-label="The folder that will be created">
          <span class="sh-label" style="display: block; margin-bottom: 4px;">Will create</span>{{ parentDir ? parentDir.replace(/[\\/]+$/, '') + '/' : '' }}<span class="acc">{{ slug }}</span>
        </div>

        <div class="switch-row">
          <input id="nw-git" v-model="git" type="checkbox" :disabled="!gitAvailable || busy" />
          <label for="nw-git">
            <span class="st-t">Start a git repository</span>
            <span class="st-d">{{ gitAvailable ? 'So you can track changes and share notebooks with your team. Vibe never commits or pushes for you.' : "git wasn't found on this computer." }}</span>
          </label>
        </div>

        <fieldset style="border: 0; margin: 0; padding: 0;" role="radiogroup">
          <legend class="field-label" style="padding: 0;">Start from</legend>
          <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 10px;">
            <label v-for="(id, i) in templateIds" :key="id || 'blank'" class="opt-card" :class="{ on: template === id }" style="cursor: pointer;">
              <input
                type="radio"
                name="nw-template"
                :value="id"
                :checked="template === id"
                :disabled="busy"
                :tabindex="template === id ? 0 : -1"
                :data-testid="`nw-template-${id || 'blank'}`"
                @change="template = id"
                @keydown="onTemplateKey($event, i, templateIds)"
              />
              <span>
                <span class="ot">{{ id === '' ? 'Blank' : (TEMPLATE_COPY[id]?.title ?? id) }}</span>
                <span class="od">{{ id === '' ? 'An empty local database. Import a CSV or connect something later.' : (TEMPLATE_COPY[id]?.text ?? templates.find((t) => t.name === id)?.description) }}</span>
              </span>
            </label>
          </div>
        </fieldset>

        <div class="note info">
          <Icon name="info" :size="16" />
          <span>This creates <code>vibe.yaml</code>, <code>notebooks/</code>, <code>queries/</code>, <code>skills/</code> and <code>data/local.duckdb</code>. The database file is kept out of git unless you choose otherwise.</span>
        </div>
      </div>
      <div class="d-foot">
        <span class="left">You can change all of this later in Settings.</span>
        <button class="btn" type="button" :disabled="busy" @click="close">Cancel</button>
        <button class="btn btn-primary" type="button" :disabled="!valid || busy" data-testid="nw-create" @click="submit">
          {{ busy ? 'Creating…' : 'Create workspace' }}
        </button>
      </div>
    </div>
  </div>
</template>
