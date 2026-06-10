<script setup lang="ts">
// Parity W6 — Report View (mockup #11 "report-main"): a single-report
// dashboard rather than a list browser.
//   - Head row: breadcrumb + report picker, last-run metadata, Run now,
//     Export CSV, Office Links, Studio-only Edit SQL (which actually edits —
//     UpdateReportSQL validates by running the SQL before persisting).
//   - Faceted filter card: category multi-selects + numeric ranges derived
//     from the report's own data, applied live as parameterized DuckDB
//     filters (GetReportDataFaceted), plus Reset and the Chart/Table seg.
//   - 2fr/1fr grid: stacked multi-series Chart.js bar (per-series SchoolHouse
//     subject-token palette + legend) beside KPI tiles and the "Ask the
//     agent" card.
//   - Always-visible full-width detail table (chart + table simultaneously —
//     the old view was either/or).
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../components/Icon.vue'
import { useAppMode } from '../composables/useAppMode'

interface ReportSummary {
  id: string
  title: string
  source: string
}

interface ChartSpec {
  label_field: string
  series_field?: string
  value_field: string
}

interface ReportData {
  columns: string[]
  rows: unknown[][]
  chart?: ChartSpec | null
  duration_ms: number
}

interface ReportFilter {
  column: string
  op: 'eq' | 'in' | 'range'
  values: string[]
}

interface CategoryFacet {
  column: string
  kind: 'category'
  options: string[]
}

interface RangeFacet {
  column: string
  kind: 'range'
}

type Facet = CategoryFacet | RangeFacet

const { isStudio } = useAppMode()

// The switchboard (P4.7) navigates here with ?id=<reportId> to open a bound
// report. The route may be absent when mounted without a router (unit tests).
const route = useRoute() ?? ({ query: {} } as ReturnType<typeof useRoute>)

const reportsList = ref<ReportSummary[]>([])
const selectedID = ref('')
const loading = ref(true)
const running = ref(false)
const errorMessage = ref('')

// Current (filtered) data — drives chart, KPI tiles, and the detail table.
const data = ref<ReportData | null>(null)
// Facets are derived once per report from its unfiltered data.
const facets = ref<Facet[]>([])
const facetChecked = ref<Record<string, string[]>>({})
const facetRange = ref<Record<string, { min: string; max: string }>>({})
const openFacet = ref('')

// Left-card mode: stacked chart (default, mockup chart-first) or banded HTML.
const viewMode = ref<'chart' | 'table'>('chart')
const bandedHTML = ref('')

// Last-run metadata strip.
const lastRunAt = ref('')
const lastRunMs = ref(0)

// Studio-only SQL editor.
const showSQL = ref(false)
const sqlText = ref('')
const sqlError = ref('')
const sqlSaving = ref(false)

const statusMessage = ref('')

const chartCanvas = ref<HTMLCanvasElement | null>(null)
// eslint-disable-next-line @typescript-eslint/no-explicit-any
let chartInstance: any = null

const selectedReport = computed(() => reportsList.value.find((r) => r.id === selectedID.value))
const hasSelection = computed(() => !!selectedID.value)

// ---- loading ----

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
  facets.value = []
  facetChecked.value = {}
  facetRange.value = {}
  openFacet.value = ''
  showSQL.value = false
  sqlText.value = ''
  sqlError.value = ''
  statusMessage.value = ''
  bandedHTML.value = ''
  try {
    // Unfiltered pass: derives the facet model, then doubles as the first run.
    const base = (await window.go.main.App.GetReportDataFaceted(id, [])) as ReportData
    facets.value = deriveFacets(base.columns || [], base.rows || [])
    initRangeState()
    applyRun(base)
  } catch (e: unknown) {
    data.value = null
    errorMessage.value = String(e)
  }
}

function buildFilters(): ReportFilter[] {
  const out: ReportFilter[] = []
  for (const f of facets.value) {
    if (f.kind === 'category') {
      const sel = facetChecked.value[f.column] || []
      if (sel.length && sel.length < f.options.length) {
        out.push({ column: f.column, op: 'in', values: sel })
      }
    } else {
      const r = facetRange.value[f.column]
      if (r && (r.min !== '' || r.max !== '')) {
        // Vue coerces type="number" inputs to numbers; the Go binding wants
        // strings ('' = open end).
        out.push({ column: f.column, op: 'range', values: [String(r.min), String(r.max)] })
      }
    }
  }
  return out
}

