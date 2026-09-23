// Component tests for inline datasheet editing in TableBrowserView (P1.5).
//
// These mount the real component against a mocked window.go.main.App and assert
// the inline edit-in-place / add-row behaviour end-to-end at the component
// boundary:
//   - double-click and single-click both open the inline editor
//   - committing a text edit calls UpdateRow(table, pkName, pkValue, {col: val})
//     with the correct PK and reloads
//   - add-row calls InsertRow and the new row appears after the data reload
//   - a rejected UpdateRow reverts the optimistic change (NOT persisted) and
//     surfaces cell-level feedback identifying the failed cell
//   - the declared SQL column type drives the editor: checkbox, date
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

// Capture the "data:changed" handler so tests can drive it, mirroring
// AgentPanel.spec.ts's approach to the same wailsjs runtime module.
let dataChangedHandler: (() => void) | null = null
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: () => void) => { if (name === 'data:changed') dataChangedHandler = cb },
  EventsOff: () => { dataChangedHandler = null },
}))

import TableBrowserView from './TableBrowserView.vue'

// --- Fake backend (window.go.main.App) ------------------------------------

interface Row { [k: string]: any }

function makeApp(opts: {
  rows?: Row[]
  columns?: { name: string; type: string; not_null: boolean; pk: boolean }[]
  updateRow?: (...a: any[]) => Promise<any>
  lookupRows?: any[][]
  lookupCols?: { name: string }[]
} = {}) {
  const columns = opts.columns ?? [
    { name: 'id', type: 'INTEGER', not_null: true, pk: true },
    { name: 'name', type: 'TEXT', not_null: false, pk: false },
  ]
  // store is the mutable "database" so InsertRow shows up on reload.
  const store: Row[] = (opts.rows ?? [{ id: 1, name: 'Ada' }]).map(r => ({ ...r }))

  const calls: Record<string, any[][]> = {}
  const rec = (name: string) => (...args: any[]) => { (calls[name] ??= []).push(args) }

  const toTableData = () => ({
    columns: columns.map(c => c.name),
    rows: store.map(r => columns.map(c => r[c.name] ?? null)),
    page: 1,
    page_size: 50,
    total_rows: store.length,
    total_pages: 1,
  })

  const app = {
    ListTables: vi.fn(async () => ['people']),
    DescribeTable: vi.fn(async () => ({ name: 'people', columns })),
    ExecuteQuery: vi.fn(async () => ({
      columns: opts.lookupCols ?? [{ name: 'id' }, { name: 'label' }],
      rows: opts.lookupRows ?? [],
      duration: '0s',
      row_count: (opts.lookupRows ?? []).length,
    })),
    GetTableData: vi.fn(async () => toTableData()),
    UpdateRow: vi.fn(async (table: string, pkCol: string, pkVal: any, values: Record<string, any>) => {
      rec('UpdateRow')(table, pkCol, pkVal, values)
      if (opts.updateRow) return opts.updateRow(table, pkCol, pkVal, values)
      const row = store.find(r => String(r[pkCol]) === String(pkVal))
      if (row) Object.assign(row, values)
      return { affected: 1 }
    }),
    InsertRow: vi.fn(async (table: string, values: Record<string, any>) => {
      rec('InsertRow')(table, values)
      store.push({ ...values })
      return { affected: 1 }
    }),
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
    global: {
      stubs: {
        // Child components are irrelevant to inline-editing behaviour.
        Icon: true,
        ConfirmDialog: true,
      },
    },
  })
}

async function openTable(wrapper: any) {
  await flushPromises()
  // Click the table in the sidebar list.
  const item = wrapper.findAll('.cursor-pointer').find((w: any) => w.text().includes('people'))
  await item!.trigger('click')
  await flushPromises()
}

// data cells = all <td> except the trailing actions cell.
function dataCells(wrapper: any) {
  const tds = wrapper.findAll('tbody td')
  // exclude the actions cell (last per row); with 2 cols + 1 action = 3 tds/row
  return tds
}

