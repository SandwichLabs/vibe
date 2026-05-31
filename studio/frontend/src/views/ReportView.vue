<script setup lang="ts">
// P3.10 ReportView — the Report View screen (mockup #11). Lists stored banded
// reports and, for the selected report, offers:
//   - a chart <-> table (banded) toggle over the same DuckDB result
//   - live DuckDB filter controls (column + value) wired to re-render via the
//     RenderReportContentFiltered / GetReportData bindings
//   - a Studio-only "Edit SQL" affordance (gated on useAppMode().isStudio)
//   - Office-Link exports wired to the P3.9 ExportReportPDF / AnalyzeInExcel
//     bindings
// Translated into Vue + SchoolHouse from the #11 report-main mockup.
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../components/Icon.vue'
import { useAppMode } from '../composables/useAppMode'

interface ReportSummary {
  id: string
  title: string
  source: string
}

const { isStudio } = useAppMode()

// The switchboard (P4.7) navigates here with ?id=<reportId> to open a bound
// report. Honor it instead of always defaulting to the first report. The route
// may be absent when this view is mounted without a router (unit tests).
const route = useRoute() ?? ({ query: {} } as ReturnType<typeof useRoute>)

const reportsList = ref<ReportSummary[]>([])
const selectedID = ref('')
const renderedHTML = ref('')
const loading = ref(true)
const rendering = ref(false)
const errorMessage = ref('')

// View mode: 'table' = banded HTML; 'chart' = Chart.js bar over the data.
const viewMode = ref<'table' | 'chart'>('table')

// Live filter state.
const filterColumn = ref('')
const filterValue = ref('')
const columns = ref<string[]>([])
const filterOptions = ref<string[]>([])

// Studio-only Edit SQL.
const showSQL = ref(false)
const reportSQL = ref('')

const chartCanvas = ref<HTMLCanvasElement | null>(null)
// eslint-disable-next-line @typescript-eslint/no-explicit-any
let chartInstance: any = null

function activeFilters(): Record<string, string> {
  if (filterColumn.value && filterValue.value) {
    return { [filterColumn.value]: filterValue.value }
  }
  return {}
}

