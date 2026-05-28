<script setup lang="ts">
import { ref, nextTick, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createVibeEditor, type VibeEditorInstance } from '../composables/useVibeEditor'
import Icon from '../components/Icon.vue'
import { vibeToolbarButtons, insertSnippet } from '../lib/vibeToolbar'
import { execScripts } from '../lib/execScripts'

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
const previewEl = ref<HTMLElement | null>(null)
let editorInstance: VibeEditorInstance | null = null

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let renderGeneration = 0
let rendering = false

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
  if (!page.value || rendering) return
  previewLoading.value = true
  rendering = true
  const gen = ++renderGeneration
  try {
    await window.go.main.App.CancelRender()
    const html = await window.go.main.App.RenderPageContent(page.value.id, {})
    if (gen === renderGeneration) {
      preview.value = html
    }
  } catch (e: any) {
    if (gen === renderGeneration) {
      preview.value = `<div class="text-error font-semibold">Render error: ${e.message || e}</div>`
    }
  }
  previewLoading.value = false
  rendering = false
  // If content changed during render, do one more render
  if (renderGeneration > gen) {
    renderPreview()
  }
}

function debouncedSaveAndRender() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(async () => {
    if (page.value) {
      try {
        await window.go.main.App.UpdatePage(page.value.id, title.value, content.value)
      } catch { /* ignore save errors during typing */ }
      renderPreview()
    }
  }, 1000)
}

watch(content, debouncedSaveAndRender)

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

watch(preview, () => {
  nextTick(() => execScripts(previewEl.value))
})

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Top Toolbar -->
    <div class="px-4 py-2 border-b-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex items-center gap-3">
      <button @click="goBack" class="btn btn-ghost btn-sm"><Icon name="arrowLeft" :size="14" /> Back</button>
      <input v-model="title" class="field-input flex-1" placeholder="Page title" />
      <button @click="save" :disabled="saving" class="btn btn-primary">
        <Icon name="save" :size="16" /> {{ saving ? 'Saving…' : 'Save' }}
      </button>
    </div>

    <div v-if="loading" class="p-6 sh-mono text-[var(--sh-graphite)]">Loading…</div>

    <!-- Editor + Preview -->
    <div v-else class="flex flex-1 min-h-0">
      <div class="w-1/2 flex flex-col border-r-2 border-[var(--sh-ink)]">
        <!-- Source label + Vibe directive toolbar -->
        <div class="px-3 py-2 border-b-2 border-[var(--sh-ink)] flex items-center gap-2" style="background: var(--sh-paper-2);">
          <span class="sh-label" style="margin: 0;">{{ page?.page_type === 'html' ? 'HTML Source' : 'Markdown Source' }}</span>
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
        <div class="px-3 py-2 border-b-2 border-[var(--sh-ink)] sh-label" style="background: var(--sh-ink); color: var(--sh-paper); margin: 0;">
          Preview {{ previewLoading ? '· updating…' : '' }}
        </div>
        <div class="flex-1 overflow-auto p-5" style="background: var(--sh-paper);">
          <div ref="previewEl" v-html="preview" :class="[page?.page_type === 'html' ? '' : 'prose', 'max-w-none vibe-content']"></div>
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
  background: var(--sh-surface);
  border: 1.5px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  cursor: pointer;
  transition: transform 0.08s, box-shadow 0.08s;
}

.vibe-toolbar-btn:hover {
  background: var(--sh-teal);
  color: var(--sh-paper);
  transform: translate(-1px, -1px);
  box-shadow: 2px 2px 0 0 var(--sh-ink);
}

.vibe-toolbar-btn:active {
  transform: translate(1px, 1px);
  box-shadow: none;
}
</style>
