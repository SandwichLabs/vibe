// Supplementary component tests for the Report View (parity W6 / mockup #11).
//
// NOT the acceptance gate — that is Rodney driving :34115 against the real
// backend (docs/parity-remediation-plan.md §1). These cover the logic that's
// awkward to prove via the live DOM: facet derivation + filter payloads,
// stacked-chart dataset construction, KPI math, and Edit-SQL gating/round-trip.
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

function makeApp(opts: { chart?: object | null; sqlError?: string } = {}) {
  // HVAC-shaped: two months × two parts, qty numeric.
  const data = {
    columns: ['month', 'part', 'qty'],
    rows: [
      ['Jan', 'Capacitors', 40],
      ['Jan', 'Filters', 28],
      ['Feb', 'Capacitors', 56],
      ['Feb', 'Filters', 34],
    ],
    chart:
      opts.chart === undefined
        ? { label_field: 'month', series_field: 'part', value_field: 'qty' }
        : opts.chart,
    duration_ms: 4.2,
  }
  const app = {
    ListReports: vi.fn(async () => [
      { id: 'rpt_parts', title: 'Parts usage', source: 'SELECT * FROM parts_used' },
      { id: 'rpt_other', title: 'Other', source: 'SELECT * FROM x' },
    ]),
    GetReportDataFaceted: vi.fn(async () => data),
    RenderReportContentFaceted: vi.fn(
      async () => '<section class="vibe-report"><div class="vibe-group-header">G</div></section>',
    ),
    GetReportSQL: vi.fn(async () => 'SELECT month, part, qty FROM parts_used'),
    UpdateReportSQL: vi.fn(async () => {
      if (opts.sqlError) throw new Error(opts.sqlError)
    }),
    ExportReportCSV: vi.fn(async () => '/tmp/parts.csv'),
    ExportReportPDF: vi.fn(async () => '/tmp/r.pdf'),
    AnalyzeInExcel: vi.fn(async () => '/tmp/r.xlsx'),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).Chart = FakeChart
  return app
}

