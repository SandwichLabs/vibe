// P3.10 component test for ReportView (mockup #11). Mounts the real component
// against mocked window.go bindings and a stubbed Chart.js, then asserts:
//   - the report picker lists reports and renders the first by default
//   - the banded (table) view renders RenderReportContentFiltered output
//   - the chart <-> table toggle switches views and draws a Chart.js chart
//   - live DuckDB filters call the bindings with the selected column=value
//   - the Studio-only "Edit SQL" affordance is gated on app mode
//   - print/export wire to the P3.9 ExportReportPDF / AnalyzeInExcel bindings
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ReportView from './ReportView.vue'
import { useAppMode } from '../composables/useAppMode'

// Stub Chart.js: record constructions + destroys so we can assert chart draws.
const chartCtor = vi.fn()
const chartDestroy = vi.fn()
class FakeChart {
  constructor(...args: unknown[]) {
    chartCtor(...args)
  }
  destroy() {
    chartDestroy()
  }
}

interface AppMocks {
  ListReports: ReturnType<typeof vi.fn>
  RenderReportContentFiltered: ReturnType<typeof vi.fn>
  GetReportData: ReturnType<typeof vi.fn>
  GetReportSQL: ReturnType<typeof vi.fn>
  ExportReportPDF: ReturnType<typeof vi.fn>
  AnalyzeInExcel: ReturnType<typeof vi.fn>
}

function makeApp(): AppMocks {
  const reports = [
    { id: 'rpt_sales', title: 'Sales by Rep', source: 'SELECT * FROM orders' },
    { id: 'rpt_other', title: 'Other', source: 'SELECT * FROM x' },
  ]
  const data = {
    columns: ['sales_rep', 'amount'],
    rows: [
      ['Alice', 350],
      ['Bob', 375],
    ],
  }
  const app: AppMocks = {
    ListReports: vi.fn(async () => reports),
    RenderReportContentFiltered: vi.fn(
      async (_id: string, filters: Record<string, string>) =>
        `<section class="vibe-report"><div class="vibe-group-header">${
          filters.sales_rep || 'ALL'
        }</div></section>`,
    ),
    GetReportData: vi.fn(async () => data),
    GetReportSQL: vi.fn(async () => 'SELECT sales_rep, amount FROM orders'),
    ExportReportPDF: vi.fn(async () => '/tmp/r.pdf'),
    AnalyzeInExcel: vi.fn(async () => '/tmp/r.xlsx'),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).Chart = FakeChart
  return app
}

function mountView() {
  return mount(ReportView, { global: { stubs: { Icon: true } }, attachTo: document.body })
}

describe('ReportView (P3.10)', () => {
  beforeEach(() => {
    chartCtor.mockClear()
    chartDestroy.mockClear()
    // Reset to Studio mode before each test.
    useAppMode().setMode('studio')
  })

  it('lists reports and renders the first banded report by default', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Picker shows both reports.
    expect(wrapper.text()).toContain('Sales by Rep')
    expect(wrapper.text()).toContain('Other')

    // First report rendered as banded HTML via the filtered binding (no filter).
    expect(app.RenderReportContentFiltered).toHaveBeenCalledWith('rpt_sales', {})
    expect(wrapper.find('[data-testid="banded"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="banded"]').html()).toContain('vibe-group-header')
  })

  it('toggles between table and chart views, drawing a Chart.js chart', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Switch to chart view.
    await wrapper.find('[data-testid="view-chart"]').trigger('click')
    await flushPromises()

    // Chart data fetched and a chart constructed.
    expect(app.GetReportData).toHaveBeenCalled()
    expect(chartCtor).toHaveBeenCalled()
    const cfg = chartCtor.mock.calls.at(-1)![1] as any
    expect(cfg.type).toBe('bar')
    expect(cfg.data.labels).toEqual(['Alice', 'Bob'])
    expect(cfg.data.datasets[0].data).toEqual([350, 375])

    // Switch back to table view.
    await wrapper.find('[data-testid="view-table"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="banded"]').exists()).toBe(true)
  })

  it('applies live DuckDB filters via the bindings', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Pick a filter column -> distinct values derived from GetReportData.
    const colSel = wrapper.find('[data-testid="filter-column"]')
    await colSel.setValue('sales_rep')
    await flushPromises()

    const valSel = wrapper.find('[data-testid="filter-value"]')
    expect(valSel.exists()).toBe(true)
    // Distinct values offered.
    expect(valSel.findAll('option').map((o) => o.text())).toContain('Alice')

    // Selecting a value re-renders filtered.
    await valSel.setValue('Alice')
    await flushPromises()

    const lastCall = app.RenderReportContentFiltered.mock.calls.at(-1)!
    expect(lastCall[0]).toBe('rpt_sales')
    expect(lastCall[1]).toEqual({ sales_rep: 'Alice' })
    expect(wrapper.find('[data-testid="banded"]').html()).toContain('Alice')
  })

  it('gates the Edit SQL affordance to Studio mode', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Studio mode: Edit SQL is present and reveals the SQL panel.
    const editBtn = wrapper.find('[data-testid="edit-sql"]')
    expect(editBtn.exists()).toBe(true)
    await editBtn.trigger('click')
    await flushPromises()
    expect(app.GetReportSQL).toHaveBeenCalledWith('rpt_sales')
    expect(wrapper.find('[data-testid="sql-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sql-panel"]').text()).toContain('SELECT sales_rep')

    // App mode: Edit SQL must NOT be available.
    useAppMode().setMode('app')
    await flushPromises()
    expect(wrapper.find('[data-testid="edit-sql"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sql-panel"]').exists()).toBe(false)
  })

  it('wires print/export to the P3.9 bindings', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-testid="export-pdf"]').trigger('click')
    await flushPromises()
    expect(app.ExportReportPDF).toHaveBeenCalledWith('rpt_sales', '')

    await wrapper.find('[data-testid="export-xlsx"]').trigger('click')
    await flushPromises()
    expect(app.AnalyzeInExcel).toHaveBeenCalledWith('rpt_sales', '')
  })
})
