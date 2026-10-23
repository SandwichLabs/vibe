<script setup lang="ts">
// NewNotebookDialog: the notebook title prompt, moved out of the old Notebooks
// list. Temporary: Step L3 of docs/plans/library.md replaces it with the type picker.
import { ref, computed, watch } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { useModalKeys } from '../composables/useModal'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: []; created: [id: string] }>()

const title = ref('')
const error = ref('')
const busy = ref(false)
const root = ref<HTMLElement | null>(null)
const open = computed(() => props.visible)
useModalKeys(open, () => { if (!busy.value) emit('close') }, root)
watch(open, (v) => { if (v) { title.value = ''; error.value = ''; busy.value = false } })

async function submit() {
  if (!title.value.trim()) { error.value = 'Title is required'; return }
  busy.value = true
  error.value = ''
  try {
    const nb = await App.CreateNotebook(title.value)
    emit('created', nb.id)
  } catch (e) {
    error.value = 'Failed to create notebook: ' + errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div v-if="visible" class="modal-overlay" role="dialog" aria-modal="true" aria-label="New notebook" @click.self="!busy && emit('close')">
    <form ref="root" class="card card-pad" style="width: 100%; max-width: 384px;" data-testid="new-notebook-dialog" @submit.prevent="submit">
      <h3 class="card-title" style="margin: 0 0 var(--sh-space-4);">New notebook</h3>
      <input v-model="title" placeholder="Notebook title" aria-label="Notebook title" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" class="field-input" :class="{ 'field-input-error': !!error }" data-testid="new-notebook-title" @input="error = ''" />
      <p v-if="error" role="alert" style="font-size: var(--sh-fs-xs); margin-top: var(--sh-space-1); font-weight: 600; color: var(--sh-error);">{{ error }}</p>
      <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-4);">
        <button type="button" class="btn btn-ghost" :disabled="busy" @click="emit('close')">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="busy || !title.trim()" data-testid="new-notebook-create">Create</button>
      </div>
    </form>
  </div>
</template>
