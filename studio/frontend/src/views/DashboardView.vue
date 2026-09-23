<script setup lang="ts">
// Studio Home / Dashboard. #69 retired the page system (and with it, the
// `_dashboard` system page + RenderPageContent pipeline this view used to
// render through) — the home screen is now backed directly by GetDashboard's
// live table/notebook/query counts, refreshed on "data:changed" the same way
// the old page-rendered version was.
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { EventsOn } from '../wailsjs/runtime/runtime'
import Icon from '../components/Icon.vue'

const router = useRouter()
const title = ref('Dashboard')
const description = ref('')
const tableCount = ref(0)
const notebookCount = ref(0)
const queryCount = ref(0)
const loading = ref(true)
const errorMessage = ref('')

let loadGen = 0

async function load() {
  const gen = ++loadGen
  loading.value = true
  errorMessage.value = ''
  try {
    const d = await window.go.main.App.GetDashboard()
    if (gen !== loadGen) return
    title.value = d?.title || 'Dashboard'
    description.value = d?.description || ''
    tableCount.value = d?.table_count ?? 0
    notebookCount.value = d?.notebook_count ?? 0
    queryCount.value = d?.query_count ?? 0
  } catch (e: unknown) {
    if (gen !== loadGen) return
    errorMessage.value = String(e)
  }
  loading.value = false
}

const hasWailsRuntime = () => typeof window !== 'undefined' && !!(window as any).runtime

let unsubDataChanged: (() => void) | null = null

onMounted(() => {
  load()
  // Re-fetch the live counts when the data underneath changes (imports,
  // table create/drop, notebook edits) so the dashboard never shows stale
  // numbers until a manual reload.
  if (hasWailsRuntime()) unsubDataChanged = EventsOn('data:changed', load)
})
onUnmounted(() => {
  unsubDataChanged?.()
})
</script>

<template>
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="page-head">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Home</div>
        <h1>{{ title }}</h1>
      </div>
      <div class="row">
        <button class="btn" @click="router.push('/import')">
          <Icon name="download" :size="16" /> Import CSV
        </button>
        <button class="btn btn-primary" @click="router.push('/notebooks?new=1')">
          <Icon name="plus" :size="16" /> New notebook
        </button>
      </div>
    </div>

    <p v-if="description" class="sh-lead" style="margin-bottom: 24px; max-width: 720px;">
      {{ description }}
    </p>

    <p v-if="errorMessage" class="sh-mono" role="alert" style="color: var(--sh-coral, #e5604a);">
      {{ errorMessage }}
    </p>

    <div v-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>

    <div v-else class="dashboard-stats" data-testid="dashboard-stats" style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 8px;">
      <div class="card" style="padding: 20px;">
        <div class="sh-meta">Tables</div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 32px;">{{ tableCount }}</div>
      </div>
      <div class="card" style="padding: 20px;">
        <div class="sh-meta">Notebooks</div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 32px;">{{ notebookCount }}</div>
      </div>
      <div class="card" style="padding: 20px;">
        <div class="sh-meta">Saved queries</div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 32px;">{{ queryCount }}</div>
      </div>
    </div>

    <div class="sec-title" style="margin-top: 32px;">
      <h2>Quick actions</h2>
      <span class="label">Jump back in</span>
    </div>
    <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px;">
      <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-teal);" @click="router.push('/notebooks')">
        <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
          <Icon name="layers" :size="22" />
        </div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Notebooks</div>
        <div class="sh-meta" style="margin-top: 4px;">SQL, markdown & A2UI cells</div>
      </div>
      <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-subj-science);" @click="router.push('/tables')">
        <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
          <Icon name="database" :size="22" />
        </div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Tables</div>
        <div class="sh-meta" style="margin-top: 4px;">Browse & edit your data</div>
      </div>
      <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-subj-math);" @click="router.push('/queries')">
        <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
          <Icon name="terminal" :size="22" />
        </div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Queries</div>
        <div class="sh-meta" style="margin-top: 4px;">Run SQL on DuckDB</div>
      </div>
    </div>
  </div>
</template>
