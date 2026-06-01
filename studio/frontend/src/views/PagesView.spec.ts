// PagesView — reports roll up into the Pages list.
//
// Agent-authored reports previously had no home in the Pages surface; this
// proves they now appear in the left list tagged "report", render via the
// report binding when selected, and route to the Report view on edit.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import PagesView from './PagesView.vue'

const PAGES = [
  { id: 'p1', title: 'Welcome', page_type: 'markdown', sort_order: 0 },
]
const REPORTS = [
  { id: 'r1', title: 'Open jobs', source: 'SELECT 1' },
]

function makeApp() {
  const app = {
    ListPages: vi.fn(async () => PAGES.map((p) => ({ ...p }))),
    ListReports: vi.fn(async () => REPORTS.map((r) => ({ ...r }))),
    RenderPageContent: vi.fn(async () => '<p>page html</p>'),
    RenderReportContent: vi.fn(async () => '<table>report html</table>'),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountView() {
  await router.push('/pages')
  await router.isReady()
  const wrapper = mount(PagesView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

describe('PagesView report rollup', () => {
  beforeEach(() => vi.clearAllMocks())

  it('lists reports alongside pages, tagged "report"', async () => {
    const app = makeApp()
    const wrapper = await mountView()
    expect(app.ListReports).toHaveBeenCalled()
    const text = wrapper.text()
    expect(text).toContain('Welcome')
    expect(text).toContain('Open jobs')
    // The report row carries a 'report' badge.
    expect(wrapper.findAll('.badge--teal').some((b) => b.text() === 'report')).toBe(true)
  })

  it('renders a selected report via RenderReportContent, not RenderPageContent', async () => {
    const app = makeApp()
    const wrapper = await mountView()
    // Pages auto-select the first entry (the page); selecting the report row
    // should hit the report binding with the un-prefixed id.
    const rows = wrapper.findAll('[data-testid="page-row"]')
    const reportRow = rows.find((r) => r.attributes('data-page-id') === 'report:r1')
    await reportRow!.trigger('click')
    await flushPromises()
    expect(app.RenderReportContent).toHaveBeenCalledWith('r1')
  })

  it('survives ListReports failing (pages still render)', async () => {
    const app = makeApp()
    app.ListReports.mockRejectedValueOnce(new Error('no reports table'))
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('Welcome')
    expect(wrapper.text()).not.toContain('Open jobs')
  })
})