async function mountView() {
  const wrapper = mount(ReportView, { global: { stubs: { Icon: true } }, attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('ReportView (W6 Report View)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAppMode().setMode('studio')
  })

  it('renders chart, legend, KPI tiles, ask-agent card, and detail table simultaneously', async () => {
    makeApp()
    const wrapper = await mountView()

    expect(wrapper.find('[data-testid="chart-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="chart-legend"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="kpi-top"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="kpi-trend"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="ask-agent-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="detail-card"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="detail-table"] tbody tr')).toHaveLength(4)
    expect(wrapper.find('[data-testid="run-meta"]').text()).toContain('4.2ms')
  })

  it('builds a stacked multi-series chart from the stored chart spec', async () => {
    makeApp()
    await mountView()

    expect(chartCtor).toHaveBeenCalled()
    const cfg = chartCtor.mock.calls.at(-1)![1] as any
    expect(cfg.type).toBe('bar')
    expect(cfg.data.labels).toEqual(['Jan', 'Feb'])
    expect(cfg.data.datasets).toHaveLength(2)
    expect(cfg.data.datasets[0].label).toBe('Capacitors')
    expect(cfg.data.datasets[0].data).toEqual([40, 56])
    expect(cfg.data.datasets[1].label).toBe('Filters')
    expect(cfg.data.datasets[1].data).toEqual([28, 34])
    expect(cfg.options.scales.x.stacked).toBe(true)
    expect(cfg.options.scales.y.stacked).toBe(true)
  })

  it('falls back to heuristic chart metadata when no spec is stored', async () => {
    makeApp({ chart: null })
    const wrapper = await mountView()

    // month → label, part → series (low cardinality), qty → value.
    const cfg = chartCtor.mock.calls.at(-1)![1] as any
    expect(cfg.data.labels).toEqual(['Jan', 'Feb'])
    expect(cfg.data.datasets).toHaveLength(2)
    expect(wrapper.find('[data-testid="kpi-top"]').text()).toContain('Capacitors')
  })

  it('computes the KPI tiles: top series and first-to-last trend', async () => {
    makeApp()
    const wrapper = await mountView()

    const top = wrapper.find('[data-testid="kpi-top"]')
    expect(top.text()).toContain('Top part')
    expect(top.text()).toContain('Capacitors')
    expect(top.text()).toContain('96 qty total')

    // Jan total 68 → Feb total 90 → +32%.
    const trend = wrapper.find('[data-testid="kpi-trend"]')
    expect(trend.text()).toContain('+32%')
    expect(trend.text()).toContain('Jan → Feb')
  })

  it('derives facets and applies a category multi-select as an "in" filter', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    // month + part are category facets; qty is a numeric range facet.
    expect(wrapper.find('[data-testid="facet-month"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="facet-part"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="facet-qty"] [data-testid="facet-min"]').exists()).toBe(true)

    await wrapper.find('[data-testid="facet-part"] [data-testid="facet-toggle"]').trigger('click')
    const options = wrapper.findAll('[data-testid="facet-option"]')
    expect(options).toHaveLength(2) // Capacitors, Filters
    await options[0].setValue(true)
    await flushPromises()

    expect(app.GetReportDataFaceted).toHaveBeenLastCalledWith('rpt_parts', [
      { column: 'part', op: 'in', values: ['Capacitors'] },
    ])

    // Reset clears the selection and re-runs unfiltered.
    await wrapper.find('[data-testid="reset-filters"]').trigger('click')
    await flushPromises()
    expect(app.GetReportDataFaceted).toHaveBeenLastCalledWith('rpt_parts', [])
  })

  it('applies a numeric range facet as a "range" filter', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    const min = wrapper.find('[data-testid="facet-qty"] [data-testid="facet-min"]')
    await min.setValue('30')
    await min.trigger('change')
    await flushPromises()

    expect(app.GetReportDataFaceted).toHaveBeenLastCalledWith('rpt_parts', [
      { column: 'qty', op: 'range', values: ['30', ''] },
    ])
  })

  it('the Table seg renders banded HTML with the active filters, keeping the detail table', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    await wrapper.find('[data-testid="view-table"]').trigger('click')
    await flushPromises()

    expect(app.RenderReportContentFaceted).toHaveBeenCalledWith('rpt_parts', [])
    expect(wrapper.find('[data-testid="banded"]').html()).toContain('vibe-group-header')
    expect(wrapper.find('[data-testid="detail-card"]').exists()).toBe(true)
  })

  it('Edit SQL is Studio-only, loads the source, and saves through UpdateReportSQL', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    await wrapper.find('[data-testid="edit-sql"]').trigger('click')
    await flushPromises()
    expect(app.GetReportSQL).toHaveBeenCalledWith('rpt_parts')
    const input = wrapper.find('[data-testid="sql-input"]')
    expect((input.element as HTMLTextAreaElement).value).toContain('FROM parts_used')

    await input.setValue('SELECT month, qty FROM parts_used WHERE qty > 30')
    await wrapper.find('[data-testid="sql-save"]').trigger('click')
    await flushPromises()
    expect(app.UpdateReportSQL).toHaveBeenCalledWith(
      'rpt_parts',
      'SELECT month, qty FROM parts_used WHERE qty > 30',
    )
    // Editor closes and the report re-runs.
    expect(wrapper.find('[data-testid="sql-editor"]').exists()).toBe(false)
  })

  it('a rejected SQL edit surfaces the error and keeps the editor open', async () => {
    makeApp({ sqlError: 'report SQL failed validation: Binder Error' })
    const wrapper = await mountView()

    await wrapper.find('[data-testid="edit-sql"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="sql-save"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="sql-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sql-error"]').text()).toContain('failed validation')
  })

  it('hides Edit SQL in App Mode', async () => {
    makeApp()
    useAppMode().setMode('app')
    const wrapper = await mountView()

    expect(wrapper.find('[data-testid="edit-sql"]').exists()).toBe(false)
    // The agent affordance is builder-only too (the shell refuses to open the
    // drawer in App Mode, so the card would be a dead button).
    expect(wrapper.find('[data-testid="ask-agent-card"]').exists()).toBe(false)
    // The rest of the dashboard still renders for App users.
    expect(wrapper.find('[data-testid="chart-card"]').exists()).toBe(true)
  })

  it('Export CSV passes the active filters and surfaces the written path', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    await wrapper.find('[data-testid="facet-month"] [data-testid="facet-toggle"]').trigger('click')
    await wrapper.findAll('[data-testid="facet-option"]')[1].setValue(true) // 'Jan' (sorted: Feb, Jan)
    await flushPromises()

    await wrapper.find('[data-testid="export-csv"]').trigger('click')
    await flushPromises()

    expect(app.ExportReportCSV).toHaveBeenCalledWith(
      'rpt_parts',
      [{ column: 'month', op: 'in', values: ['Jan'] }],
      '',
    )
    expect(wrapper.find('[data-testid="report-status"]').text()).toContain('/tmp/parts.csv')
  })

  it('"Modify this report" dispatches the vibe:open-agent window event', async () => {
    makeApp()
    const wrapper = await mountView()
    const seen = vi.fn()
    window.addEventListener('vibe:open-agent', seen)
    await wrapper.find('[data-testid="modify-report"]').trigger('click')
    expect(seen).toHaveBeenCalledTimes(1)
    window.removeEventListener('vibe:open-agent', seen)
  })
})
