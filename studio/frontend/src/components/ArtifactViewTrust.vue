<script setup lang="ts">
// ArtifactViewTrust — a custom (script) view, shown as source until the person
// allows it (#159). Nothing here runs the view: the text of view.js and
// artifact.yaml sits in read-only CodeMirror, and Allow approves exactly the hash
// that was on screen (Go refuses it if the files changed since). A view that was
// allowed and then edited says so and asks again. An allowed view runs in
// CustomView (the Worker host, #160); a change to the artifact on disk, or the
// state leaving "allowed", tears that down at once and this screen asks again.
// Every string is text.
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import Icon from './Icon.vue'
import { createVibeEditor, type VibeEditorInstance } from '../composables/useVibeEditor'
import CustomView from './CustomView.vue'
import { useArtifactViewTrust } from '../composables/useArtifactViewTrust'
import type { ParamValue } from '../a2ui/artifactParams'

const props = defineProps<{ name: string; detail?: ArtifactDetail | null; values?: Record<string, ParamValue> }>()
const { status, changedWhileReading, showNew, busy, error, refreshing, allow, deny, revoke } = useArtifactViewTrust(() => props.name)

const state = computed(() => status.value?.state ?? 'none')
const shortHash = computed(() => (status.value?.hash ?? '').slice(0, 12))

type FileTab = 'script' | 'manifest'
const tab = ref<FileTab>('script')
const scriptEl = ref<HTMLElement | null>(null)
const manifestEl = ref<HTMLElement | null>(null)
const editors: Partial<Record<FileTab, VibeEditorInstance>> = {}

function mount(which: FileTab, el: HTMLElement | null) {
  if (!el || editors[which]) return
  editors[which] = createVibeEditor(el, {
    value: (which === 'script' ? status.value?.source : status.value?.manifest) ?? '',
    lang: which === 'script' ? 'javascript' : 'yaml',
    readOnly: true,
  })
}
onMounted(() => { mount('script', scriptEl.value); mount('manifest', manifestEl.value) })
onBeforeUnmount(() => { for (const e of Object.values(editors)) e?.destroy() })

// The text on screen is always the text the hash covers: a reload replaces it.
watch(status, (s) => {
  for (const [which, text] of [['script', s?.source], ['manifest', s?.manifest]] as const) {
    const e = editors[which]
    if (e && e.getValue() !== (text ?? '')) e.setValue(text ?? '')
  }
})
watch(tab, async () => { await nextTick(); editors[tab.value]?.view.requestMeasure() })
// The view runs only while it is allowed and nothing is being re-read.
const running = computed(() => state.value === 'allowed' && !refreshing.value && !!props.detail && !props.detail.error)
async function remeasure() { await nextTick(); for (const e of Object.values(editors)) e?.view.requestMeasure() }
</script>

