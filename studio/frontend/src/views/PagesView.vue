<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
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
const showConfirmDelete = ref(false)
const pendingDeleteId = ref<string | null>(null)

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
    renderedContent.value = `<div class="text-red-600">Error: ${e.message || e}</div>`
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
    const id = await window.go.main.App.CreatePage(newTitle.value, '')
    newTitle.value = ''
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
    <div class="w-72 border-r border-gray-200 bg-white flex flex-col">
      <div class="p-3 border-b border-gray-200 flex items-center justify-between">
        <h2 class="font-semibold text-sm">Pages</h2>
        <button @click="showNewModal = true" class="text-blue-600 hover:text-blue-700 text-sm font-medium">+ New</button>
      </div>
      <div class="flex-1 overflow-auto">
        <div v-if="loading" class="p-4 text-gray-500 text-sm">Loading...</div>
        <div v-else-if="pages.length === 0" class="p-4 text-gray-400 text-sm text-center">No pages yet</div>
        <div
          v-for="page in pages"
          :key="page.id"
          @click="selectedId = page.id"
          class="px-3 py-2.5 cursor-pointer border-b border-gray-100 hover:bg-gray-50 flex items-center justify-between group"
          :class="{ 'bg-blue-50 border-l-2 border-l-blue-500': selectedId === page.id }"
        >
          <div class="min-w-0">
            <div class="text-sm font-medium truncate">{{ page.title || 'Untitled' }}</div>
            <div class="text-xs text-gray-400">{{ page.page_type }}</div>
          </div>
          <div class="hidden group-hover:flex gap-1">
            <button @click.stop="editPage(page.id)" class="text-xs text-blue-500 hover:text-blue-700">Edit</button>
            <button @click.stop="deletePage(page.id)" class="text-xs text-red-500 hover:text-red-700">Del</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Content area -->
    <div class="flex-1 overflow-auto p-6">
      <div v-if="renderLoading" class="text-gray-500">Rendering...</div>
      <div v-else-if="!selectedId" class="text-gray-400 text-center mt-20">Select a page to view</div>
      <div v-else v-html="renderedContent" class="prose max-w-none vibe-content"></div>
    </div>

    <!-- New page modal -->
    <div v-if="showNewModal" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50" @click.self="showNewModal = false">
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-sm">
        <h3 class="text-lg font-semibold mb-4">New Page</h3>
        <input v-model="newTitle" placeholder="Page title" class="w-full px-3 py-2 border rounded-lg" :class="{ 'border-red-500': titleError }" @keyup.enter="createPage" @input="titleError = false" autofocus />
        <p v-if="titleError" class="text-red-500 text-xs mt-1">Title is required</p>
        <div class="flex gap-3 justify-end mt-4">
          <button @click="showNewModal = false" class="px-4 py-2 text-gray-600 hover:text-gray-800">Cancel</button>
          <button @click="createPage" :disabled="!newTitle.trim()" class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">Create</button>
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

    <div v-if="errorMessage" class="fixed bottom-4 right-4 bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg text-sm z-50">
      {{ errorMessage }}
    </div>
  </div>
</template>

<style>
.vibe-content .vibe-table-container { overflow-x: auto; margin: 1rem 0; }
.vibe-content .vibe-table { width: 100%; border-collapse: collapse; }
.vibe-content .vibe-table th,
.vibe-content .vibe-table td { padding: 0.5rem 0.75rem; border: 1px solid #e5e7eb; text-align: left; }
.vibe-content .vibe-table thead th { background: #f9fafb; font-weight: 600; }
.vibe-content .vibe-metric { display: inline-block; padding: 1rem 1.5rem; background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 0.5rem; text-align: center; margin: 0.5rem; }
.vibe-content .vibe-metric-value { font-size: 2rem; font-weight: 700; color: #2563eb; }
.vibe-content .vibe-metric-label { font-size: 0.875rem; color: #6b7280; margin-top: 0.25rem; }
.vibe-content .vibe-chart-container { margin: 1rem 0; }
</style>
