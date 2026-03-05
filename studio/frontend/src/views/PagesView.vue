<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'

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
    <div class="w-72 border-r-3 border-black bg-white flex flex-col">
      <div class="p-3 border-b-3 border-black flex items-center justify-between">
        <h2 class="font-bold text-sm uppercase font-mono tracking-wide">Pages</h2>
        <button @click="showNewModal = true" class="btn btn-primary btn-sm">+ New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 text-muted font-mono text-sm">Loading...</div>
        <div v-else-if="pages.length === 0" class="p-4 text-muted text-sm text-center font-mono">No pages yet</div>
        <div
          v-for="page in pages"
          :key="page.id"
          @click="selectedId = page.id"
          class="px-3 py-2.5 cursor-pointer border-b-2 border-black/10 flex items-center justify-between group"
          :class="selectedId === page.id ? 'bg-yellow border-l-4 border-l-black' : 'hover:bg-cream'"
        >
          <div class="min-w-0">
            <div class="text-sm font-bold truncate">{{ page.title || 'Untitled' }}</div>
            <div class="badge mt-0.5">{{ page.page_type }}</div>
          </div>
          <div class="hidden group-hover:flex gap-1">
            <button @click.stop="editPage(page.id)" class="btn btn-ghost btn-sm">Edit</button>
            <button @click.stop="deletePage(page.id)" class="btn btn-ghost btn-sm text-error">Del</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Content area -->
    <div class="flex-1 overflow-auto p-6">
      <div v-if="renderLoading" class="text-muted font-mono text-sm">Rendering...</div>
      <div v-else-if="!selectedId" class="text-muted text-center mt-20 font-mono">Select a page to view</div>
      <div v-else v-html="renderedContent" :class="[selectedPageType === 'html' ? '' : 'prose', 'max-w-none vibe-content']"></div>
    </div>

    <!-- New page modal -->
    <div v-if="showNewModal" class="modal-overlay" @click.self="showNewModal = false; newPageType = 'markdown'">
      <div class="card w-full max-w-sm p-6">
        <h3 class="text-lg font-bold mb-4">New Page</h3>
        <input v-model="newTitle" placeholder="Page title" class="field-input" :class="{ 'field-input-error': titleError }" @keyup.enter="createPage" @input="titleError = false" autofocus />
        <p v-if="titleError" class="text-error text-xs mt-1 font-semibold">Title is required</p>
        <div class="flex gap-4 mt-3">
          <label class="flex items-center gap-1.5 text-sm cursor-pointer font-medium">
            <input type="radio" v-model="newPageType" value="markdown" class="accent-yellow" />
            Markdown
          </label>
          <label class="flex items-center gap-1.5 text-sm cursor-pointer font-medium">
            <input type="radio" v-model="newPageType" value="html" class="accent-yellow" />
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
