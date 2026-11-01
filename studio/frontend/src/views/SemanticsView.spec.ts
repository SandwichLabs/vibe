// SemanticsView (E4): the grouped list, a definition's card, Try it and Run
// assertions, a join's profile, the glossary, and the proposals inbox: the diff, Accept
// pinned to the version on screen, Accept anyway for a changed file, and the re-read
// when a proposal changed again.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import SemanticsView from './SemanticsView.vue'

const metric: SemanticMetric = {
  name: 'net_revenue', title: 'Net revenue', description: 'Revenue after refunds.', query: 'net_revenue_base', measure: 'sum(amount)',
  dimensions: ['region', 'month'], format: 'currency', settled_by: { text: 'A counted refunds, B did not. Refunds are excluded.' },
  assertions: [{ kind: 'non_negative' }],
}
const LIST: DefinitionList = {
  entities: [{ kind: 'entity', name: 'customer', summary: 'one row per customer' }],
  metrics: [{ kind: 'metric', name: 'net_revenue', title: 'Net revenue' }, { kind: 'metric', name: 'broken_one', error: 'bad yaml' }],
  joins: [{ kind: 'join', name: 'orders_customers' }],
  terms: ['Churn'], open_proposals: 1,
}
const detailOf = (over: Partial<DefinitionDetail>): DefinitionDetail => ({ kind: 'metric', name: 'net_revenue', text: 'version: 1\n', rev: 'r1', ...over })
const DETAILS: Record<string, DefinitionDetail> = {
  'metric/net_revenue': detailOf({ metric }),
  'metric/broken_one': detailOf({ name: 'broken_one', text: 'nope: [', error: 'line 1: did not find expected node' }),
  'entity/customer': detailOf({ kind: 'entity', name: 'customer', entity: {
    name: 'customer', grain: 'one row per customer', table: 'prod_pg.public.customers', id_column: 'customer_id',
    columns: [{ name: 'email', pii: true }, { name: 'plan' }], assertions: [{ kind: 'unique', column: 'customer_id' }],
  } }),
  'join/orders_customers': detailOf({ kind: 'join', name: 'orders_customers', join: {
    name: 'orders_customers', left: 'order', right: 'customer', on: [{ left: 'customer_id', right: 'customer_id' }], cardinality: 'many_to_one',
  } }),
  'glossary/': detailOf({ kind: 'glossary', name: 'glossary.md', text: '## Churn\n\nLeft <script>alert(1)</script>.\n', glossary: { text: '## Churn\n\nLeft <script>alert(1)</script>.\n', terms: ['Churn'] } }),
}
const PROPOSAL: Proposal = {
  id: '20261029T153012Z-metric-orders-7f3a', kind: 'metric', name: 'orders', status: 'open', proposed_at: '2026-10-29T15:30:12Z', proposed_by: 'agent',
  rationale: 'Orders are counted once per order id.', evidence: ['run 1: 120 rows'], replaces_rev: '', decided_at: '',
}
const preview = (over: Partial<ProposalPreview> = {}): ProposalPreview => ({
  proposal: PROPOSAL, target: 'semantics/metrics/orders.yaml', exists: false, current_text: '', current_rev: '',
  proposed_text: 'version: 1\nname: orders\nmeasure: count(*)\n', stale: false, shown_rev: 'pin-1', warnings: [], ...over,
})
const result = (over: Partial<QueryResult> = {}): QueryResult => ({
  columns: [{ name: 'region', type: 'VARCHAR' }, { name: 'net_revenue', type: 'BIGINT' }] as ColumnInfo[], rows: [['east', 10], ['west', 5]], duration: '4 ms', row_count: 2, total_rows: 2, ...over,
})

