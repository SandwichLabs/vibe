// Component tests for the P2.9 datasheet visual-querying UX in TableBrowserView:
//   - Filter-by-Selection: clicking a cell value adds an `eq` FilterSpec
//     condition and calls GetTableDataFiltered (P2.5) with it
//   - the record-count bar reflects the filtered total_rows returned by the
//     backend
//   - Filter-by-Form: a multi-condition panel builds the correct combinator +
//     conditions FilterSpec and calls GetTableDataFiltered
//   - 'Save as query' persists the active filter via SaveFilterAsQuery (P2.6)
//
// These mount the real component against a mocked window.go.main.App and assert
// the acceptance behaviour end-to-end at the component boundary. They are kept
// in a separate file from the inline-edit suite so the two concerns don't
// entangle.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

// #82: TableBrowserView reads ?table= to pre-select a table (e.g. after
// "Open table →" from Import). Not exercised here, but useRoute() must
// resolve to something.
vi.mock('vue-router', async () => {
  const { reactive } = await import('vue')
  const mockRoute = reactive({ query: {} as Record<string, string> })
  ;(globalThis as any).__mockRoute = mockRoute
  return { useRoute: () => mockRoute, useRouter: () => ({ push: () => {} }) }
})

import TableBrowserView from './TableBrowserView.vue'

interface Row { [k: string]: any }

const groupsOf = (names: string[]) => [{
  connection: 'local', type: 'duckdb', status: 'ok', readOnly: false, default: true,
  tables: names.map((n) => ({ name: n, qualified: n, kind: 'table' })),
}]

function makeApp(opts: {
  rows?: Row[]
  columns?: { name: string; type: string; not_null: boolean; pk: boolean }[]
  // total_rows the FILTERED query returns (drives the record-count bar).
  filteredTotal?: number
  saveFilterAsQuery?: (...a: any[]) => Promise<any>
} = {}) {
  const columns = opts.columns ?? [
    { name: 'id', type: 'INTEGER', not_null: true, pk: true },
    { name: 'name', type: 'TEXT', not_null: false, pk: false },
    { name: 'city', type: 'TEXT', not_null: false, pk: false },
  ]
  const store: Row[] = (opts.rows ?? [
    { id: 1, name: 'Ada', city: 'London' },
    { id: 2, name: 'Grace', city: 'New York' },
    { id: 3, name: 'Linus', city: 'London' },
  ]).map(r => ({ ...r }))

  const toTableData = (rowsIn: Row[], total: number) => ({
    columns: columns.map(c => c.name),
    rows: rowsIn.map(r => columns.map(c => r[c.name] ?? null)),
    page: 1,
    page_size: 50,
    total_rows: total,
    total_pages: 1,
  })

  const app = {
    ListCatalogGroups: vi.fn(async () => groupsOf(['people'])),
    DescribeTable: vi.fn(async () => ({ name: 'people', columns })),
    ExecuteQuery: vi.fn(async () => ({ columns: [], rows: [], duration: '0s', row_count: 0 })),
    GetTableData: vi.fn(async () => toTableData(store, store.length)),
    // The filtered endpoint returns the (mocked) filtered subset + total.
    GetTableDataFiltered: vi.fn(async (_name: string, _spec: any) => {
      const total = opts.filteredTotal ?? 2
      return toTableData(store.slice(0, total), total)
    }),
    SaveFilterAsQuery: vi.fn(async (...a: any[]) => {
      if (opts.saveFilterAsQuery) return opts.saveFilterAsQuery(...a)
      return 'query-1'
    }),
    UpdateRow: vi.fn(async () => ({ affected: 1 })),
    InsertRow: vi.fn(async () => ({ affected: 1 })),
    DeleteRow: vi.fn(async () => ({ affected: 1 })),
    CreateTable: vi.fn(async () => {}),
    DropTable: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, store }
}

function mountView() {
  return mount(TableBrowserView, {
    global: { stubs: { Icon: true, ConfirmDialog: true } },
  })
}

async function openTable(wrapper: any) {
  await flushPromises()
  // The Connections rail is the table list now: opening a table is a route query.
  ;(globalThis as any).__mockRoute.query.table = 'people'
  await flushPromises()
}

