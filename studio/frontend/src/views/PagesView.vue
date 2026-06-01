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
const hoveredId = ref<string | null>(null)

const selectedPageType = computed(() => {
  const p = pages.value.find(p => p.id === selectedId.value)
  return p?.page_type ?? 'markdown'
})

// A page entry is a banded report (rolled up into this list) when its
// page_type is 'report'. Reports render via a different binding and edit in the
// Report view, so several handlers branch on this.
function isReport(p: PageSummary): boolean {
  return p.page_type === 'report'
}
const selectedIsReport = computed(() => {
  const p = pages.value.find(p => p.id === selectedId.value)
  return !!p && isReport(p)
})

async function loadPages() {
  loading.value = true
  try {
    // Reports roll up into the Pages list so agent-authored reports are
    // discoverable here (not only in the Report view). We tag them with
    // page_type 'report' and prefix the id so handlers can tell them apart and
    // route/render them correctly. Pages come first, then reports.
    const [pageList, reportList] = await Promise.all([
      window.go.main.App.ListPages(),
      window.go.main.App.ListReports().catch(() => []),
    ])
    const reportRows: PageSummary[] = (reportList || []).map((r: any) => ({
      id: `report:${r.id}`,
      title: r.title,
      page_type: 'report',
      sort_order: 0,
    }))
    pages.value = [...pageList, ...reportRows]
    if (pages.value.length > 0 && !selectedId.value) {
      selectedId.value = pages.value[0].id
    }
  } catch { /* ignore */ }
  loading.value = false
}

// reportIdOf strips the 'report:' prefix used to namespace report rows.
function reportIdOf(id: string): string {
  return id.startsWith('report:') ? id.slice('report:'.length) : id
}

async function renderPage(id: string) {
  renderLoading.value = true
  try {
    renderedContent.value = id.startsWith('report:')
      ? await window.go.main.App.RenderReportContent(reportIdOf(id))
      : await window.go.main.App.RenderPageContent(id, {})
  } catch (e: any) {
    renderedContent.value = `<div style="color: var(--sh-error); font-weight: 600;">Error: ${e.message || e}</div>`
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
  // Reports are edited in the Report view, not the page editor.
  if (id.startsWith('report:')) {
    router.push({ path: '/reports', query: { id: reportIdOf(id) } })
    return
  }
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
  <div class="row" style="height: 100%; align-items: stretch; gap: 0;">
    <!-- Page list -->
    <div class="col" style="width: 288px; border-right: var(--sh-border); background: var(--sh-paper-2); gap: 0;">
      <div class="row" style="justify-content: space-between; padding: var(--sh-space-3) var(--sh-space-4); border-bottom: var(--sh-border);">
        <h2 class="sh-label" style="margin: 0;">Pages</h2>
        <button @click="showNewModal = true" class="btn btn-primary btn-sm"><Icon name="plus" :size="14" /> New</button>
      </div>
      <div style="flex: 1; overflow: auto;">
        <div v-if="loading" class="sh-mono" style="padding: var(--sh-space-4); color: var(--sh-graphite);">Loading…</div>
        <div v-else-if="pages.length === 0" class="sh-mono" style="padding: var(--sh-space-4); color: var(--sh-graphite); text-align: center;">No pages yet</div>
        <div
          v-for="page in pages"
          :key="page.id"
          @click="selectedId = page.id"
          @mouseenter="hoveredId = page.id"
          @mouseleave="hoveredId = null"
          class="row"
          data-testid="page-row"
          :data-page-id="page.id"
          :class="{ 'row-selected': selectedId === page.id }"
          :style="{
            justifyContent: 'space-between',
            padding: 'var(--sh-space-2) var(--sh-space-4)',
            cursor: 'pointer',
            borderBottom: 'var(--sh-border-thin)',
            background: selectedId !== page.id && hoveredId === page.id ? 'rgba(31,35,48,0.04)' : '',
          }"
        >
          <div style="min-width: 0;">
            <div class="row" style="gap: var(--sh-space-1); align-items: center; min-width: 0;">
              <Icon :name="isReport(page) ? 'barChart' : 'fileText'" :size="13" :style="{ color: isReport(page) ? 'var(--sh-teal)' : 'var(--sh-graphite)', flex: 'none' }" />
              <div style="font-size: var(--sh-fs-sm); font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ page.title || 'Untitled' }}</div>
            </div>
            <div class="badge" :class="isReport(page) ? 'badge--teal' : 'badge--graphite'" style="margin-top: var(--sh-space-1);">{{ isReport(page) ? 'report' : page.page_type }}</div>
          </div>
          <div v-show="hoveredId === page.id" class="row" style="gap: var(--sh-space-1);">
            <button @click.stop="editPage(page.id)" class="btn btn-ghost btn-sm" :title="isReport(page) ? 'Open in Report view' : 'Edit page'"><Icon :name="isReport(page) ? 'arrowRight' : 'edit'" :size="14" /></button>
            <button v-if="!isReport(page)" @click.stop="deletePage(page.id)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);"><Icon name="trash" :size="14" /></button>
          </div>
        </div>
      </div>
    </div>

    <!-- Content area -->
    <div style="flex: 1; overflow: auto; padding: var(--sh-space-6);">
      <div v-if="renderLoading" class="sh-mono" style="color: var(--sh-graphite);">Rendering…</div>
      <div v-else-if="!selectedId" class="sh-mono" style="color: var(--sh-graphite); text-align: center; margin-top: var(--sh-space-10);">Select a page to view</div>
      <div v-else ref="previewEl" v-html="renderedContent" :class="[selectedPageType === 'html' ? '' : 'prose', 'vibe-content']" style="max-width: none;"></div>
    </div>

    <!-- New page modal -->
    <div v-if="showNewModal" class="modal-overlay" @click.self="showNewModal = false; newPageType = 'markdown'">
      <div class="card card-pad" style="width: 100%; max-width: 384px;">
        <h3 class="card-title" style="margin: 0 0 var(--sh-space-4);">New page</h3>
        <input v-model="newTitle" placeholder="Page title" class="field-input" :class="{ 'field-input-error': titleError }" @keyup.enter="createPage" @input="titleError = false" autofocus />
        <p v-if="titleError" style="font-size: var(--sh-fs-xs); margin-top: var(--sh-space-1); font-weight: 600; color: var(--sh-error);">Title is required</p>
        <div class="row" style="gap: var(--sh-space-4); margin-top: var(--sh-space-3);">
          <label class="row" style="gap: var(--sh-space-1); font-size: var(--sh-fs-sm); cursor: pointer; font-weight: 500;">
            <input type="radio" v-model="newPageType" value="markdown" style="accent-color: var(--sh-teal);" />
            Markdown
          </label>
          <label class="row" style="gap: var(--sh-space-1); font-size: var(--sh-fs-sm); cursor: pointer; font-weight: 500;">
            <input type="radio" v-model="newPageType" value="html" style="accent-color: var(--sh-teal);" />
            HTML
          </label>
        </div>
        <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-4);">
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