async function runReport() {
  if (!selectedID.value) return
  running.value = true
  errorMessage.value = ''
  try {
    const filters = buildFilters()
    const fresh = (await window.go.main.App.GetReportDataFaceted(
      selectedID.value,
      filters,
    )) as ReportData
    applyRun(fresh)
    if (viewMode.value === 'table') {
      bandedHTML.value = await window.go.main.App.RenderReportContentFaceted(
        selectedID.value,
        filters,
      )
    }
  } catch (e: unknown) {
    errorMessage.value = String(e)
  }
  running.value = false
}

function applyRun(fresh: ReportData) {
  data.value = fresh
  lastRunAt.value = new Date().toLocaleTimeString()
  lastRunMs.value = Math.round((fresh.duration_ms || 0) * 10) / 10
  nextTick(() => drawChart())
}

// ---- facets ----

const FACET_MAX = 4
const CATEGORY_MAX_DISTINCT = 24

// Derive up to FACET_MAX facets from the unfiltered result: low-cardinality
// non-numeric columns become category multi-selects, numeric columns become
// min/max ranges (mockup's Range / Technician / Status / Part category bar).
function deriveFacets(cols: string[], rows: unknown[][]): Facet[] {
  const out: Facet[] = []
  for (let i = 0; i < cols.length && out.length < FACET_MAX; i++) {
    const sample = rows.find((r) => r[i] !== null && r[i] !== undefined)?.[i]
    if (typeof sample === 'number') {
      out.push({ column: cols[i], kind: 'range' })
      continue
    }
    const distinct = new Set<string>()
    for (const r of rows) {
      if (r[i] !== null && r[i] !== undefined) distinct.add(String(r[i]))
      if (distinct.size > CATEGORY_MAX_DISTINCT) break
    }
    if (distinct.size >= 1 && distinct.size <= CATEGORY_MAX_DISTINCT) {
      out.push({ column: cols[i], kind: 'category', options: Array.from(distinct).sort() })
    }
  }
  return out
}

// Range facets bind v-model to facetRange[col].min/max — seed the entries so
// the bindings always have an object to write into.
function initRangeState() {
  for (const f of facets.value) {
    if (f.kind === 'range' && !facetRange.value[f.column]) {
      facetRange.value[f.column] = { min: '', max: '' }
    }
  }
}

function facetSummary(f: CategoryFacet): string {
  const sel = facetChecked.value[f.column] || []
  if (!sel.length || sel.length === f.options.length) return 'All'
  return sel.join(', ')
}

function toggleFacetOption(column: string, option: string) {
  const sel = facetChecked.value[column] || []
  facetChecked.value[column] = sel.includes(option)
    ? sel.filter((v) => v !== option)
    : [...sel, option]
  runReport()
}

function isFacetChecked(column: string, option: string): boolean {
  return (facetChecked.value[column] || []).includes(option)
}

const filtersActive = computed(
  () =>
    Object.values(facetChecked.value).some((v) => v.length) ||
    Object.values(facetRange.value).some((r) => r.min !== '' || r.max !== ''),
)

async function resetFilters() {
  facetChecked.value = {}
  facetRange.value = {}
  initRangeState()
  openFacet.value = ''
  await runReport()
}

async function setView(mode: 'chart' | 'table') {
  viewMode.value = mode
  if (mode === 'table' && selectedID.value) {
    try {
      bandedHTML.value = await window.go.main.App.RenderReportContentFaceted(
        selectedID.value,
        buildFilters(),
      )
    } catch (e: unknown) {
      errorMessage.value = String(e)
    }
  } else {
    await nextTick()
    drawChart()
  }
}

// ---- chart ----

// Per-series palette: SchoolHouse subject tokens, in the mockup's order.
const SERIES_TOKENS = [
  '--sh-subj-math',
  '--sh-subj-history',
  '--sh-subj-pe',
  '--sh-subj-art',
  '--sh-subj-music',
  '--sh-subj-science',
  '--sh-subj-ela',
  '--sh-subj-world',
]

