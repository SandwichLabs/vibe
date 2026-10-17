// ArtifactView / ArtifactsView (#162): the list, the view with its controls, the
// broken-artifact state, and the rule that a view never reaches a raw sql binding.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import { _resetQueryPoolForTests } from '../a2ui/query'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import ArtifactView from './ArtifactView.vue'
import ArtifactsView from './ArtifactsView.vue'

const detail = (over: Partial<ArtifactDetail> = {}): ArtifactDetail => ({
  name: 'sales', title: 'Regional sales', description: 'Revenue by region', rev: 'r1', updated_at: '',
  params: [
    { name: 'from', type: 'date', default: 'today-90d' },
    { name: 'to', type: 'date', default: 'today' },
    { name: 'region', type: 'enum', default: '', options_query: 'regions' },
  ],
  queries: [{ name: 'regions', uses: [] }, { name: 'by_region', uses: ['from', 'to'] }, { name: 'customers', uses: ['region'] }],
  defaults: { from: '2026-01-01', to: '2026-03-01', region: '' },
  view: {
    title: 'Sales',
    components: [
      { id: 'c', type: 'container', layout: 'row' },
      { id: 'd', type: 'dateRange', parent: 'c', start: 'from', end: 'to' },
      { id: 'r', type: 'select', parent: 'c', param: 'region' },
      { id: 'b', type: 'barChart', query: 'by_region' },
      { id: 't', type: 'table', query: 'customers' },
      { id: 's', type: 'stat', label: 'Inline', value: 7 },
    ],
  },
  ...over,
})

