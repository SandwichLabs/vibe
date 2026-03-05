<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

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
    preview.value = `<div class="text-red-600">Render error: ${e.message || e}</div>`
  }
  previewLoading.value = false
}

function debouncedPreview() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(async () => {
    if (page.value) {
      // Save first so preview reflects latest content.
      try {
        await window.go.main.App.UpdatePage(page.value.id, title.value, content.value)
        await renderPreview()
      } catch { /* ignore preview errors */ }
    }
  }, 500)
}

watch(content, debouncedPreview)

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

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Toolbar -->
    <div class="px-4 py-2 border-b border-gray-200 bg-white flex items-center gap-3">
      <button @click="goBack" class="text-gray-500 hover:text-gray-700 text-sm">&larr; Back</button>
      <input v-model="title" class="flex-1 px-2 py-1 border rounded text-sm" placeholder="Page title" />
      <button @click="save" :disabled="saving" class="px-4 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </div>

    <div v-if="loading" class="p-6 text-gray-500">Loading...</div>

    <!-- Editor + Preview -->
    <div v-else class="flex flex-1 min-h-0">
      <div class="w-1/2 flex flex-col border-r border-gray-200">
        <div class="px-3 py-1.5 bg-gray-100 text-xs font-medium text-gray-500 border-b border-gray-200">Markdown Source</div>
        <textarea
          v-model="content"
          class="flex-1 p-4 font-mono text-sm resize-none focus:outline-none"
          placeholder="Write your page content here..."
          spellcheck="false"
        ></textarea>
      </div>
      <div class="w-1/2 flex flex-col">
        <div class="px-3 py-1.5 bg-gray-100 text-xs font-medium text-gray-500 border-b border-gray-200">
          Preview {{ previewLoading ? '(updating...)' : '' }}
        </div>
        <div class="flex-1 overflow-auto p-4">
          <div v-html="preview" class="prose max-w-none vibe-content"></div>
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="fixed bottom-4 right-4 bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg text-sm z-50">
      {{ errorMessage }}
    </div>
  </div>
</template>