function seriesColor(i: number): string {
  const token = SERIES_TOKENS[i % SERIES_TOKENS.length]
  const v =
    typeof getComputedStyle === 'function'
      ? getComputedStyle(document.documentElement).getPropertyValue(token).trim()
      : ''
  return v || '#1F7268'
}

// Stored chart spec, or heuristics: label = first non-numeric column, value =
// first numeric column, series = next low-cardinality non-numeric column.
const chartMeta = computed<ChartSpec | null>(() => {
  const d = data.value
  if (!d || !d.columns.length) return null
  if (d.chart && d.chart.label_field && d.chart.value_field) return d.chart
  const isNumeric = d.columns.map((_, i) => {
    const sample = d.rows.find((r) => r[i] !== null && r[i] !== undefined)?.[i]
    return typeof sample === 'number'
  })
  const labelIdx = isNumeric.findIndex((n) => !n)
  const valueIdx = isNumeric.findIndex((n) => n)
  if (valueIdx < 0) return null
  let series = ''
  for (let i = 0; i < d.columns.length; i++) {
    if (i === labelIdx || isNumeric[i]) continue
    const distinct = new Set(d.rows.map((r) => String(r[i])))
    if (distinct.size <= 12) {
      series = d.columns[i]
      break
    }
  }
  return {
    label_field: labelIdx >= 0 ? d.columns[labelIdx] : d.columns[0],
    series_field: series || undefined,
    value_field: d.columns[valueIdx],
  }
})

interface ChartModel {
  labels: string[]
  series: { name: string; color: string; data: number[] }[]
  stacked: boolean
}

const chartModel = computed<ChartModel | null>(() => {
  const d = data.value
  const meta = chartMeta.value
  if (!d || !meta) return null
  const li = d.columns.indexOf(meta.label_field)
  const vi = d.columns.indexOf(meta.value_field)
  if (li < 0 || vi < 0) return null
  const si = meta.series_field ? d.columns.indexOf(meta.series_field) : -1

  // Labels in first-appearance order (the query's own ordering).
  const labels: string[] = []
  const labelIdx = new Map<string, number>()
  for (const r of d.rows) {
    const l = String(r[li])
    if (!labelIdx.has(l)) {
      labelIdx.set(l, labels.length)
      labels.push(l)
    }
  }

  const seriesNames: string[] = []
  const sums = new Map<string, number[]>()
  for (const r of d.rows) {
    const s = si >= 0 ? String(r[si]) : meta.value_field
    if (!sums.has(s)) {
      sums.set(s, new Array(labels.length).fill(0))
      seriesNames.push(s)
    }
    const arr = sums.get(s)!
    arr[labelIdx.get(String(r[li]))!] += Number(r[vi]) || 0
  }

  return {
    labels,
    series: seriesNames.map((name, i) => ({
      name,
      color: seriesColor(i),
      data: sums.get(name)!,
    })),
    stacked: si >= 0,
  }
})

function drawChart() {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const Chart = (window as any).Chart
  const canvas = chartCanvas.value
  const model = chartModel.value
  if (chartInstance) {
    chartInstance.destroy()
    chartInstance = null
  }
  if (!Chart || !canvas || !model) return
  const ink =
    getComputedStyle(document.documentElement).getPropertyValue('--sh-ink').trim() || '#1F2330'
  chartInstance = new Chart(canvas, {
    type: 'bar',
    data: {
      labels: model.labels,
      datasets: model.series.map((s) => ({
        label: s.name,
        data: s.data,
        backgroundColor: s.color,
        borderColor: ink,
        borderWidth: 1.5,
      })),
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } }, // custom .chart-legend below
      scales: {
        x: { stacked: model.stacked },
        y: { stacked: model.stacked, beginAtZero: true },
      },
    },
  })
}

// ---- KPI tiles ----

interface TopKPI {
  caption: string
  name: string
  sub: string
}