function makeApp(d: ArtifactDetail, over: MockApp = {}) {
  const app: MockApp = {
    GetArtifact: vi.fn(async () => d),
    GetArtifactSource: vi.fn(async () => ({ name: d.name, manifest: 'title: x\n', view: '{}', manifest_missing: false, view_missing: false, rev: d.rev, updated_at: '' })),
    CheckArtifact: vi.fn(async () => []),
    ListArtifacts: vi.fn(async () => []),
    RunA2UIQuery: vi.fn(async () => ({ columns: [], types: [], rows: [] })),
    RunArtifactQuery: vi.fn(async (_n: string, q: string) => {
      if (q === 'regions') return { columns: ['region'], types: ['VARCHAR'], rows: [['East'], ['West']] }
      if (q === 'by_region') return { columns: ['region', 'revenue'], types: ['VARCHAR', 'INTEGER'], rows: [['East', 5], ['West', 9]] }
      return { columns: ['customer'], types: ['VARCHAR'], rows: [['Acme']] }
    }),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountAt(name: string) {
  await router.push(`/artifacts/${name}`)
  await router.isReady()
  const w = mount(ArtifactView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return w
}

describe('ArtifactView', () => {
  beforeEach(() => { _resetQueryPoolForTests() })

  it('renders the page Go returned, runs each query once, and shows option rows as text', async () => {
    const app = makeApp(detail())
    const w = await mountAt('sales')
    expect(w.get('[data-testid="artifact-title"]').text()).toBe('Regional sales')
    const calls = (app.RunArtifactQuery as any).mock.calls.map((c: any[]) => c[1]).sort()
    expect(calls).toEqual(['by_region', 'customers', 'regions'])
    expect((app.RunArtifactQuery as any).mock.calls.find((c: any[]) => c[1] === 'by_region')).toEqual(['sales', 'by_region', { from: '2026-01-01', to: '2026-03-01' }])
    expect(w.text()).toContain('Acme')
    expect(w.text()).toContain('Inline')
    const opts = w.findAll('select option').map((o) => o.text())
    expect(opts).toEqual(['All', 'East', 'West'])
    expect(app.RunA2UIQuery).not.toHaveBeenCalled()
  })

  it('changing the select re-runs only the dependent query, with a typed value from the option list', async () => {
    const app = makeApp(detail())
    const w = await mountAt('sales')
    ;(app.RunArtifactQuery as any).mockClear()
    const select = w.get('select')
    ;(select.element as HTMLSelectElement).selectedIndex = 2
    await select.trigger('change')
    await flushPromises()
    expect(app.RunArtifactQuery).toHaveBeenCalledTimes(1)
    expect(app.RunArtifactQuery).toHaveBeenCalledWith('sales', 'customers', { region: 'West' })
    expect(w.find('.a2ui-control__reset').exists()).toBe(true)
    await w.get('.a2ui-control__reset').trigger('click')
    await flushPromises()
    expect(app.RunArtifactQuery).toHaveBeenLastCalledWith('sales', 'customers', { region: '' })
  })

  it('a date the control refuses runs nothing and shows the error', async () => {
    const app = makeApp(detail())
    const w = await mountAt('sales')
    ;(app.RunArtifactQuery as any).mockClear()
    const input = w.findAll('input[type="date"]')[0]
    ;(input.element as HTMLInputElement).value = ''
    await input.trigger('change')
    await flushPromises()
    expect(app.RunArtifactQuery).not.toHaveBeenCalled()
    expect(w.get('[role="alert"]').text()).toMatch(/valid date/)
  })

  it('opens a broken artifact to its errors, as text', async () => {
    makeApp({ ...detail(), view: undefined, error: 'invalid artifact: view.a2ui.json:\n<img src=x onerror=alert(1)>' })
    const w = await mountAt('sales')
    const box = w.get('[data-testid="artifact-errors"]')
    expect(box.text()).toContain('<img src=x onerror=alert(1)>')
    expect(box.find('img').exists()).toBe(false)
    expect(w.find('[data-testid="a2ui-page"]').exists()).toBe(false)
  })

  it('a missing artifact and a bad route name say so without touching a run binding', async () => {
    const app = makeApp(detail(), { GetArtifact: vi.fn(async () => { throw new Error('there is no artifact named "gone"') }) })
    const w = await mountAt('gone')
    expect(w.get('[data-testid="artifact-failure"]').text()).toContain('no artifact named')
    const bad = await mountAt('..%2Fetc')
    expect(bad.get('[data-testid="artifact-failure"]').text()).toContain('not a valid artifact name')
    expect(app.RunArtifactQuery).not.toHaveBeenCalled()
  })

  it('a view carrying a raw sql component renders, shows it refused, and never reaches a run binding', async () => {
    const d = detail({
      view: { title: 'X', components: [{ id: 'q', type: 'table', sql: 'SELECT 1' }, { id: 's', type: 'stat', label: 'x', sql: 'SELECT 2' }] },
      queries: [],
    })
    const app = makeApp(d)
    const w = await mountAt('sales')
    expect(w.text()).toContain('only use its own named queries')
    expect(app.RunArtifactQuery).not.toHaveBeenCalled()
    expect(app.RunA2UIQuery).not.toHaveBeenCalled()
    expect(app.RunA2UIQueryIn).toBeUndefined()
  })

  it('a view edit that re-points a component at another query shows the new one', async () => {
    const mk = (rev: string, query: string) => detail({
      rev, params: [], defaults: {}, queries: [{ name: 'a', uses: [] }, { name: 'b', uses: [] }],
      view: { title: 'X', components: [{ id: 't', type: 'table', query }] },
    })
    let current = mk('r1', 'a')
    const app = makeApp(current, {
      GetArtifact: vi.fn(async () => current),
      RunArtifactQuery: vi.fn(async (_n: string, q: string) => ({ columns: ['v'], types: ['VARCHAR'], rows: [[`from-${q}`]] })),
    })
    const w = await mountAt('sales')
    expect(w.text()).toContain('from-a')
    current = mk('r2', 'b')
    await w.get('[data-testid="artifact-refresh"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('from-b')
    expect(w.text()).not.toContain('from-a')
    ;(app.RunArtifactQuery as any).mockClear()
    await w.get('[data-testid="artifact-refresh"]').trigger('click')
    await flushPromises()
    expect((app.RunArtifactQuery as any).mock.calls.map((c: any[]) => c[1])).toEqual(['b'])
  })

  it('a query error shows inline and a truncated result is labelled', async () => {
    makeApp(detail(), {
      RunArtifactQuery: vi.fn(async (_n: string, q: string) => {
        if (q === 'customers') throw new Error('failed to run: Binder Error: no column')
        return { columns: ['a', 'b'], types: ['VARCHAR', 'INTEGER'], rows: [['x', 1]], truncated: q === 'by_region' }
      }),
    })
    const w = await mountAt('sales')
    expect(w.text()).toContain('QUERY ERROR: Binder Error: no column')
    expect(w.get('[data-testid="artifact-truncated"]').text()).toContain('by_region')
  })
})

describe('ArtifactsView', () => {
  it('lists good and broken artifacts', async () => {
    makeApp(detail(), {
      ListArtifacts: vi.fn(async () => [
        { name: 'broken', title: '', description: '', view: '', updated_at: '', rev: '', error: 'invalid artifact: bad' },
        { name: 'sales', title: 'Regional sales', description: 'Revenue', view: 'a2ui', updated_at: '', rev: 'r' },
      ]),
    })
    await router.push('/artifacts')
    const w = mount(ArtifactsView, { global: { plugins: [router], stubs: { Icon: true } } })
    await flushPromises()
    const rows = w.findAll('[data-testid="artifact-row"]')
    expect(rows.map((r) => r.attributes('data-artifact-name'))).toEqual(['broken', 'sales'])
    expect(rows[0].text()).toContain('invalid artifact: bad')
    expect(rows[1].text()).toContain('Regional sales')
  })

  it('says how to make one when there are none', async () => {
    makeApp(detail())
    await router.push('/artifacts')
    const w = mount(ArtifactsView, { global: { plugins: [router], stubs: { Icon: true } } })
    await flushPromises()
    expect(w.find('[data-testid="artifacts-empty"]').exists()).toBe(true)
  })
})
