<script setup lang="ts">
// P3.10: Studio Home / Dashboard. The body is rendered by the SAME page
// pipeline used by the page editor/preview — it renders the `_dashboard`
// system page (seeded by the container migration) via RenderPageContent, so the
// home screen is data-driven (live {metric:}/{table:} directives executed on
// DuckDB) rather than carrying hardcoded stats from GetDashboard().
import { ref, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'

// Stable id of the seeded `_dashboard` system page (see container schema
// DashboardPageID). The page pipeline renders this through the standard
// SQL-block + directive path.
const DASHBOARD_PAGE_ID = '_dashboard'

const router = useRouter()
const title = ref('Dashboard')
const description = ref('')
const rendered = ref('')
const loading = ref(true)
const errorMessage = ref('')

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    // File metadata still comes from GetFileInfo for the crumb/lead; the BODY
    // is rendered through the page pipeline.
    const info = await window.go.main.App.GetFileInfo()
    title.value = info?.title || 'Dashboard'
    description.value = info?.description || ''
  } catch {
    /* metadata is best-effort */
  }
  try {
    rendered.value = await window.go.main.App.RenderPageContent(DASHBOARD_PAGE_ID, {})
  } catch (e: unknown) {
    rendered.value = ''
    errorMessage.value = String(e)
  }
  loading.value = false
  await nextTick()
}

onMounted(load)
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
        <button class="btn btn-primary" @click="router.push('/pages?new=1')">
          <Icon name="plus" :size="16" /> New page
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

    <!-- Dashboard body, rendered through the page pipeline (no hardcoded stats). -->
    <!-- eslint-disable-next-line vue/no-v-html -->
    <div
      v-else
      class="dashboard-rendered vibe-content prose"
      data-testid="dashboard-rendered"
      v-html="rendered"
    ></div>

    <div class="sec-title" style="margin-top: 32px;">
      <h2>Quick actions</h2>
      <span class="label">Jump back in</span>
    </div>
    <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px;">
      <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-teal);" @click="router.push('/pages')">
        <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
          <Icon name="fileText" :size="22" />
        </div>
        <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Pages</div>
        <div class="sh-meta" style="margin-top: 4px;">Author markdown & HTML pages</div>
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
