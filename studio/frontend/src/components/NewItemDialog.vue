<script setup lang="ts">
// NewItemDialog: New for the Library (docs/plans/library.md L6). Step 1 asks
// what kind (TypePicker), step 2 asks for a title (notebook) or a name
// (artifact kinds), then lands on the new item. useNewItem owns the state.
import { ref, computed, watch, nextTick } from 'vue'
import TypePicker from './TypePicker.vue'
import { useModalKeys } from '../composables/useModal'
import { useNewItem } from '../composables/useNewItem'
import type { LibraryType } from '../lib/libraryRoutes'
import { MAX_TITLE, STEP2_TITLE, needsAllow } from '../lib/newItem'

const props = defineProps<{ visible: boolean; initialType?: LibraryType | null }>()
const emit = defineEmits<{ close: []; created: [to: string] }>()

const open = computed(() => props.visible)
const preselect = computed(() => props.initialType)
const m = useNewItem(open, preselect, (to) => emit('created', to))
const root = ref<HTMLElement | null>(null)
const picker = ref<InstanceType<typeof TypePicker> | null>(null)
const field = ref<HTMLInputElement | null>(null)
const close = () => { if (!m.busy.value) emit('close') }
useModalKeys(open, close, root, () => root.value?.querySelector<HTMLElement>('[role="radio"][tabindex="0"]'))

watch(m.step, async (s) => {
  await nextTick()
  if (s === 'name') field.value?.focus()
  else picker.value?.focusSelected()
})
</script>

<template>
  <div v-if="visible" class="modal-overlay" role="dialog" aria-modal="true" :aria-label="m.step.value === 'type' ? 'New' : STEP2_TITLE[m.type.value]" @click.self="close">
    <div ref="root" class="card card-pad" style="width: 100%; max-width: 860px;" data-testid="new-item-dialog">
      <template v-if="m.step.value === 'type'">
        <h3 class="card-title" style="margin: 0 0 var(--sh-space-3);">New</h3>
        <TypePicker ref="picker" v-model="m.type.value" v-model:with-script="m.withScript.value" v-model:advanced="m.advanced.value" @next="m.next" />
        <p class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin: var(--sh-space-3) 0 0; text-align: right;">←/→ choose · Enter next</p>
        <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-3);">
          <button type="button" class="btn btn-ghost" data-testid="new-cancel" @click="close">Cancel</button>
          <button type="button" class="btn btn-primary" data-testid="new-next" @click="m.next">Next</button>
        </div>
      </template>
      <form v-else style="max-width: 440px;" data-testid="new-item-form" @submit.prevent="m.submit">
        <h3 class="card-title" style="margin: 0 0 var(--sh-space-3);">{{ STEP2_TITLE[m.type.value] }}</h3>
        <label class="sh-label" for="new-item-field" style="margin: 0;">{{ m.isNotebook.value ? 'Title' : 'Name' }}</label>
        <input id="new-item-field" ref="field" v-model="m.value.value" class="field-input" :class="{ 'field-input-error': !!m.error.value }" :placeholder="m.isNotebook.value ? 'Notebook title' : 'e.g. weekly-sales'"
          autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" :aria-invalid="!!m.error.value" :aria-describedby="m.error.value ? 'new-item-error new-item-creates' : 'new-item-creates'" data-testid="new-item-field" @input="m.error.value = ''" />
        <p v-if="m.warnLong.value" class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin: var(--sh-space-1) 0 0;" data-testid="new-item-long">Titles are cut to {{ MAX_TITLE }} characters.</p>
        <p class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin: var(--sh-space-2) 0 0;" id="new-item-creates" data-testid="new-item-creates">
          <template v-if="m.isNotebook.value">Creates a notebook in <code>notebooks/</code></template>
          <template v-else>Creates <code>artifacts/{{ m.slug.value }}/</code> with a working example.</template>
        </p>
        <p v-if="needsAllow(m.type.value)" class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin: var(--sh-space-1) 0 0;" data-testid="new-item-allow">It opens as source with an <b>Allow</b> button.</p>
        <p v-if="m.error.value" id="new-item-error" role="alert" data-testid="new-item-error" style="font-size: var(--sh-fs-xs); margin-top: var(--sh-space-2); font-weight: 600; color: var(--sh-error);">{{ m.error.value }}</p>
        <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-4);">
          <button type="button" class="btn btn-ghost" :disabled="m.busy.value" data-testid="new-back" @click="m.back">Back</button>
          <button type="button" class="btn btn-ghost" :disabled="m.busy.value" @click="close">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="m.busy.value || !m.value.value.trim()" data-testid="new-create">{{ m.busy.value ? 'Creating…' : 'Create' }}</button>
        </div>
      </form>
    </div>
  </div>
</template>