function makeApp(overrides: MockApp = {}) {
  const app: MockApp = {
    ListDefinitions: vi.fn(async () => structuredClone(LIST)),
    GetDefinition: vi.fn(async (kind: string, name: string) => structuredClone(DETAILS[`${kind}/${name}`])),
    ListProposals: vi.fn(async () => [PROPOSAL]),
    GetProposal: vi.fn(async () => preview()),
    AcceptProposal: vi.fn(async () => {}),
    RejectProposal: vi.fn(async () => {}),
    RunMetric: vi.fn(async () => result()),
    RunAssertions: vi.fn(async () => [{ subject_kind: 'metric', subject: 'net_revenue', assertion: 'compile', status: 'pass', violations: 0, duration_ms: 1 }, { subject_kind: 'metric', subject: 'net_revenue', assertion: 'non_negative', status: 'pass', violations: 0, duration_ms: 3 }]),
    ProfileJoin: vi.fn(async () => ({
      join: 'orders_customers', declared: 'many_to_one', rev: 'r2', left_max_dup: 1,
      measured: { at: '2026-10-29T16:00:00Z', left_rows: 12, right_rows: 4, max_fanout: 3, left_orphans: 0, right_orphans: 0, verdict: 'fan_out' },
      warnings: ['declared many_to_one but a right row matches up to 3 left rows'],
    })),
    ...overrides,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

// Every mounted view is unmounted after its test: a left-over one would keep reacting to the
// next test's route and consume its mocks.
const mounted: Array<{ unmount(): void }> = []
afterEach(() => { for (const w of mounted.splice(0)) w.unmount() })

async function mountAt(query = '') {
  await router.push('/semantics' + query)
  await router.isReady()
  const w = mount(SemanticsView, { global: { plugins: [router], stubs: { Icon: true } } })
  mounted.push(w)
  await flushPromises()
  return w
}

describe('SemanticsView', () => {
  beforeEach(() => vi.clearAllMocks())

  it('groups the definitions, flags a broken file and counts the open proposals', async () => {
    makeApp()
    const w = await mountAt()
    expect(w.get('[data-testid="def-metric-net_revenue"]').text()).toContain('Net revenue')
    expect(w.get('[data-testid="def-metric-broken_one"]').text()).toContain('error')
    expect(w.find('[data-testid="def-entity-customer"]').exists()).toBe(true)
    expect(w.find('[data-testid="def-join-orders_customers"]').exists()).toBe(true)
    expect(w.get('[data-testid="def-glossary"]').text()).toContain('1 term')
    expect(w.get('[data-testid="proposal-count"]').text()).toBe('1')
    expect(w.get('[data-testid="def-proposal-' + PROPOSAL.id + '"]').text()).toContain('orders')
  })

  it('explains how to add definitions when there are none', async () => {
    makeApp({ ListDefinitions: vi.fn(async () => ({ entities: [], metrics: [], joins: [], terms: [], open_proposals: 0 })), ListProposals: vi.fn(async () => []) })
    const w = await mountAt()
    const t = w.get('[data-testid="semantics-empty"]').text()
    expect(t).toContain('semantics/metrics/net_revenue.yaml')
    expect(t).toContain('Ask the assistant')
  })

  it('opens a metric card with its facts and a link to its saved query', async () => {
    const app = makeApp()
    const w = await mountAt('?kind=metric&name=net_revenue')
    expect(app.GetDefinition).toHaveBeenCalledWith('metric', 'net_revenue')
    expect(w.get('[data-testid="metric-title"]').text()).toBe('Net revenue')
    expect(w.get('[data-testid="metric-measure"]').text()).toBe('sum(amount)')
    expect(w.get('[data-testid="metric-dimensions"]').text()).toContain('region')
    expect(w.get('[data-testid="metric-settled-by"]').text()).toContain('Refunds are excluded')
    const link = w.get('[data-testid="metric-query-link"]')
    expect(link.attributes('href')).toContain('open=net_revenue_base')
  })

  it('Try it runs the metric by the ticked dimensions and shows the truncation notice', async () => {
    const app = makeApp({ RunMetric: vi.fn(async () => result({ truncated: true })) })
    const w = await mountAt('?kind=metric&name=net_revenue')
    await w.get('[data-testid="try-dim-region"]').setValue(true)
    await w.get('[data-testid="try-run"]').trigger('click')
    await flushPromises()
    expect(app.RunMetric).toHaveBeenCalledWith('net_revenue', ['region'])
    expect(w.get('[data-testid="result-grid"]').text()).toContain('east')
    expect(w.get('[data-testid="result-truncated"]').text()).toContain('Showing the first 2 rows')
  })

  it('a run that fails shows its error, and a run in flight cannot be started twice', async () => {
    let release!: (r: QueryResult) => void
    const run = vi.fn(() => new Promise<QueryResult>((resolve) => { release = resolve }))
    makeApp({ RunMetric: run })
    const w = await mountAt('?kind=metric&name=net_revenue')
    await w.get('[data-testid="try-run"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="run-progress"]').text()).toContain('Running')
    expect(w.get('[data-testid="try-run"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="try-run"]').trigger('click')
    expect(run).toHaveBeenCalledTimes(1)
    release(result())
    await flushPromises()

    makeApp({ RunMetric: vi.fn(async () => { throw new Error('metric(): the saved query is missing') }) })
    const w2 = await mountAt('?kind=metric&name=net_revenue')
    await w2.get('[data-testid="try-run"]').trigger('click')
    await flushPromises()
    expect(w2.get('[data-testid="run-error"]').text()).toContain('the saved query is missing')
  })

  it('Run assertions shows a results table', async () => {
    const app = makeApp()
    const w = await mountAt('?kind=metric&name=net_revenue')
    await w.get('[data-testid="run-assertions"]').trigger('click')
    await flushPromises()
    expect(app.RunAssertions).toHaveBeenCalledWith('metric', 'net_revenue')
    expect(w.get('[data-testid="assertion-summary"]').text()).toContain('2 passed')
    expect(w.get('[data-testid="assertion-row-0"]').text()).toContain('Compiles')
    expect(w.get('[data-testid="assertion-row-1"]').text()).toContain('non_negative')
  })

  it('an entity card shows the grain, table and personal-data columns', async () => {
    makeApp()
    const w = await mountAt('?kind=entity&name=customer')
    expect(w.get('[data-testid="entity-grain"]').text()).toBe('one row per customer')
    expect(w.get('[data-testid="entity-table"]').text()).toBe('prod_pg.public.customers')
    expect(w.get('[data-testid="entity-pii"]').text()).toContain('email')
    expect(w.get('[data-testid="entity-pii"]').text()).not.toContain('plan')
  })

  it('Profile shows the measured cardinality and the warning when it disagrees', async () => {
    const app = makeApp()
    const w = await mountAt('?kind=join&name=orders_customers')
    expect(w.get('[data-testid="join-declared"]').text()).toBe('many_to_one')
    expect(w.get('[data-testid="join-measured"]').text()).toContain('not measured yet')
    await w.get('[data-testid="join-profile"]').trigger('click')
    await flushPromises()
    expect(app.ProfileJoin).toHaveBeenCalledWith('orders_customers')
    expect(w.get('[data-testid="join-measured"]').text()).toContain('fan_out')
    expect(w.get('[data-testid="join-warning"]').text()).toContain('up to 3 left rows')
  })

  it('a stale join file is shown as a conflict, not an error', async () => {
    makeApp({ ProfileJoin: vi.fn(async () => { throw new Error('conflict: semantics/joins/orders_customers.yaml changed') }) })
    const w = await mountAt('?kind=join&name=orders_customers')
    await w.get('[data-testid="join-profile"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="run-conflict"]').text()).toContain('nothing was written')
    expect(w.find('[data-testid="run-error"]').exists()).toBe(false)
  })

  it('renders the glossary through the safe markdown renderer', async () => {
    makeApp()
    const w = await mountAt('?kind=glossary')
    const html = w.get('[data-testid="glossary-rendered"]').html()
    expect(html).toContain('Churn')
    expect(html).not.toContain('<script')
  })

  it('a file that cannot be read opens as its text with the reason', async () => {
    makeApp()
    const w = await mountAt('?kind=metric&name=broken_one')
    expect(w.get('[data-testid="definition-broken"]').text()).toContain('did not find expected node')
    expect(w.get('[data-testid="definition-broken"]').text()).toContain('nope: [')
  })
})

describe('SemanticsView proposals inbox', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the rationale, evidence and a diff with changed lines marked', async () => {
    makeApp({ GetProposal: vi.fn(async () => preview({ exists: true, current_text: 'version: 1\nname: orders\nmeasure: sum(x)\n', proposed_text: 'version: 1\nname: orders\nmeasure: count(*)\n' })) })
    const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
    expect(w.get('[data-testid="proposal-rationale"]').text()).toContain('counted once per order id')
    expect(w.get('[data-testid="proposal-evidence"]').text()).toContain('run 1: 120 rows')
    const ops = w.findAll('[data-testid="proposal-diff"] [data-op]').map((l) => l.attributes('data-op'))
    expect(ops).toEqual([' ', ' ', '-', '+'])
    expect(w.get('[data-testid="proposal-diff-head"]').text()).toContain('1 added, 1 removed')
  })

  it('Accept passes the shown rev, then the new metric card appears', async () => {
    const app = makeApp()
    const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
    await w.get('[data-testid="proposal-accept"]').trigger('click')
    await flushPromises()
    expect(app.AcceptProposal).toHaveBeenCalledWith(PROPOSAL.id, 'pin-1', false)
    expect(router.currentRoute.value.query).toMatchObject({ kind: 'metric', name: 'orders' })
  })

  it('a file that changed since the proposal offers Accept anyway, which overwrites with the shown rev', async () => {
    const app = makeApp({ GetProposal: vi.fn(async () => preview({ exists: true, stale: true, stale_reason: 'changed', current_text: 'x\n' })) })
    const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
    expect(w.get('[data-testid="proposal-stale"]').text()).toContain('The file changed since this was proposed')
    expect(w.get('[data-testid="proposal-accept"]').text()).toBe('Accept anyway')
    await w.get('[data-testid="proposal-accept"]').trigger('click')
    await flushPromises()
    expect(app.AcceptProposal).toHaveBeenCalledWith(PROPOSAL.id, 'pin-1', true)
  })

  it('a proposal that changed again is re-read and its new diff shown, nothing accepted', async () => {
    const get = vi.fn()
      .mockResolvedValueOnce(preview())
      .mockResolvedValueOnce(preview({ shown_rev: 'pin-2', proposed_text: 'version: 1\nname: orders\nmeasure: count(distinct id)\n' }))
    const app = makeApp({ GetProposal: get, AcceptProposal: vi.fn(async () => { throw new Error('conflict: this changed again since you looked') }) })
    const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
    await w.get('[data-testid="proposal-accept"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="proposal-changed-again"]').text()).toContain('changed again since you looked')
    expect(w.get('[data-testid="proposal-diff"]').text()).toContain('count(distinct id)')
    await w.get('[data-testid="proposal-accept"]').trigger('click')
    await flushPromises()
    expect(app.AcceptProposal).toHaveBeenLastCalledWith(PROPOSAL.id, 'pin-2', false)
  })

  it('Accept and Profile are off in a read-only workspace', async () => {
    makeApp()
    const { workspaceInfo } = await import('../composables/useWorkspace')
    workspaceInfo.value = { readOnly: true } as any
    try {
      const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
      expect(w.get('[data-testid="proposal-accept"]').attributes('disabled')).toBeDefined()
      const j = await mountAt('?kind=join&name=orders_customers')
      expect(j.get('[data-testid="join-profile"]').attributes('disabled')).toBeDefined()
    } finally {
      workspaceInfo.value = null
    }
  })

  it('shows both notes when the file changed since proposed and it changed again', async () => {
    const get = vi.fn()
      .mockResolvedValueOnce(preview({ exists: true, stale: true, stale_reason: 'changed', current_text: 'x\n' }))
      .mockResolvedValueOnce(preview({ exists: true, stale: true, stale_reason: 'changed', current_text: 'y\n', shown_rev: 'pin-2' }))
    makeApp({ GetProposal: get, AcceptProposal: vi.fn(async () => { throw new Error('conflict: this changed again since you looked') }) })
    const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
    await w.get('[data-testid="proposal-accept"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="proposal-changed-again"]').exists()).toBe(true)
    expect(w.get('[data-testid="proposal-stale"]').text()).toContain('not a merge')
  })

  it('Reject calls RejectProposal and returns to the inbox', async () => {
    const app = makeApp()
    const w = await mountAt(`?kind=proposal&name=${PROPOSAL.id}`)
    await w.get('[data-testid="proposal-reject"]').trigger('click')
    await flushPromises()
    expect(app.RejectProposal).toHaveBeenCalledWith(PROPOSAL.id)
    expect(router.currentRoute.value.query).toMatchObject({ kind: 'proposal', name: '' })
  })

  it('an empty inbox says what to do', async () => {
    makeApp({ ListProposals: vi.fn(async () => []), ListDefinitions: vi.fn(async () => ({ ...structuredClone(LIST), open_proposals: 0 })) })
    const w = await mountAt('?kind=proposal')
    expect(w.get('[data-testid="proposal-empty"]').text()).toContain('Nothing changes until you do')
  })
})