// Tile 1: the biggest contributor — by series when stacked, else by label.
const kpiTop = computed<TopKPI | null>(() => {
  const model = chartModel.value
  const meta = chartMeta.value
  if (!model || !meta || !model.series.length) return null
  if (model.stacked) {
    let best = model.series[0]
    let bestTotal = -Infinity
    for (const s of model.series) {
      const total = s.data.reduce((a, b) => a + b, 0)
      if (total > bestTotal) {
        best = s
        bestTotal = total
      }
    }
    return {
      caption: `Top ${meta.series_field}`,
      name: best.name,
      sub: `${formatNum(bestTotal)} ${meta.value_field} total`,
    }
  }
  const s = model.series[0]
  let bi = 0
  for (let i = 1; i < s.data.length; i++) if (s.data[i] > s.data[bi]) bi = i
  return {
    caption: `Top ${meta.label_field}`,
    name: model.labels[bi] ?? '',
    sub: `${formatNum(s.data[bi] ?? 0)} ${meta.value_field}`,
  }
})

interface TrendKPI {
  caption: string
  pct: string
  up: boolean
  sub: string
}

// Tile 2: first-vs-last label totals across all series.
const kpiTrend = computed<TrendKPI | null>(() => {
  const model = chartModel.value
  const meta = chartMeta.value
  if (!model || !meta || model.labels.length < 2) return null
  const totalAt = (i: number) => model.series.reduce((a, s) => a + (s.data[i] || 0), 0)
  const first = totalAt(0)
  const last = totalAt(model.labels.length - 1)
  if (first === 0) return null
  const pct = Math.round(((last - first) / first) * 100)
  return {
    caption: `Trend · ${meta.label_field}`,
    pct: `${pct >= 0 ? '+' : ''}${pct}%`,
    up: pct >= 0,
    sub: `${model.labels[0]} → ${model.labels[model.labels.length - 1]}`,
  }
})

function formatNum(n: number): string {
  return Number.isInteger(n) ? String(n) : n.toFixed(2)
}

// ---- detail table ----

const DETAIL_MAX = 50
const detailRows = computed(() => (data.value?.rows || []).slice(0, DETAIL_MAX))
const totalRows = computed(() => data.value?.rows.length || 0)

function isNumCell(v: unknown): boolean {
  return typeof v === 'number'
}

function cellText(v: unknown): string {
  if (v === null || v === undefined) return ''
  return typeof v === 'number' ? formatNum(v) : String(v)
}

// ---- actions ----

async function toggleSQL() {
  if (!isStudio.value) return
  showSQL.value = !showSQL.value
  sqlError.value = ''
  if (showSQL.value && !sqlText.value) {
    try {
      sqlText.value = await window.go.main.App.GetReportSQL(selectedID.value)
    } catch (e: unknown) {
      sqlError.value = String(e)
    }
  }
}

async function saveSQL() {
  if (!isStudio.value || !selectedID.value) return
  sqlSaving.value = true
  sqlError.value = ''
  try {
    await window.go.main.App.UpdateReportSQL(selectedID.value, sqlText.value)
    showSQL.value = false
    // The shape may have changed: re-derive facets from the new query.
    await selectReport(selectedID.value)
    statusMessage.value = 'Report SQL updated'
  } catch (e: unknown) {
    sqlError.value = String(e)
  }
  sqlSaving.value = false
}

