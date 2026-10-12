<script setup lang="ts">
import { App } from '../lib/bindings'
// NoticesDialog: the generated THIRD_PARTY_NOTICES.md (embedded in the binary) in a scrollable,
// monospace, searchable viewer. Search narrows the file to the sections that mention the query.
import { computed, ref, toRef, watch } from 'vue'
import { useModalKeys } from '../composables/useModal'
import { errorText } from '../lib/errors'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const noticesOpen = toRef(props, 'open')
const root = ref<HTMLElement | null>(null)
const text = ref('')
const query = ref('')
const error = ref('')
const loading = ref(false)

function close() { emit('close') }
useModalKeys(noticesOpen, close, root, () => root.value?.querySelector<HTMLElement>('#notices-q'))

watch(noticesOpen, async (open) => {
  if (!open) { query.value = ''; return }
  if (text.value) return
  loading.value = true
  error.value = ''
  try {
    text.value = await App.GetThirdPartyNotices()
  } catch (e: unknown) {
    error.value = errorText(e)
  }
  loading.value = false
}, { immediate: true })

// A section starts at a markdown heading outside a code fence (the licence texts are fenced, and
// some contain "## " lines of their own).
function splitSections(src: string): string[] {
  const out: string[] = []
  let cur: string[] = []
  let fenced = false
  for (const line of src.split('\n')) {
    if (line.startsWith('```')) fenced = !fenced
    else if (!fenced && /^#{2,4} /.test(line) && cur.length) { out.push(cur.join('\n')); cur = [] }
    cur.push(line)
  }
  if (cur.length) out.push(cur.join('\n'))
  return out
}
const sections = computed(() => splitSections(text.value))
const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return { body: text.value, count: sections.value.length, filtered: false }
  const hits = sections.value.filter((s) => s.toLowerCase().includes(q))
  return { body: hits.join('\n'), count: hits.length, filtered: true }
})
</script>

<template>
  <div v-if="open" class="modal-overlay" role="presentation" @click.self="close">
    <div ref="root" class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="notices-h" data-testid="notices-dialog">
      <div class="d-head">
        <h2 id="notices-h">Third-party notices</h2>
        <p>The software Vibe includes, and the licences it comes under.</p>
      </div>
      <div class="d-body">
        <div class="field">
          <label for="notices-q">Search</label>
          <input id="notices-q" v-model="query" class="field-input" type="search" placeholder="A package or licence name" autocomplete="off" data-testid="notices-search" />
          <div class="help count-line" role="status" data-testid="notices-count">
            <template v-if="shown.filtered">{{ shown.count }} section{{ shown.count === 1 ? '' : 's' }} match</template>
            <template v-else-if="loading">Loading…</template>
          </div>
        </div>
        <div v-if="error" class="note attn" role="alert">{{ error }}</div>
        <pre v-else class="notices-text" tabindex="0" aria-label="Third-party notices text" data-testid="notices-text">{{ shown.body }}</pre>
      </div>
      <div class="d-foot">
        <button class="btn btn-primary" type="button" data-testid="notices-close" @click="close">Close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.notices-text { margin: 0; padding: var(--sh-space-3); height: min(50vh, 460px); overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--sh-font-mono); font-size: 12px; line-height: 1.5; background: var(--vibe-code-bg); color: var(--vibe-code-fg); border-radius: var(--sh-radius-sm); }
.d-body { overflow: hidden; }
.count-line { min-height: 1.5em; }
</style>
