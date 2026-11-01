<script setup lang="ts">
// PageFiles — the files an HTML page is allowed by (#162, H6), read-only: the
// manifest and every file under html/ with its role and size, and the selected
// file's text in CodeMirror (a binary asset shows only its size). What is shown
// is exactly what the hash covers; the parent replaces `files` whenever it
// re-reads them. Every string is text.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { createSlawEditor, type SlawEditorInstance } from '../composables/useSlawEditor'

const props = defineProps<{ manifest: string; files: ArtifactViewFile[] }>()

const all = computed<ArtifactViewFile[]>(() => [
  { path: 'artifact.yaml', role: 'manifest', size: new TextEncoder().encode(props.manifest).length, text: props.manifest },
  ...props.files,
])
const selected = ref('artifact.yaml')
const current = computed(() => all.value.find((f) => f.path === selected.value) ?? all.value[0])
const el = ref<HTMLElement | null>(null)
let editor: SlawEditorInstance | undefined

function langOf(path: string): 'yaml' | 'javascript' | 'json' | 'markdown' {
  if (path.endsWith('.yaml')) return 'yaml'
  if (path.endsWith('.js') || path.endsWith('.mjs')) return 'javascript'
  if (path.endsWith('.json')) return 'json'
  return 'markdown'
}
const sizeText = (n: number) => (n < 1024 ? `${n} B` : `${(n / 1024).toFixed(1)} KB`)

let editorLang = ''
// show puts a text file in the editor; the language is fixed when an editor is
// made, so a file of another kind gets a new one.
function show(f: ArtifactViewFile) {
  if (!el.value || f.text === undefined) return
  const lang = langOf(f.path)
  if (editor && lang === editorLang) {
    if (editor.getValue() !== f.text) editor.setValue(f.text)
    return
  }
  editor?.destroy()
  editor = createSlawEditor(el.value, { value: f.text, lang, readOnly: true })
  editorLang = lang
}
onMounted(() => show(current.value))
onBeforeUnmount(() => editor?.destroy())
// The editor shows the selected file's text; a re-read replaces it.
watch(current, show)
</script>

<template>
  <div class="page-files" data-testid="page-files">
    <ul class="page-files__list" aria-label="Page files">
      <li v-for="f in all" :key="f.path">
        <button
          type="button" class="page-files__item" :class="{ on: f.path === current.path }" :aria-pressed="f.path === current.path"
          :data-testid="`page-file-${f.path}`" @click="selected = f.path"
        >
          <span class="sh-mono page-files__path">{{ f.path }}</span>
          <span class="page-files__meta">{{ f.role }} · {{ sizeText(f.size) }}</span>
        </button>
      </li>
    </ul>
    <div class="page-files__view">
      <div v-show="current.text !== undefined" ref="el" class="page-files__editor" data-testid="page-file-source" />
      <div v-if="current.text === undefined" class="sh-mono page-files__binary" data-testid="page-file-binary">
        {{ current.path }} is a binary file ({{ sizeText(current.size) }}); it is covered by the hash and not shown.
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-files { display: grid; grid-template-columns: minmax(160px, 240px) 1fr; gap: var(--sh-space-3); min-width: 0; }
.page-files__list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sh-space-1); max-height: 60vh; overflow: auto; }
.page-files__item { width: 100%; text-align: left; display: flex; flex-direction: column; gap: 2px; padding: var(--sh-space-2); border: var(--sh-border); border-radius: var(--sh-radius); background: var(--sh-paper); color: inherit; cursor: pointer; }
.page-files__item.on { background: var(--sh-paper-2); border-width: 2px; }
.page-files__path { font-size: var(--sh-fs-xs); overflow-wrap: anywhere; }
.page-files__meta { font-size: var(--sh-fs-xs); color: var(--sh-graphite); }
.page-files__view { min-width: 0; border: var(--sh-border); border-radius: var(--sh-radius); overflow: hidden; min-height: 320px; display: flex; flex-direction: column; }
.page-files__editor { flex: 1; min-height: 320px; max-height: 60vh; overflow: hidden; }
.page-files__editor :deep(.cm-editor) { height: 100%; max-height: 60vh; }
.page-files__binary { padding: var(--sh-space-4); color: var(--sh-graphite); font-size: var(--sh-fs-xs); }
@media (max-width: 640px) { .page-files { grid-template-columns: 1fr; } }
</style>