async function exportCSV() {
  if (!selectedID.value) return
  try {
    const path = await window.go.main.App.ExportReportCSV(selectedID.value, buildFilters(), '')
    if (path) statusMessage.value = `Exported ${path}`
  } catch (e: unknown) {
    errorMessage.value = String(e)
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

// "Modify this report" routes into the W0 agent panel (same event the Query
// Explorer's Ask-agent uses; AppShell listens and opens the drawer).
function modifyReport() {
  window.dispatchEvent(new CustomEvent('vibe:open-agent'))
}

// ---- routing ----

function queryReportId(): string | null {
  const q = route.query.id
  const id = Array.isArray(q) ? q[0] : q
  return id ? String(id) : null
}

watch(
  () => route.query.id,
  (q) => {
    const id = Array.isArray(q) ? q[0] : q
    if (
      id &&
      reportsList.value.some((r) => r.id === String(id)) &&
      selectedID.value !== String(id)
    ) {
      selectReport(String(id))
    }
  },
)

onMounted(loadReports)
</script>

<template>
  <div class="report-view">
    <!-- Head row: crumb + picker, last-run meta, actions -->
    <div class="headrow">
      <div>
        <div class="crumb">
          Reports
          <template v-if="selectedReport"><span class="sep">/</span>{{ selectedReport.title }}</template>
        </div>
        <div class="title-row">
          <h1 class="report-title">{{ selectedReport?.title || 'Reports' }}</h1>
          <select
            v-if="reportsList.length > 1"
            class="report-picker"
            data-testid="report-select"
            :value="selectedID"
            @change="selectReport(($event.target as HTMLSelectElement).value)"
          >
            <option v-for="r in reportsList" :key="r.id" :value="r.id">{{ r.title }}</option>
          </select>
        </div>
      </div>
      <div class="head-actions">
        <span v-if="lastRunAt" class="run-meta" data-testid="run-meta">
          Last run · {{ lastRunAt }} · {{ lastRunMs }}ms
          <Icon name="zap" :size="12" class="run-meta__zap" />
        </span>
        <button
          class="btn btn-sm"
          :disabled="!hasSelection || running"
          data-testid="run-now"
          @click="runReport"
        >
          <Icon name="play" :size="14" /> Run now
        </button>
        <button
          class="btn btn-sm"
          :disabled="!hasSelection"
          data-testid="export-csv"
          @click="exportCSV"
        >
          <Icon name="download" :size="14" /> Export CSV
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="!hasSelection" data-testid="export-pdf" @click="exportPDF">
          <Icon name="fileText" :size="14" /> PDF
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="!hasSelection" data-testid="export-xlsx" @click="analyzeInExcel">
          <Icon name="tableIcon" :size="14" /> Excel
        </button>
        <button
          v-if="isStudio"
          class="btn btn-ghost btn-sm"
          :disabled="!hasSelection"
          data-testid="edit-sql"
          @click="toggleSQL"
        >
          <Icon name="edit" :size="14" /> Edit SQL
        </button>
      </div>
    </div>

    <p v-if="errorMessage" class="report-error" role="alert">{{ errorMessage }}</p>
    <p v-if="statusMessage" class="report-status" data-testid="report-status">{{ statusMessage }}</p>

    <p v-if="loading" class="report-muted">Loading reports…</p>
    <p v-else-if="!reportsList.length" class="report-muted">
      No reports yet. Ask the agent to build one with the Report Wizard.
    </p>

    <template v-if="hasSelection">
      <!-- Studio-only SQL editor (actually edits — UpdateReportSQL) -->
      <div v-if="isStudio && showSQL" class="card sql-card" data-testid="sql-editor">
        <div class="card-head">
          <h3 class="card-title">Report SQL</h3>
          <div class="sql-actions">
            <button class="btn btn-ghost btn-sm" data-testid="sql-cancel" @click="showSQL = false">Cancel</button>
            <button
              class="btn btn-primary btn-sm"
              :disabled="sqlSaving"
              data-testid="sql-save"
              @click="saveSQL"
            >
              <Icon name="save" :size="14" /> Save &amp; run
            </button>
          </div>
        </div>
        <textarea v-model="sqlText" class="sql-input" data-testid="sql-input" rows="6" spellcheck="false"></textarea>
        <p v-if="sqlError" class="report-error" data-testid="sql-error">{{ sqlError }}</p>
      </div>

      <!-- Faceted filter bar -->
      <div class="card card-pad filter-bar" data-testid="filters">
        <div class="filter-row">
          <div v-for="f in facets" :key="f.column" class="field facet" :data-testid="`facet-${f.column}`">
            <label>{{ f.column.replace(/_/g, ' ') }}</label>
            <template v-if="f.kind === 'category'">
              <button
                class="facet-toggle"
                data-testid="facet-toggle"
                @click="openFacet = openFacet === f.column ? '' : f.column"
              >
                <span class="facet-summary">{{ facetSummary(f) }}</span>
                <Icon name="chevronDown" :size="14" />
              </button>
              <div v-if="openFacet === f.column" class="facet-pop" data-testid="facet-options">
                <label v-for="opt in f.options" :key="opt" class="facet-opt">
                  <input
                    type="checkbox"
                    :checked="isFacetChecked(f.column, opt)"
                    data-testid="facet-option"
                    @change="toggleFacetOption(f.column, opt)"
                  />
                  {{ opt }}
                </label>
              </div>
            </template>
            <template v-else-if="facetRange[f.column]">
              <div class="facet-range">
                <input
                  v-model="facetRange[f.column].min"
                  type="number"
                  placeholder="min"
                  data-testid="facet-min"
                  @change="runReport"
                />
                <span>–</span>
                <input
                  v-model="facetRange[f.column].max"
                  type="number"
                  placeholder="max"
                  data-testid="facet-max"
                  @change="runReport"
                />
              </div>
            </template>
          </div>
          <span class="filter-spacer"></span>
          <button
            v-if="filtersActive"
            class="btn btn-ghost btn-sm reset-btn"
            data-testid="reset-filters"
            @click="resetFilters"
          >
            <Icon name="x" :size="13" /> Reset
          </button>
          <div class="seg" role="group" aria-label="View mode">
            <button :class="{ on: viewMode === 'chart' }" data-testid="view-chart" @click="setView('chart')">
              <Icon name="barChart" :size="14" /> Chart
            </button>
            <button :class="{ on: viewMode === 'table' }" data-testid="view-table" @click="setView('table')">
              <Icon name="tableIcon" :size="14" /> Table
            </button>
          </div>
        </div>
      </div>

      <!-- Chart/banded + tiles grid -->
      <div class="report-grid">
        <div class="card" data-testid="chart-card">
          <div class="card-head">
            <h3 class="card-title">
              {{ viewMode === 'chart' ? (selectedReport?.title || 'Chart') : 'Banded report' }}
            </h3>
            <span v-if="viewMode === 'chart' && chartModel?.stacked" class="badge badge--graphite">Stacked bar</span>
          </div>
          <div v-show="viewMode === 'chart'" class="chart-body">
            <div class="chart-canvas-wrap" data-testid="chart">
              <canvas ref="chartCanvas"></canvas>
            </div>
            <div v-if="chartModel" class="chart-legend" data-testid="chart-legend">
              <div v-for="s in chartModel.series" :key="s.name" class="sw">
                <span class="chip" :style="{ background: s.color }"></span>{{ s.name }}
              </div>
            </div>
          </div>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <div
            v-if="viewMode === 'table'"
            class="banded-body"
            data-testid="banded"
            v-html="bandedHTML"
          ></div>
        </div>

        <div class="tile-col">
          <div v-if="kpiTop" class="card stripe science kpi-tile" data-testid="kpi-top">
            <div class="kpi-caption">{{ kpiTop.caption }}</div>
            <div class="kpi-big">{{ kpiTop.name }}</div>
            <div class="kpi-sub">{{ kpiTop.sub }}</div>
          </div>
          <div v-if="kpiTrend" class="card stripe pe kpi-tile" data-testid="kpi-trend">
            <div class="kpi-caption">{{ kpiTrend.caption }}</div>
            <div class="kpi-big" :class="kpiTrend.up ? 'kpi-up' : 'kpi-down'">{{ kpiTrend.pct }}</div>
            <div class="kpi-sub">{{ kpiTrend.sub }}</div>
          </div>
          <!-- Agent affordance is a builder tool; the shell also refuses to
               open the drawer in App Mode, so hide rather than dead-button. -->
          <div v-if="isStudio" class="ask-agent" data-testid="ask-agent-card">
            <span class="lab"><Icon name="sparkle" :size="12" /> Ask the agent</span>
            <div class="ph">"Show only weekdays · group by technician"</div>
            <button class="btn btn-primary btn-sm" data-testid="modify-report" @click="modifyReport">
              Modify this report
            </button>
          </div>
        </div>
      </div>

      <!-- Always-visible detail table -->
      <div class="card detail-card" data-testid="detail-card">
        <div class="card-head">
          <h3 class="card-title">Detail · {{ totalRows }} rows</h3>
          <span class="detail-caption" data-testid="detail-caption">
            Showing {{ detailRows.length }} of {{ totalRows }}
          </span>
        </div>
        <div class="detail-scroll">
          <table class="table" data-testid="detail-table">
            <thead>
              <tr>
                <th v-for="c in data?.columns || []" :key="c">{{ c }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, ri) in detailRows" :key="ri">
                <td v-for="(v, ci) in row" :key="ci" :class="{ num: isNumCell(v) }">{{ cellText(v) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.report-view { display: flex; flex-direction: column; gap: 16px; padding: 24px; height: 100%; overflow-y: auto; }

.title-row { display: flex; align-items: center; gap: 12px; }
.report-title { font-family: var(--sh-font-display, serif); font-size: 26px; font-weight: 600; letter-spacing: -0.015em; margin: 4px 0 0; }
.report-picker { font-family: var(--sh-font-mono, monospace); font-size: 12px; padding: 4px 8px; border: 1.5px solid var(--sh-ink, #1a1a1a); background: var(--sh-paper, #fdfaf3); }
.head-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.run-meta { font-family: var(--sh-font-mono, monospace); font-size: 11px; font-weight: 500; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite, #555); display: inline-flex; align-items: center; gap: 4px; }
.run-meta__zap { color: var(--sh-success, #2e7d32); }

.report-error { color: var(--sh-error, #c62828); font-weight: 600; margin: 0; }
.report-status { font-family: var(--sh-font-mono, monospace); font-size: 12px; color: var(--sh-teal, #1f7268); margin: 0; }
.report-muted { opacity: 0.65; }

.sql-card { padding: 0 0 12px; }
.sql-card .card-head { margin-bottom: 0; }
.sql-actions { display: flex; gap: 8px; }
.sql-input { width: calc(100% - 32px); margin: 12px 16px 0; font-family: var(--sh-font-mono, monospace); font-size: 13px; border: 1.5px solid var(--sh-ink, #1a1a1a); background: var(--sh-paper-2, #f7f2e7); padding: 10px; resize: vertical; }
.sql-card .report-error { margin: 8px 16px 0; }

.filter-bar { padding: 14px 16px; overflow: visible; }
.filter-row { display: flex; gap: 14px; align-items: flex-end; flex-wrap: wrap; }
.filter-spacer { flex: 1; }
.facet { position: relative; min-width: 150px; }
.facet-toggle { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; padding: 8px 10px; border: 1.5px solid var(--sh-ink, #1a1a1a); background: var(--sh-paper, #fdfaf3); font-size: 13px; cursor: pointer; }
.facet-summary { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 180px; }
.facet-pop { position: absolute; top: 100%; left: 0; z-index: 30; min-width: 100%; max-height: 240px; overflow-y: auto; background: var(--sh-paper, #fdfaf3); border: 2px solid var(--sh-ink, #1a1a1a); box-shadow: var(--sh-shadow, 4px 4px 0 rgba(0,0,0,0.85)); padding: 6px; display: flex; flex-direction: column; gap: 2px; }
.facet-opt { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 4px 6px; cursor: pointer; white-space: nowrap; }
.facet-opt:hover { background: var(--sh-teal-8, rgba(31,114,104,0.08)); }
.facet-range { display: flex; align-items: center; gap: 6px; }
.facet-range input { width: 76px; padding: 8px; border: 1.5px solid var(--sh-ink, #1a1a1a); background: var(--sh-paper, #fdfaf3); font-size: 13px; }
.reset-btn { align-self: flex-end; }
.seg { align-self: flex-end; }

.report-grid { display: grid; grid-template-columns: 2fr 1fr; gap: 20px; align-items: start; }
.chart-body { padding: 20px; }
.chart-canvas-wrap { height: 300px; position: relative; }
.banded-body { padding: 20px; max-height: 420px; overflow: auto; }

.tile-col { display: flex; flex-direction: column; gap: 16px; }
.kpi-tile { padding: 16px 18px 16px 26px; }
.kpi-caption { font-family: var(--sh-font-mono, monospace); font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite, #555); }
.kpi-big { font-family: var(--sh-font-display, serif); font-weight: 600; font-size: 32px; letter-spacing: -0.015em; margin-top: 4px; }
.kpi-up { color: var(--sh-success, #2e7d32); }
.kpi-down { color: var(--sh-error, #c62828); }
.kpi-sub { font-size: 13px; color: var(--sh-graphite, #555); margin-top: 2px; }

.detail-card { min-height: 0; }
.detail-caption { font-family: var(--sh-font-mono, monospace); font-size: 11px; color: var(--sh-graphite, #555); text-transform: uppercase; letter-spacing: 0.06em; }
.detail-scroll { overflow-x: auto; }
</style>
