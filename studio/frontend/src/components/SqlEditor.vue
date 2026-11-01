<script setup lang="ts">
// SqlEditor — the Query Explorer's CodeMirror editor. v-model is the SQL text;
// `run` fires on Ctrl/Cmd+Enter (which keeps priority over the completion
// list's own keys); `connection` is the Run-on choice completion resolves
// unqualified names against. Notebook SQL cells mount createSlawEditor
// themselves (useNotebookCells) and share the same completion source.
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { createSlawEditor, type SlawEditorInstance } from '../composables/useSlawEditor'
import { closeCompletion } from '@codemirror/autocomplete'
import { sqlCompletion } from '../lib/sqlComplete'

const props = defineProps<{ modelValue: string; connection: string; placeholder?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; run: [] }>()

const el = ref<HTMLElement | null>(null)
let editor: SlawEditorInstance | null = null

onMounted(() => {
  if (!el.value) return
  editor = createSlawEditor(el.value, {
    value: props.modelValue,
    lang: 'sql',
    placeholder: props.placeholder,
    onChange: (v) => { if (v !== props.modelValue) emit('update:modelValue', v) },
    extraKeymap: [{ key: 'Mod-Enter', run: (view) => { closeCompletion(view); emit('run'); return true } }],
    extensions: sqlCompletion({ connection: () => props.connection }),
  })
})

// Saved-query loads, New query and the ?sql= route intent assign the parent's
// text: push it into the editor unless the editor is where it came from.
watch(() => props.modelValue, (v) => {
  if (editor && v !== editor.getValue()) editor.setValue(v, { history: false })
})

onBeforeUnmount(() => editor?.destroy())
</script>

<template>
  <div ref="el" class="sql-editor" data-testid="sql-input"></div>
</template>

<style scoped>
.sql-editor {
  height: 180px;
  border: 2px solid var(--slaw-border);
  overflow: hidden;
}
.sql-editor :deep(.cm-editor) { height: 100%; }
</style>
