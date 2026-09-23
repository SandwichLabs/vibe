// Supplementary component tests for the Query Explorer (parity W5).
//
// NOT the acceptance gate — that is Rodney driving :34115 against the real
// backend (docs/parity-remediation-plan.md §1). These cover the logic that's
// awkward to prove via the live DOM: lazy tree expansion with PK/FK badge
// derivation, run-metadata state, export wiring, and the Ask-agent event.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import QueryEditorView from './QueryEditorView.vue'

function makeApp(opts: { execError?: string } = {}) {
  const app = {
    ListQueries: vi.fn(async () => [{ id: 'q1', title: 'Top customers' }]),
    GetQuery: vi.fn(async () => ({ id: 'q1', title: 'Top customers', sql: 'SELECT 1' })),
    SaveQuery: vi.fn(async () => {}),
    DeleteQuery: vi.fn(async () => {}),
    ListTableStats: vi.fn(async () => [
      { name: 'customers', rows: 89 },
      { name: 'service_calls', rows: 142 },
    ]),
    DescribeTable: vi.fn(async (name: string) => ({
      name,
      columns: [
        { name: 'id', type: 'BIGINT', not_null: true, pk: true, fk: false },
        { name: 'customer_id', type: 'BIGINT', not_null: false, pk: false, fk: true },
        { name: 'status', type: 'VARCHAR', not_null: false, pk: false, fk: false },
      ],
    })),
    ExecuteQuery: vi.fn(async () => {
      if (opts.execError) throw new Error(opts.execError)
      return {
        columns: [{ name: 'x' }],
        rows: [[1]],
        duration: '4.2ms',
        row_count: 1,
      }
    }),
    ExportQueryResults: vi.fn(async (_sql: string, format: string) => `/tmp/out.${format}`),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountView() {
  const wrapper = mount(QueryEditorView, { global: { stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

describe('QueryEditorView (W5 Query Explorer)', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders the schema tree with row counts from ListTableStats', async () => {
    makeApp()
    const wrapper = await mountView()

    expect(wrapper.find('[data-testid="schema-tree"]').exists()).toBe(true)
    const tables = wrapper.findAll('[data-testid="tree-table"]')
    expect(tables).toHaveLength(2)
    expect(tables[0].text()).toContain('customers')
    expect(tables[0].text()).toContain('89')
    expect(tables[1].text()).toContain('142')
  })

  it('expanding a table lazily describes it and badges PK/FK columns', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    const calls = wrapper.findAll('[data-testid="tree-table"]')[1]
    await calls.trigger('click')
    await flushPromises()

    expect(app.DescribeTable).toHaveBeenCalledWith('service_calls')
    const cols = wrapper.findAll('[data-testid="tree-col"]')
    expect(cols).toHaveLength(3)
    expect(cols[0].text()).toContain('PK · int')
    expect(cols[1].text()).toContain('FK · int')
    expect(cols[2].text()).toContain('text')

    // Collapse on second click; no re-describe on re-expand (cached).
    await calls.trigger('click')
    expect(wrapper.findAll('[data-testid="tree-col"]')).toHaveLength(0)
    await calls.trigger('click')
    expect(app.DescribeTable).toHaveBeenCalledTimes(1)
  })

  it('double-clicking a tree table seeds a starter SELECT', async () => {
    makeApp()
    const wrapper = await mountView()
    await wrapper.findAll('[data-testid="tree-table"]')[0].trigger('dblclick')
    const input = wrapper.find('[data-testid="sql-input"]').element as HTMLTextAreaElement
    expect(input.value).toBe('SELECT * FROM "customers" LIMIT 100')
  })

  it('a successful run surfaces the metadata bar: Complete badge, DuckDB label, last-run time', async () => {
    makeApp()
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="run-meta"]').exists()).toBe(false)

    await wrapper.find('[data-testid="sql-input"]').setValue('SELECT 1 AS x')
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    const meta = wrapper.find('[data-testid="run-meta"]')
    expect(meta.exists()).toBe(true)
    expect(wrapper.find('[data-testid="run-badge"]').text()).toContain('Complete')
    expect(wrapper.find('[data-testid="run-engine"]').text()).toContain('DuckDB')
    expect(wrapper.find('[data-testid="run-last"]').text()).toContain('Last run ·')
    expect(meta.text()).toContain('1')
    expect(meta.text()).toContain('4.2ms')
  })

  it('a failed run shows the Error badge with the message', async () => {
    makeApp({ execError: 'Binder Error: nope' })
    const wrapper = await mountView()
    await wrapper.find('[data-testid="sql-input"]').setValue('SELECT nope')
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="run-badge"]').text()).toContain('Error')
    expect(wrapper.text()).toContain('Binder Error: nope')
  })

  it('CSV / Parquet buttons export the current SQL via ExportQueryResults', async () => {
    const app = makeApp()
    const wrapper = await mountView()

    // Disabled with no SQL.
    expect((wrapper.find('[data-testid="export-csv"]').element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.find('[data-testid="sql-input"]').setValue('SELECT * FROM customers')
    await wrapper.find('[data-testid="export-csv"]').trigger('click')
    await flushPromises()
    expect(app.ExportQueryResults).toHaveBeenCalledWith('SELECT * FROM customers', 'csv', '')

    await wrapper.find('[data-testid="export-parquet"]').trigger('click')
    await flushPromises()
    expect(app.ExportQueryResults).toHaveBeenCalledWith('SELECT * FROM customers', 'parquet', '')

    // The written path is surfaced.
    expect(wrapper.find('[data-testid="query-status"]').text()).toContain('/tmp/out.parquet')
  })

  it('Ask agent dispatches the vibe:open-agent window event', async () => {
    makeApp()
    const wrapper = await mountView()
    const seen = vi.fn()
    window.addEventListener('vibe:open-agent', seen)
    await wrapper.find('[data-testid="ask-agent"]').trigger('click')
    expect(seen).toHaveBeenCalledTimes(1)
    window.removeEventListener('vibe:open-agent', seen)
  })

  it('the Unsaved badge tracks divergence from the loaded saved query', async () => {
    makeApp()
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="query-dirty"]').exists()).toBe(false)

    await wrapper.find('[data-testid="saved-query"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="query-dirty"]').exists()).toBe(false)

    await wrapper.find('[data-testid="sql-input"]').setValue('SELECT 2')
    expect(wrapper.find('[data-testid="query-dirty"]').exists()).toBe(true)
  })
})
