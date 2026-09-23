<script setup lang="ts">
// NotebooksView — the notebook library list (#66 part A). Mirrors PagesView's
// list/create/delete shape; each row opens NotebookView (#/notebooks/:id).
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'

interface NotebookSummary { id: string; title: string; cellCount: number; updatedAt: string }

const router = useRouter()
const route = useRoute()
const notebooks = ref<NotebookSummary[]>([])
const loading = ref(true)
const showNewModal = ref(false)
const newTitle = ref('')
const titleError = ref(false)
const errorMessage = ref('')
const showConfirmDelete = ref(false)
const pendingDeleteId = ref<string | null>(null)
const hoveredId = ref<string | null>(null)
const importing = ref(false)

async function loadNotebooks() {
  loading.value = true
  try {
    notebooks.value = (await window.go.main.App.ListNotebooks()) || []
  } catch { /* ignore */ }
  loading.value = false
}

function openNotebook(id: string) {
  router.push(`/notebooks/${id}`)
}

async function createNotebook() {
  if (!newTitle.value.trim()) {
    titleError.value = true
    return
  }
  titleError.value = false
  try {
    const nb = await window.go.main.App.CreateNotebook(newTitle.value)
    newTitle.value = ''
    showNewModal.value = false
    await loadNotebooks()
    openNotebook(nb.id)
  } catch (e: any) {
    errorMessage.value = 'Failed to create notebook: ' + (e.message || e)
  }
}

function deleteNotebook(id: string) {
  pendingDeleteId.value = id
  showConfirmDelete.value = true
}

async function confirmDeleteNotebook() {
  const id = pendingDeleteId.value
  showConfirmDelete.value = false
  pendingDeleteId.value = null
  if (!id) return
  try {
    await window.go.main.App.DeleteNotebook(id)
    await loadNotebooks()
  } catch (e: any) {
    errorMessage.value = 'Failed to delete notebook: ' + (e.message || e)
  }
}

async function importNotebook() {
  importing.value = true
  try {
    const path = await window.go.main.App.SelectNotebookFile()
    if (!path) { importing.value = false; return }
    const result = await window.go.main.App.ImportNotebook(path)
    await loadNotebooks()
    if (result?.warnings?.length) {
      errorMessage.value = `Imported with ${result.warnings.length} warning(s): ${result.warnings[0]}`
    }
    if (result?.notebook?.id) openNotebook(result.notebook.id)
  } catch (e: any) {
    errorMessage.value = 'Failed to import notebook: ' + (e.message || e)
  }
  importing.value = false
}

function formatDate(iso: string): string {
  try { return new Date(iso).toLocaleString() } catch { return iso }
}

onMounted(async () => {
  await loadNotebooks()
  if (route.query.new === '1') {
    showNewModal.value = true
    router.replace('/notebooks')
  }
})
</script>

<template>
  <div class="col" style="height: 100%; gap: 0;">
    <div class="row" style="justify-content: space-between; padding: var(--sh-space-4) var(--sh-space-6); border-bottom: var(--sh-border);">
      <h2 class="sh-label" style="margin: 0;">Notebooks</h2>
      <div class="row" style="gap: var(--sh-space-2);">
        <button @click="importNotebook" :disabled="importing" class="btn btn-ghost btn-sm"><Icon name="upload" :size="14" /> Import</button>
        <button @click="showNewModal = true" class="btn btn-primary btn-sm" data-testid="new-notebook"><Icon name="plus" :size="14" /> New Notebook</button>
      </div>
    </div>

    <div style="flex: 1; overflow: auto; padding: var(--sh-space-6);">
      <div v-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>
      <div v-else-if="notebooks.length === 0" class="col" style="align-items: center; gap: var(--sh-space-3); margin-top: var(--sh-space-10); text-align: center;">
        <Icon name="layers" :size="32" style="color: var(--sh-graphite);" />
        <div class="sh-mono" style="color: var(--sh-graphite);">No notebooks yet</div>
        <button @click="showNewModal = true" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> New Notebook</button>
      </div>
      <div v-else class="col" style="gap: var(--sh-space-2);">
        <div
          v-for="nb in notebooks"
          :key="nb.id"
          @click="openNotebook(nb.id)"
          @mouseenter="hoveredId = nb.id"
          @mouseleave="hoveredId = null"
          class="card card-pad row"
          data-testid="notebook-row"
          :data-notebook-id="nb.id"
          style="justify-content: space-between; cursor: pointer; align-items: center;"
        >
          <div class="row" style="gap: var(--sh-space-3); align-items: center; min-width: 0;">
            <Icon name="layers" :size="16" style="color: var(--sh-teal); flex: none;" />
            <div style="min-width: 0;">
              <div style="font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ nb.title || 'Untitled notebook' }}</div>
              <div class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite);">
                {{ nb.cellCount }} cell{{ nb.cellCount === 1 ? '' : 's' }} · updated {{ formatDate(nb.updatedAt) }}
              </div>
            </div>
          </div>
          <div v-show="hoveredId === nb.id" class="row" style="gap: var(--sh-space-1);">
            <button @click.stop="deleteNotebook(nb.id)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="showNewModal" class="modal-overlay" @click.self="showNewModal = false">
      <div class="card card-pad" style="width: 100%; max-width: 384px;">
        <h3 class="card-title" style="margin: 0 0 var(--sh-space-4);">New notebook</h3>
        <input v-model="newTitle" placeholder="Notebook title" class="field-input" :class="{ 'field-input-error': titleError }" @keyup.enter="createNotebook" @input="titleError = false" autofocus />
        <p v-if="titleError" style="font-size: var(--sh-fs-xs); margin-top: var(--sh-space-1); font-weight: 600; color: var(--sh-error);">Title is required</p>
        <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-4);">
          <button @click="showNewModal = false" class="btn btn-ghost">Cancel</button>
          <button @click="createNotebook" :disabled="!newTitle.trim()" class="btn btn-primary">Create</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDelete"
      title="Delete Notebook"
      message="Delete this notebook? This cannot be undone."
      @confirm="confirmDeleteNotebook"
      @cancel="showConfirmDelete = false; pendingDeleteId = null"
    />

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>