describe('TableBrowserView inline datasheet editing', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('double-click opens an editor and Enter commits via UpdateRow with the correct PK', async () => {
    const { app } = makeApp({ rows: [{ id: 7, name: 'Ada' }] })
    const wrapper = mountView()
    await openTable(wrapper)

    // The "name" cell is the 2nd td of the row.
    const nameCell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    await nameCell.trigger('dblclick')
    const input = nameCell.find('input.cell-input')
    expect(input.exists()).toBe(true)

    await input.setValue('Grace')
    await input.trigger('keydown.enter')
    await flushPromises()

    expect(app.UpdateRow).toHaveBeenCalledTimes(1)
    expect(app.UpdateRow).toHaveBeenCalledWith('people', 'id', 7, { name: 'Grace' })
    // Reloaded value is persisted.
    expect(wrapper.findAll('tbody tr')[0].findAll('td')[1].text()).toContain('Grace')
  })

  it('single-click also opens the inline editor', async () => {
    makeApp({ rows: [{ id: 1, name: 'Ada' }] })
    const wrapper = mountView()
    await openTable(wrapper)

    const nameCell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    await nameCell.trigger('click')
    expect(nameCell.find('input.cell-input').exists()).toBe(true)
  })

  it('does not open an editor on the primary-key column', async () => {
    makeApp({ rows: [{ id: 1, name: 'Ada' }] })
    const wrapper = mountView()
    await openTable(wrapper)

    const pkCell = wrapper.findAll('tbody tr')[0].findAll('td')[0]
    await pkCell.trigger('dblclick')
    expect(pkCell.find('input').exists()).toBe(false)
  })

  it('add-row inserts via InsertRow and the new row appears after reload', async () => {
    const { app } = makeApp({ rows: [{ id: 1, name: 'Ada' }] })
    const wrapper = mountView()
    await openTable(wrapper)

    // Open the add-row modal.
    const addBtn = wrapper.findAll('button').find((b: any) => b.text().includes('Add row'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill the "name" field in the modal and insert.
    const modalInputs = wrapper.findAll('.modal-overlay .field-input')
    // inputs map to columns id, name (in tableInfo order)
    await modalInputs[1].setValue('Linus')
    const insertBtn = wrapper.findAll('.modal-overlay button').find((b: any) => b.text().includes('Insert'))
    await insertBtn!.trigger('click')
    await flushPromises()

    expect(app.InsertRow).toHaveBeenCalledTimes(1)
    expect(app.InsertRow).toHaveBeenCalledWith('people', { name: 'Linus' })
    // New row visible after reload.
    expect(wrapper.find('tbody').text()).toContain('Linus')
  })

  it('rejected UpdateRow reverts the optimistic change and shows cell-level feedback', async () => {
    const { app } = makeApp({
      rows: [{ id: 1, name: 'Ada' }],
      updateRow: async () => { throw { message: 'name must be unique' } },
    })
    const wrapper = mountView()
    await openTable(wrapper)

    const nameCell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    await nameCell.trigger('dblclick')
    const input = nameCell.find('input.cell-input')
    await input.setValue('Bad')
    await input.trigger('keydown.enter')
    await flushPromises()

    expect(app.UpdateRow).toHaveBeenCalledTimes(1)
    // Optimistic change reverted: the persisted/displayed value is still Ada.
    const cellAfter = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    expect(cellAfter.text()).toContain('Ada')
    expect(cellAfter.text()).not.toContain('Bad')
    // Cell-level feedback present on the failed cell.
    expect(cellAfter.classes()).toContain('cell-error')
    expect(cellAfter.find('.cell-error-msg').text()).toContain('name must be unique')
  })

  it('renders a checkbox editor for boolean columns', async () => {
    makeApp({
      columns: [
        { name: 'id', type: 'INTEGER', not_null: true, pk: true },
        { name: 'active', type: 'BOOLEAN', not_null: false, pk: false },
      ],
      rows: [{ id: 1, active: false }],
    })
    const wrapper = mountView()
    await openTable(wrapper)

    const activeCell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    // Read-only checkbox shown in display mode.
    expect(activeCell.find('input[type="checkbox"]').exists()).toBe(true)
    await activeCell.trigger('dblclick')
    // Editor is a checkbox, not a text input.
    expect(activeCell.find('input.cell-input').exists()).toBe(false)
    expect(activeCell.find('input[type="checkbox"]').exists()).toBe(true)
  })

  it('toggling the checkbox editor commits the boolean via UpdateRow', async () => {
    const { app } = makeApp({
      columns: [
        { name: 'id', type: 'INTEGER', not_null: true, pk: true },
        { name: 'active', type: 'BOOLEAN', not_null: false, pk: false },
      ],
      rows: [{ id: 5, active: false }],
    })
    const wrapper = mountView()
    await openTable(wrapper)

    const activeCell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    await activeCell.trigger('dblclick')
    const cb = activeCell.find('input[type="checkbox"]')
    ;(cb.element as HTMLInputElement).checked = true
    await cb.trigger('change')
    await flushPromises()

    expect(app.UpdateRow).toHaveBeenCalledWith('people', 'id', 5, { active: 'true' })
  })

  it('renders a date editor for date columns', async () => {
    makeApp({
      columns: [
        { name: 'id', type: 'INTEGER', not_null: true, pk: true },
        { name: 'born', type: 'DATE', not_null: false, pk: false },
      ],
      rows: [{ id: 1, born: '1815-12-10' }],
    })
    const wrapper = mountView()
    await openTable(wrapper)

    const cell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    await cell.trigger('dblclick')
    expect(cell.find('input[type="date"]').exists()).toBe(true)
  })
})

// Review fix #6: an agent-driven create_table/create_record/import previously
// left the table list (and the open table's rows) stale until a manual
// reload — this view never listened for "data:changed" (DashboardView did,
// TableBrowserView didn't).
describe('TableBrowserView "data:changed" live refresh', () => {
  beforeEach(() => {
    dataChangedHandler = null
    ;(globalThis as any).window = (globalThis as any).window || {}
    ;(window as any).runtime = {} // hasWailsRuntime() gate
  })

  it('re-lists tables and reloads the open table when data:changed fires', async () => {
    const { app, store } = makeApp({ rows: [{ id: 1, name: 'Ada' }] })
    const wrapper = mountView()
    await openTable(wrapper)

    expect(app.ListTables).toHaveBeenCalledTimes(1)
    expect(app.GetTableData).toHaveBeenCalledTimes(1)
    expect(dataChangedHandler).not.toBeNull()

    // Simulate the agent inserting a row and the backend's emitted event.
    store.push({ id: 2, name: 'Grace' })
    dataChangedHandler!()
    await flushPromises()

    expect(app.ListTables).toHaveBeenCalledTimes(2)
    expect(app.GetTableData).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Grace')
  })

  it('clears the open table if data:changed reveals it was dropped', async () => {
    const { app } = makeApp({ rows: [{ id: 1, name: 'Ada' }] })
    app.ListTables.mockImplementationOnce(async () => ['people']) // initial mount
    const wrapper = mountView()
    await openTable(wrapper)

    app.ListTables.mockImplementation(async () => []) // the agent dropped it
    dataChangedHandler!()
    await flushPromises()

    expect(wrapper.text()).not.toContain('Ada')
    expect(wrapper.text()).toContain('No tables yet')
  })
})
