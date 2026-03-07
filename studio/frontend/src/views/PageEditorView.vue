<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createVibeEditor, type VibeEditorInstance } from '../composables/useVibeEditor'
import { vibeToolbarButtons, insertSnippet } from '../lib/vibeToolbar'

const route = useRoute()
const router = useRouter()
const page = ref<PageDetail | null>(null)
const title = ref('')
const content = ref('')
const preview = ref('')
const loading = ref(true)
const saving = ref(false)
const previewLoading = ref(false)
const errorMessage = ref('')

const editorEl = ref<HTMLElement | null>(null)
let editorInstance: VibeEditorInstance | null = null

let debounceTimer: ReturnType<typeof setTimeout> | null = null

async function loadPage() {
  loading.value = true
  try {
    const id = route.params.id as string
    page.value = await window.go.main.App.GetPage(id)
    title.value = page.value.title
    content.value = page.value.content
    await renderPreview()
  } catch (e: any) {
    errorMessage.value = 'Failed to load page: ' + (e.message || e)
  }
  loading.value = false
}

async function renderPreview() {
  if (!page.value) return
  previewLoading.value = true
  try {
    preview.value = await window.go.main.App.RenderPageContent(page.value.id, {})
  } catch (e: any) {
    preview.value = `<div class="text-error font-semibold">Render error: ${e.message || e}</div>`
  }
  previewLoading.value = false
}

function debouncedPreview() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(async () => {
    if (page.value) {
      try {
        await window.go.main.App.UpdatePage(page.value.id, title.value, content.value)
        await renderPreview()
      } catch { /* ignore preview errors */ }
    }
  }, 500)
}

watch(content, debouncedPreview)

/** Initialise CodeMirror once the editor container is in the DOM. */
function initEditor() {
  if (!editorEl.value || editorInstance) return
  editorInstance = createVibeEditor(editorEl.value, {
    value: content.value,
    onChange: (val: string) => {
      content.value = val
    },
  })
}

/** Keep the editor in sync when content is loaded externally (page load). */
watch(loading, (isLoading) => {
  if (!isLoading) {
    // Wait one tick for the DOM to render
    setTimeout(() => {
      if (editorInstance) {
        editorInstance.setValue(content.value)
      } else {
        initEditor()
      }
    }, 0)
  }
})

function handleToolbarClick(snippet: string) {
  if (editorInstance) {
    insertSnippet(editorInstance.view, snippet)
  }
}

async function save() {
  if (!page.value) return
  saving.value = true
  try {
    await window.go.main.App.UpdatePage(page.value.id, title.value, content.value)
    await window.go.main.App.SaveFile()
  } catch (e: any) {
    errorMessage.value = 'Save failed: ' + (e.message || e)
  }
  saving.value = false
}

function goBack() {
  router.push('/pages')
}

onMounted(loadPage)

onUnmounted(() => {
  if (debounceTimer) clearTimeout(debounceTimer)
  if (editorInstance) {
    editorInstance.destroy()
    editorInstance = null
  }
})

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Top Toolbar -->
    <div class="px-4 py-2 border-b-3 border-black bg-white flex items-center gap-3">
      <button @click="goBack" class="btn btn-ghost btn-sm">&larr; Back</button>
      <input v-model="title" class="field-input flex-1" placeholder="Page title" />
      <button @click="save" :disabled="saving" class="btn btn-primary">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </div>

    <div v-if="loading" class="p-6 text-muted font-mono text-sm">Loading...</div>

    <!-- Editor + Preview -->
    <div v-else class="flex flex-1 min-h-0">
      <div class="w-1/2 flex flex-col border-r-3 border-black">
        <!-- Source label + Vibe directive toolbar -->
        <div class="px-3 py-1.5 bg-yellow text-xs font-bold font-mono uppercase tracking-wide border-b-3 border-black flex items-center gap-2">
          <span>{{ page?.page_type === 'html' ? 'HTML Source' : 'Markdown Source' }}</span>
          <span class="flex-1"></span>
          <button
            v-for="btn in vibeToolbarButtons"
            :key="btn.id"
            @click="handleToolbarClick(btn.snippet)"
            :title="btn.tooltip"
            class="vibe-toolbar-btn"
            v-html="btn.icon"
          ></button>
        </div>
        <!-- CodeMirror editor container -->
        <div ref="editorEl" class="flex-1 min-h-0 overflow-hidden"></div>
      </div>
      <div class="w-1/2 flex flex-col">
        <div class="px-3 py-1.5 bg-black text-yellow text-xs font-bold font-mono uppercase tracking-wide border-b-3 border-black">
          Preview {{ previewLoading ? '(updating...)' : '' }}
        </div>
        <div class="flex-1 overflow-auto p-4 bg-paper">
          <div v-html="preview" :class="[page?.page_type === 'html' ? '' : 'prose', 'max-w-none vibe-content']"></div>
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
  </div>
</template>

<style scoped>
.vibe-toolbar-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 4px;
  background: #fff;
  border: 2px solid #000;
  cursor: pointer;
  transition: transform 0.08s, box-shadow 0.08s;
}

.vibe-toolbar-btn:hover {
  background: #000;
  color: #FFD700;
  transform: translate(-1px, -1px);
  box-shadow: 2px 2px 0px #000;
}

.vibe-toolbar-btn:active {
  transform: translate(1px, 1px);
  box-shadow: none;
}
</style>
