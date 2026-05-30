<script setup lang="ts">
// P3.10 ReportView — the Report View screen (mockup #11). Lists stored banded
// reports, renders the selected report's banded HTML via RenderReportContent
// (the same ComputeBands + RenderReport pipeline the agent Report Wizard
// targets), and offers Office-Link exports (HTML/PDF/XLSX). Translated into Vue
// + SchoolHouse from the #11 report-main mockup.
import { ref, onMounted } from 'vue'
import Icon from '../components/Icon.vue'

interface ReportSummary {
  id: string
  title: string
  source: string
}

const reportsList = ref<ReportSummary[]>([])
const selectedID = ref('')
const renderedHTML = ref('')
const loading = ref(true)
const rendering = ref(false)
const errorMessage = ref('')

async function loadReports() {
  loading.value = true
  errorMessage.value = ''
  try {
    reportsList.value = (await window.go.main.App.ListReports()) || []
    if (reportsList.value.length && !selectedID.value) {
      await selectReport(reportsList.value[0].id)
    }
  } catch (e: unknown) {
    reportsList.value = []
    errorMessage.value = String(e)
  }
  loading.value = false
}

async function selectReport(id: string) {
  selectedID.value = id
  rendering.value = true
  errorMessage.value = ''
  try {
    renderedHTML.value = await window.go.main.App.RenderReportContent(id)
  } catch (e: unknown) {
    renderedHTML.value = ''
    errorMessage.value = String(e)
  }
  rendering.value = false
}

async function exportPDF() {
  if (!selectedID.value) return
  try {
    await window.go.main.App.ExportReportPDF(selectedID.value, '')
  } catch (e: unknown) {
    errorMessage.value = String(e)
  }
}

async function analyzeInExcel() {
  if (!selectedID.value) return
  try {
    await window.go.main.App.AnalyzeInExcel(selectedID.value, '')
  } catch (e: unknown) {
    errorMessage.value = String(e)
  }
}

onMounted(loadReports)
</script>

<template>
  <div class="report-view">
    <header class="report-view__header">
      <h1 class="report-view__title">Reports</h1>
      <div class="report-view__actions">
        <button class="sh-btn" :disabled="!selectedID" @click="exportPDF">
          <Icon name="download" /> Print / PDF
        </button>
        <button class="sh-btn" :disabled="!selectedID" @click="analyzeInExcel">
          <Icon name="tableIcon" /> Analyze in Excel
        </button>
      </div>
    </header>

    <p v-if="errorMessage" class="report-view__error" role="alert">{{ errorMessage }}</p>

    <div class="report-view__body">
      <aside class="report-view__picker">
        <p v-if="loading" class="report-view__muted">Loading reports…</p>
        <p v-else-if="!reportsList.length" class="report-view__muted">
          No reports yet. Ask the agent to build one with the Report Wizard.
        </p>
        <ul v-else class="report-view__list">
          <li v-for="r in reportsList" :key="r.id">
            <button
              class="report-view__list-item"
              :class="{ 'is-active': r.id === selectedID }"
              @click="selectReport(r.id)"
            >
              <span class="report-view__list-title">{{ r.title }}</span>
              <span class="report-view__list-source">{{ r.source }}</span>
            </button>
          </li>
        </ul>
      </aside>

      <section class="report-view__canvas">
        <p v-if="rendering" class="report-view__muted">Rendering…</p>
        <!-- eslint-disable-next-line vue/no-v-html -->
        <div v-else-if="renderedHTML" class="report-view__rendered" v-html="renderedHTML"></div>
        <p v-else class="report-view__muted">Select a report to preview its banded layout.</p>
      </section>
    </div>
  </div>
</template>

<style scoped>
.report-view { display: flex; flex-direction: column; height: 100%; gap: 1rem; padding: 1.5rem; }
.report-view__header { display: flex; align-items: center; justify-content: space-between; }
.report-view__title { font-family: var(--font-display, serif); font-size: 1.75rem; margin: 0; }
.report-view__actions { display: flex; gap: 0.5rem; }
.report-view__error { color: var(--color-coral, #e5604a); font-weight: 600; }
.report-view__body { display: flex; gap: 1rem; flex: 1; min-height: 0; }
.report-view__picker { width: 16rem; flex-shrink: 0; overflow-y: auto; }
.report-view__list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.5rem; }
.report-view__list-item {
  display: flex; flex-direction: column; align-items: flex-start; gap: 0.15rem;
  width: 100%; text-align: left; padding: 0.6rem 0.75rem; cursor: pointer;
  border: 2px solid var(--color-border, #1a1a1a); background: var(--color-paper, #fdfaf3);
  box-shadow: 3px 3px 0 var(--color-border, #1a1a1a);
}
.report-view__list-item.is-active { background: var(--color-teal, #2f8f83); color: #fff; }
.report-view__list-title { font-weight: 600; }
.report-view__list-source { font-family: var(--font-mono, monospace); font-size: 0.75rem; opacity: 0.8; }
.report-view__canvas { flex: 1; overflow: auto; border: 2px solid var(--color-border, #1a1a1a); background: #fff; padding: 1.5rem; }
.report-view__muted { opacity: 0.65; }
</style>
