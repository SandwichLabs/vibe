<script setup lang="ts">
import { ref, computed, nextTick, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import { execScripts } from '../lib/execScripts'

const router = useRouter()
const route = useRoute()
const pages = ref<PageSummary[]>([])
const selectedId = ref<string | null>(null)
const renderedContent = ref('')
const loading = ref(true)
const renderLoading = ref(false)
const showNewModal = ref(false)
const newTitle = ref('')
const titleError = ref(false)
const errorMessage = ref('')
const newPageType = ref<'markdown' | 'html'>('markdown')
const showConfirmDelete = ref(false)
const pendingDeleteId = ref<string | null>(null)
const previewEl = ref<HTMLElement | null>(null)

const selectedPageType = computed(() => {
  const p = pages.value.find(p => p.id === selectedId.value)
  return p?.page_type ?? 'markdown'
})

async function loadPages() {
  loading.value = true
  try {
    pages.value = await window.go.main.App.ListPages()
    if (pages.value.length > 0 && !selectedId.value) {
      selectedId.value = pages.value[0].id
    }
  } catch { /* ignore */ }
  loading.value = false
}

async function renderPage(id: string) {
  renderLoading.value = true
  try {
    renderedContent.value = await window.go.main.App.RenderPageContent(id, {})
  } catch (e: any) {
    renderedContent.value = `<div class="text-error font-semibold">Error: ${e.message || e}</div>`
  }
  renderLoading.value = false
}

watch(selectedId, (id) => {
  if (id) renderPage(id)
})

watch(renderedContent, () => {
  nextTick(() => execScripts(previewEl.value))
})

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})

async function createPage() {
  if (!newTitle.value.trim()) {
    titleError.value = true
    return
  }
  titleError.value = false
  try {
    const id = newPageType.value === 'html'
      ? await window.go.main.App.CreateHTMLPage(newTitle.value, '')
      : await window.go.main.App.CreatePage(newTitle.value, '')
    newTitle.value = ''
    newPageType.value = 'markdown'
    showNewModal.value = false
    await loadPages()
    selectedId.value = id
  } catch (e: any) {
    errorMessage.value = 'Failed to create page: ' + (e.message || e)
  }
}

function deletePage(id: string) {
  pendingDeleteId.value = id
  showConfirmDelete.value = true
}

async function confirmDeletePage() {
  const id = pendingDeleteId.value
  showConfirmDelete.value = false
  pendingDeleteId.value = null
  if (!id) return
  try {
    await window.go.main.App.DeletePage(id)
    if (selectedId.value === id) {
      selectedId.value = null
      renderedContent.value = ''
    }
    await loadPages()
  } catch (e: any) {
    errorMessage.value = 'Failed to delete page: ' + (e.message || e)
  }
}

function editPage(id: string) {
  router.push(`/pages/${id}/edit`)
}

onMounted(async () => {
  await loadPages()
  if (route.query.new === '1') {
    showNewModal.value = true
    router.replace('/pages')
  }
})
</script>

<template>
  <div class="flex h-full">
    <!-- Page list -->
    <div class="w-72 border-r-2 border-[var(--sh-ink)] bg-[var(--sh-paper-2)] flex flex-col">
      <div class="px-4 py-3 border-b-2 border-[var(--sh-ink)] flex items-center justify-between">
        <h2 class="sh-label" style="margin: 0;">Pages</h2>
        <button @click="showNewModal = true" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 sh-mono text-[var(--sh-graphite)]">Loading…</div>
        <div v-else-if="pages.length === 0" class="p-4 sh-mono text-[var(--sh-graphite)] text-center">No pages yet</div>
        <div
          v-for="page in pages"
          :key="page.id"
          @click="selectedId = page.id"
          class="px-4 py-2.5 cursor-pointer border-b border-[rgba(31,35,48,0.12)] flex items-center justify-between group"
          :class="selectedId === page.id ? 'row-selected' : 'hover:bg-[rgba(31,35,48,0.04)]'"
        >
          <div class="min-w-0">
            <div class="text-sm font-semibold truncate">{{ page.title || 'Untitled' }}</div>
            <div class="badge badge--graphite mt-1">{{ page.page_type }}</div>
          </div>
          <div class="hidden group-hover:flex gap-1">
            <button @click.stop="editPage(page.id)" class="btn btn-ghost btn-sm"><Icon name="edit" :size="14" /></button>
            <button @click.stop="deletePage(page.id)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
          </div>
        </div>
      </div>
    </div>

    <!-- Content area -->
    <div class="flex-1 overflow-auto p-8">
      <div v-if="renderLoading" class="sh-mono text-[var(--sh-graphite)]">Rendering…</div>
      <div v-else-if="!selectedId" class="sh-mono text-[var(--sh-graphite)] text-center mt-20">Select a page to view</div>
      <div v-else ref="previewEl" v-html="renderedContent" :class="[selectedPageType === 'html' ? '' : 'prose', 'max-w-none vibe-content']"></div>
    </div>

    <!-- New page modal -->
    <div v-if="showNewModal" class="modal-overlay" @click.self="showNewModal = false; newPageType = 'markdown'">
      <div class="card card-pad w-full max-w-sm">
        <h3 class="card-title mb-4">New page</h3>
        <input v-model="newTitle" placeholder="Page title" class="field-input" :class="{ 'field-input-error': titleError }" @keyup.enter="createPage" @input="titleError = false" autofocus />
        <p v-if="titleError" class="text-xs mt-1 font-semibold" style="color: var(--sh-error);">Title is required</p>
        <div class="flex gap-4 mt-3">
          <label class="flex items-center gap-1.5 text-sm cursor-pointer font-medium">
            <input type="radio" v-model="newPageType" value="markdown" style="accent-color: var(--sh-teal);" />
            Markdown
          </label>
          <label class="flex items-center gap-1.5 text-sm cursor-pointer font-medium">
            <input type="radio" v-model="newPageType" value="html" style="accent-color: var(--sh-teal);" />
            HTML
          </label>
        </div>
        <div class="flex gap-3 justify-end mt-4">
          <button @click="showNewModal = false; newPageType = 'markdown'" class="btn btn-ghost">Cancel</button>
          <button @click="createPage" :disabled="!newTitle.trim()" class="btn btn-primary">Create</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDelete"
      title="Delete Page"
      message="Delete this page? This cannot be undone."
      @confirm="confirmDeletePage"
      @cancel="showConfirmDelete = false; pendingDeleteId = null"
    />

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
  </div>
</template>