async function loadReports() {
  loading.value = true
  errorMessage.value = ''
  try {
    reportsList.value = (await window.go.main.App.ListReports()) || []
    const wanted = queryReportId()
    if (wanted && reportsList.value.some((r) => r.id === wanted)) {
      await selectReport(wanted)
    } else if (reportsList.value.length && !selectedID.value) {
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
  // Reset per-report view state.
  filterColumn.value = ''
  filterValue.value = ''
  filterOptions.value = []
  showSQL.value = false
  reportSQL.value = ''
  await loadColumns()
  await rerender()
}

// Load the report's column list (for the filter-column picker) from its data.
async function loadColumns() {
  try {
    const data = await window.go.main.App.GetReportData(selectedID.value, {})
    columns.value = data?.columns || []
  } catch (e: unknown) {
    columns.value = []
    errorMessage.value = String(e)
  }
}

// Re-render the current view (banded HTML or chart) with the active filters,
// wiring straight through the bindings (live DuckDB filters).
async function rerender() {
  if (!selectedID.value) return
  rendering.value = true
  errorMessage.value = ''
  try {
    if (viewMode.value === 'table') {
      renderedHTML.value = await window.go.main.App.RenderReportContentFiltered(
        selectedID.value,
        activeFilters(),
      )
    } else {
      const data = await window.go.main.App.GetReportData(selectedID.value, activeFilters())
      await nextTick()
      drawChart(data?.columns || [], data?.rows || [])
    }
  } catch (e: unknown) {
    renderedHTML.value = ''
    errorMessage.value = String(e)
  }
  rendering.value = false
}

// When the user picks a filter column, derive the distinct values for the
// value dropdown from the report's data (live, from DuckDB).
async function onFilterColumnChange() {
  filterValue.value = ''
  filterOptions.value = []
  if (!filterColumn.value) {
    await rerender()
    return
  }
  try {
    const data = await window.go.main.App.GetReportData(selectedID.value, {})
    const idx = (data?.columns || []).indexOf(filterColumn.value)
    if (idx >= 0) {
      const seen = new Set<string>()
      for (const row of data?.rows || []) {
        const v = String(row[idx])
        if (!seen.has(v)) seen.add(v)
      }
      filterOptions.value = Array.from(seen).sort()
    }
  } catch (e: unknown) {
    errorMessage.value = String(e)
  }
  await rerender()
}

function drawChart(cols: string[], rows: unknown[][]) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const Chart = (window as any).Chart
  const canvas = chartCanvas.value
  if (!Chart || !canvas) return
  if (chartInstance) {
    chartInstance.destroy()
    chartInstance = null
  }
  // Label column = first non-numeric column; value column = first numeric.
  let labelIdx = 0
  let valueIdx = cols.length > 1 ? 1 : 0
  for (let i = 0; i < cols.length; i++) {
    const sample = rows.length ? rows[0][i] : null
    if (typeof sample === 'number') {
      valueIdx = i
      break
    }
  }
  const labels = rows.map((r) => String(r[labelIdx]))
  const values = rows.map((r) => Number(r[valueIdx]) || 0)
  chartInstance = new Chart(canvas, {
    type: 'bar',
    data: {
      labels,
      datasets: [{ label: cols[valueIdx] || 'value', data: values, backgroundColor: '#1F7268' }],
    },
    options: { responsive: true, maintainAspectRatio: false },
  })
}

async function setView(mode: 'table' | 'chart') {
  viewMode.value = mode
  await rerender()
}

async function toggleSQL() {
  if (!isStudio.value) return
  showSQL.value = !showSQL.value
  if (showSQL.value && !reportSQL.value) {
    try {
      reportSQL.value = await window.go.main.App.GetReportSQL(selectedID.value)
    } catch (e: unknown) {
      errorMessage.value = String(e)
    }
  }
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

// Re-render whenever the filter value changes (live).
watch(filterValue, () => {
  rerender()
})

function queryReportId(): string | null {
  const q = route.query.id
  const id = Array.isArray(q) ? q[0] : q
  return id ? String(id) : null
}

const hasSelection = computed(() => !!selectedID.value)

// Re-select when the switchboard navigates to a different bound report without
// remounting the view.
watch(() => route.query.id, (q) => {
  const id = Array.isArray(q) ? q[0] : q
  if (id && reportsList.value.some((r) => r.id === String(id)) && selectedID.value !== String(id)) {
    selectReport(String(id))
  }
})

onMounted(loadReports)
</script>

<template>
  <div class="report-view">
    <header class="report-view__header">
      <h1 class="report-view__title">Reports</h1>
      <div class="report-view__actions">
        <!-- Chart <-> table toggle -->
        <div class="report-view__toggle" role="group" aria-label="View mode">
          <button
            class="sh-btn report-view__toggle-btn"
            :class="{ 'is-active': viewMode === 'table' }"
            :aria-pressed="viewMode === 'table'"
            data-testid="view-table"
            @click="setView('table')"
          >
            <Icon name="tableIcon" /> Table
          </button>
          <button
            class="sh-btn report-view__toggle-btn"
            :class="{ 'is-active': viewMode === 'chart' }"
            :aria-pressed="viewMode === 'chart'"
            data-testid="view-chart"
            @click="setView('chart')"
          >
            <Icon name="barChart" /> Chart
          </button>
        </div>

        <!-- Studio-only Edit SQL -->
        <button
          v-if="isStudio"
          class="sh-btn"
          :disabled="!hasSelection"
          data-testid="edit-sql"
          @click="toggleSQL"
        >
          <Icon name="terminal" /> Edit SQL
        </button>

        <button class="sh-btn" :disabled="!hasSelection" data-testid="export-pdf" @click="exportPDF">
          <Icon name="download" /> Print / PDF
        </button>
        <button class="sh-btn" :disabled="!hasSelection" data-testid="export-xlsx" @click="analyzeInExcel">
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

      <section class="report-view__main">
        <!-- Live DuckDB filter controls -->
        <div v-if="hasSelection" class="report-view__filters" data-testid="filters">
          <label class="report-view__filter-label">
            Filter
            <select v-model="filterColumn" data-testid="filter-column" @change="onFilterColumnChange">
              <option value="">(none)</option>
              <option v-for="c in columns" :key="c" :value="c">{{ c }}</option>
            </select>
          </label>
          <label v-if="filterColumn" class="report-view__filter-label">
            =
            <select v-model="filterValue" data-testid="filter-value">
              <option value="">(all)</option>
              <option v-for="v in filterOptions" :key="v" :value="v">{{ v }}</option>
            </select>
          </label>
        </div>

        <!-- Studio-only SQL panel -->
        <pre
          v-if="isStudio && showSQL"
          class="report-view__sql"
          data-testid="sql-panel"
        >{{ reportSQL }}</pre>

        <div class="report-view__canvas">
          <p v-if="rendering" class="report-view__muted">Rendering…</p>

          <!-- Chart view -->
          <div v-show="viewMode === 'chart'" class="report-view__chart" data-testid="chart">
            <canvas ref="chartCanvas"></canvas>
          </div>

          <!-- Table (banded) view -->
          <template v-if="viewMode === 'table'">
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div
              v-if="renderedHTML"
              class="report-view__rendered"
              data-testid="banded"
              v-html="renderedHTML"
            ></div>
            <p v-else-if="!rendering" class="report-view__muted">
              Select a report to preview its banded layout.
            </p>
          </template>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.report-view { display: flex; flex-direction: column; height: 100%; gap: 1rem; padding: 1.5rem; }
.report-view__header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
.report-view__title { font-family: var(--font-display, serif); font-size: 1.75rem; margin: 0; }
.report-view__actions { display: flex; gap: 0.5rem; align-items: center; }
.report-view__toggle { display: inline-flex; }
.report-view__toggle-btn.is-active { background: var(--sh-teal); color: #fff; }
.report-view__error { color: var(--color-coral, #e5604a); font-weight: 600; }
.report-view__body { display: flex; gap: 1rem; flex: 1; min-height: 0; }
.report-view__picker { width: 16rem; flex-shrink: 0; overflow-y: auto; }
.report-view__main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 0.75rem; }
.report-view__filters { display: flex; gap: 1rem; align-items: center; flex-wrap: wrap; }
.report-view__filter-label { display: inline-flex; gap: 0.4rem; align-items: center; font-family: var(--font-mono, monospace); font-size: 0.8rem; }
.report-view__sql { font-family: var(--font-mono, monospace); font-size: 0.8rem; background: var(--color-paper-2, #f2eddf); border: 2px solid var(--color-border, #1a1a1a); padding: 0.75rem; white-space: pre-wrap; margin: 0; }
.report-view__list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.5rem; }
.report-view__list-item {
  display: flex; flex-direction: column; align-items: flex-start; gap: 0.15rem;
  width: 100%; text-align: left; padding: 0.6rem 0.75rem; cursor: pointer;
  border: 2px solid var(--color-border, #1a1a1a); background: var(--color-paper, #fdfaf3);
  box-shadow: 3px 3px 0 var(--color-border, #1a1a1a);
}
.report-view__list-item.is-active { background: var(--sh-teal); color: #fff; }
.report-view__list-title { font-weight: 600; }
.report-view__list-source { font-family: var(--font-mono, monospace); font-size: 0.75rem; opacity: 0.8; }
.report-view__canvas { flex: 1; overflow: auto; border: 2px solid var(--color-border, #1a1a1a); background: #fff; padding: 1.5rem; }
.report-view__chart { height: 360px; }
.report-view__muted { opacity: 0.65; }
</style>
