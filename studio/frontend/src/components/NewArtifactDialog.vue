<script setup lang="ts">
// NewArtifactDialog — names a new data artifact (#162, step 6a). Go creates it
// from a small working starter (an existing name is refused, never overwritten)
// and the person lands on its view (the Source tab is one click away). The name is checked here for
// a good message; Go checks it again.
import { ref, computed, watch } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { useModalKeys } from '../composables/useModal'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; created: [name: string] }>()

const NAME = /^[a-z0-9][a-z0-9_-]{0,63}$/
const name = ref('')
const error = ref('')
const busy = ref(false)
const root = ref<HTMLElement | null>(null)
const open = computed(() => props.visible)
useModalKeys(open, () => { if (!busy.value) emit('close') }, root)

watch(open, (v) => { if (v) { name.value = ''; error.value = ''; busy.value = false } })

async function submit() {
  const n = name.value.trim()
  if (!NAME.test(n)) {
    error.value = 'Use 1 to 64 lowercase letters, digits, - or _ (for example "weekly-sales").'
    return
  }
  busy.value = true
  error.value = ''
  try {
    await App.NewArtifact(n, 'a2ui')
    emit('created', n)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div v-if="visible" class="modal-overlay" role="dialog" aria-modal="true" aria-label="New artifact" @click.self="!busy && emit('close')">
    <form ref="root" class="card card-pad" style="width: 100%; max-width: 420px;" data-testid="new-artifact-dialog" @submit.prevent="submit">
      <h3 class="card-title" style="margin: 0 0 var(--sh-space-3);">New artifact</h3>
      <label class="sh-label" for="new-artifact-name" style="margin: 0;">Name</label>
      <input id="new-artifact-name" v-model="name" class="field-input" style="width: 100%;" placeholder="e.g. weekly-sales" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" data-testid="new-artifact-name" />
      <p class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin: var(--sh-space-2) 0 0;">
        Creates <code>artifacts/{{ name.trim() || '&lt;name&gt;' }}/</code> with a small working example to edit.
      </p>
      <p v-if="error" role="alert" data-testid="new-artifact-error" style="font-size: var(--sh-fs-xs); margin-top: var(--sh-space-2); font-weight: 600; color: var(--sh-error);">{{ error }}</p>
      <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-4);">
        <button type="button" class="btn btn-ghost" :disabled="busy" @click="emit('close')">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="busy || !name.trim()" data-testid="new-artifact-create">{{ busy ? 'Creating…' : 'Create' }}</button>
      </div>
    </form>
  </div>
</template>