<template>
  <div class="col view-trust" data-testid="view-trust">
    <div class="row view-trust__bar">
      <strong>Custom view</strong>
      <span v-if="state === 'allowed'" class="badge badge--success" data-testid="view-trust-state"><span class="dot"></span>Allowed</span>
      <span v-else-if="state === 'denied'" class="badge badge--error" data-testid="view-trust-state"><span class="dot"></span>Denied</span>
      <span v-else-if="state === 'changed'" class="badge badge--warn" data-testid="view-trust-state"><span class="dot"></span>Changed since you allowed it</span>
      <span v-else-if="state === 'undecided'" class="badge badge--warn" data-testid="view-trust-state"><span class="dot"></span>Not allowed yet</span>
      <span style="flex: 1;" />
      <span v-if="shortHash" class="sh-mono view-trust__hash" :title="status?.hash" data-testid="view-trust-hash">{{ shortHash }}</span>
    </div>

    <div v-if="error" class="view-trust__err" role="alert" data-testid="view-trust-error">{{ error }}</div>
    <div v-if="status?.error" class="view-trust__err" role="alert" data-testid="view-trust-unusable">
      This view can't be shown: <span class="sh-mono">{{ status.error }}</span>
    </div>

    <template v-if="status && !status.error">
      <div v-if="state === 'denied'" class="note warn" role="status" data-testid="view-trust-denied">
        <Icon name="alert" :size="16" />
        <span>You denied this view, so it will not run, even after edits. Revoke the decision to be asked again.</span>
      </div>
      <div v-else-if="state !== 'allowed'" class="note warn" role="status" data-testid="view-trust-line">
        <Icon name="alert" :size="16" />
        <span>
          <template v-if="state === 'changed'">This view changed since you allowed it. Read it again before you allow it. </template>
          This view runs code written by someone else or by the agent. It cannot reach the network or your files, and can only run this artifact's queries.
        </span>
      </div>

      <div v-if="changedWhileReading" class="note warn" role="alert" data-testid="view-trust-changed-reading">
        <Icon name="alert" :size="16" />
        <span>This view changed while you were reading it. Allow is off until you look at the new version.</span>
        <button type="button" class="btn btn-sm" data-testid="view-trust-show-new" @click="showNew()">Show the new version</button>
      </div>

      <div class="row view-trust__actions">
        <button v-if="state !== 'allowed'" type="button" class="btn btn-primary btn-sm" :disabled="busy || changedWhileReading" data-testid="view-trust-allow" @click="allow()">Allow</button>
        <button v-if="state !== 'denied'" type="button" class="btn btn-sm" :disabled="busy" data-testid="view-trust-deny" @click="deny()">Deny</button>
        <button v-if="state === 'allowed' || state === 'denied'" type="button" class="btn btn-sm" :disabled="busy" data-testid="view-trust-revoke" @click="revoke()">Revoke</button>
      </div>
    </template>

    <CustomView v-if="running" :name="name" :detail="detail!" :values="values ?? {}" />
    <div v-else-if="state === 'allowed'" class="note info" role="status" data-testid="view-trust-checking">
      <Icon name="info" :size="16" />
      <span>Checking the view…</span>
    </div>

    <component :is="state === 'allowed' ? 'details' : 'div'" class="col view-trust__src" data-testid="view-trust-src" @toggle="remeasure">
    <summary v-if="state === 'allowed'" class="view-trust__summary">View source</summary>
    <div class="seg view-trust__tabs" role="group" aria-label="Custom view file">
      <button type="button" :class="{ on: tab === 'script' }" :aria-pressed="tab === 'script'" data-testid="view-trust-tab-script" @click="tab = 'script'">view.js</button>
      <button type="button" :class="{ on: tab === 'manifest' }" :aria-pressed="tab === 'manifest'" data-testid="view-trust-tab-manifest" @click="tab = 'manifest'">artifact.yaml</button>
    </div>
    <div class="view-trust__editors">
      <div v-show="tab === 'script'" ref="scriptEl" class="view-trust__editor" data-testid="view-trust-source" />
      <div v-show="tab === 'manifest'" ref="manifestEl" class="view-trust__editor" data-testid="view-trust-manifest" />
    </div>
    </component>
  </div>
</template>

<style scoped>
.view-trust { gap: var(--sh-space-3); }
.view-trust__bar { align-items: center; gap: var(--sh-space-3); }
.view-trust__tabs { width: fit-content; align-self: flex-start; }
.view-trust__summary { cursor: pointer; font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin-bottom: var(--sh-space-2); }
.view-trust__src { gap: var(--sh-space-3); }
.view-trust__hash { font-size: var(--sh-fs-xs); color: var(--sh-graphite); }
.view-trust__actions { gap: var(--sh-space-2); }
.view-trust__editors { min-height: 320px; border: var(--sh-border); border-radius: var(--sh-radius); overflow: hidden; display: flex; flex-direction: column; }
.view-trust__editor { flex: 1; min-height: 320px; max-height: 60vh; overflow: hidden; }
.view-trust__editor :deep(.cm-editor) { height: 100%; max-height: 60vh; }
.view-trust__err { border: 2px solid var(--sh-error); background: var(--sh-paper-2); color: var(--sh-error); padding: var(--sh-space-3); border-radius: var(--sh-radius); }
</style>