describe('TableBrowserView visual querying (P2.9)', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('Filter-by-Selection: clicking a cell value calls GetTableDataFiltered with an eq condition', async () => {
    const { app } = makeApp({ filteredTotal: 2 })
    const wrapper = mountView()
    await openTable(wrapper)

    // The "city" cell of the first row holds "London" — Filter-by-Selection
    // should add an eq filter on that column/value.
    const cityCell = wrapper.findAll('tbody tr')[0]
      .findAll('[data-filter-selection]')
      .find((w: any) => w.attributes('title')?.includes('London'))
    expect(cityCell, 'expected a Filter-by-Selection affordance on the cell value').toBeTruthy()
    await cityCell!.trigger('click')
    await flushPromises()

    expect(app.GetTableDataFiltered).toHaveBeenCalledTimes(1)
    const [tbl, spec] = app.GetTableDataFiltered.mock.calls[0]
    expect(tbl).toBe('people')
    expect(spec.conditions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ column: 'city', op: 'eq', value: 'London' }),
      ]),
    )
  })

  it('the record-count bar reflects the filtered total returned by the backend', async () => {
    makeApp({ filteredTotal: 2 })
    const wrapper = mountView()
    await openTable(wrapper)

    const cityCell = wrapper.findAll('tbody tr')[0]
      .findAll('[data-filter-selection]')
      .find((w: any) => w.attributes('title')?.includes('London'))
    await cityCell!.trigger('click')
    await flushPromises()

    const countBar = wrapper.find('.record-count-bar')
    expect(countBar.exists(), 'expected a record-count bar').toBe(true)
    // The bar reflects the FILTERED total (2), not the unfiltered store size (3).
    expect(countBar.text()).toContain('2')
  })

  it('Filter-by-Form builds the correct multi-condition FilterSpec', async () => {
    const { app } = makeApp({ filteredTotal: 1 })
    const wrapper = mountView()
    await openTable(wrapper)

    // Open the Filter-by-Form panel.
    const fbfBtn = wrapper.findAll('button').find((b: any) => /filter by form/i.test(b.text()))
    expect(fbfBtn, 'expected a "Filter by Form" button').toBeTruthy()
    await fbfBtn!.trigger('click')
    await flushPromises()

    // Two condition rows: name LIKE 'A%' AND city = 'London'.
    const colSelects = wrapper.findAll('select.fbf-column')
    const opSelects = wrapper.findAll('select.fbf-op')
    const valInputs = wrapper.findAll('input.fbf-value')
    expect(colSelects.length, 'expected at least 2 Filter-by-Form condition rows').toBeGreaterThanOrEqual(2)

    await colSelects[0].setValue('name')
    await opSelects[0].setValue('like')
    await valInputs[0].setValue('A%')

    await colSelects[1].setValue('city')
    await opSelects[1].setValue('eq')
    await valInputs[1].setValue('London')

    const applyBtn = wrapper.findAll('button').find((b: any) => /^apply/i.test(b.text()))
    expect(applyBtn, 'expected an Apply button in the Filter-by-Form panel').toBeTruthy()
    await applyBtn!.trigger('click')
    await flushPromises()

    expect(app.GetTableDataFiltered).toHaveBeenCalled()
    const [, spec] = app.GetTableDataFiltered.mock.calls.at(-1)!
    expect(spec.combinator).toBe('AND')
    expect(spec.conditions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ column: 'name', op: 'like', value: 'A%' }),
        expect.objectContaining({ column: 'city', op: 'eq', value: 'London' }),
      ]),
    )
  })

  it("'Save as query' persists the active filter via SaveFilterAsQuery", async () => {
    const { app } = makeApp({ filteredTotal: 2 })
    const wrapper = mountView()
    await openTable(wrapper)

    // Apply a Filter-by-Selection first so there is an active filter to save.
    const cityCell = wrapper.findAll('tbody tr')[0]
      .findAll('[data-filter-selection]')
      .find((w: any) => w.attributes('title')?.includes('London'))
    await cityCell!.trigger('click')
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b: any) => /save as query/i.test(b.text()))
    expect(saveBtn, 'expected a "Save as query" button').toBeTruthy()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(app.SaveFilterAsQuery).toHaveBeenCalledTimes(1)
    const args = app.SaveFilterAsQuery.mock.calls[0]
    // (name, spec, columns, title) per the P2.6 backend signature.
    expect(args[0]).toBe('people')
    expect(args[1].conditions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ column: 'city', op: 'eq', value: 'London' }),
      ]),
    )
  })
})
