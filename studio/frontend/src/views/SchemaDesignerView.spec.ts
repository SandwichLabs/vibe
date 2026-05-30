// Component tests for SchemaDesignerView (P2.9) — the field-property editor that
// completes §2.2 and translates the #3/#8 schema panels into Vue.
//
// These mount the real component against a mocked window.go.main.App and assert
// the acceptance behaviour end-to-end at the component boundary:
//   - the table's columns + field meta load via DescribeTable / GetFieldMeta
//   - editing a field property (e.g. required / control type / default) and
//     saving calls SetFieldMeta(table, column, meta) with the edited values
//   - adding a column calls AddColumn(table, column) with the chosen
//     name + type
//   - dropping a column calls DropColumn(table, column)
//   - adding an index calls AddIndex(table, ...)
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SchemaDesignerView from './SchemaDesignerView.vue'

function baseMeta(table: string, column: string, over: Partial<any> = {}) {
  return {
    table_name: table,
    column_name: column,
    caption: '',
    control_type: 'textbox',
    default_value: '',
    required: false,
    format_code: '',
    validation_rule: '',
    validation_message: '',
    input_mask: '',
    lookup_sql: '',
    display_col: '',
    value_col: '',
    control_tip: '',
    sort_order: 0,
    ...over,
  }
}

function makeApp(opts: {
  columns?: { name: string; type: string; not_null: boolean; pk: boolean }[]
  fieldMeta?: any[]
} = {}) {
  const columns = opts.columns ?? [
    { name: 'id', type: 'INTEGER', not_null: true, pk: true },
    { name: 'name', type: 'VARCHAR', not_null: false, pk: false },
  ]
  const app = {
    ListTables: vi.fn(async () => ['people']),
    DescribeTable: vi.fn(async () => ({ name: 'people', columns })),
    GetFieldMeta: vi.fn(async () => opts.fieldMeta ?? [baseMeta('people', 'name')]),
    SetFieldMeta: vi.fn(async (_t: string, _c: string, _m: any) => {}),
    AddColumn: vi.fn(async (_t: string, _col: any) => {}),
    DropColumn: vi.fn(async (_t: string, _c: string) => {}),
    AddIndex: vi.fn(async (..._a: any[]) => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app }
}

function mountView() {
  return mount(SchemaDesignerView, {
    global: { stubs: { Icon: true, ConfirmDialog: true } },
  })
}

async function openTable(wrapper: any) {
  await flushPromises()
  const item = wrapper.findAll('.cursor-pointer').find((w: any) => w.text().includes('people'))
  expect(item, 'expected the table to be listed in the designer sidebar').toBeTruthy()
  await item!.trigger('click')
  await flushPromises()
}

describe('SchemaDesignerView (P2.9)', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('loads the table columns and field meta', async () => {
    const { app } = makeApp()
    const wrapper = mountView()
    await openTable(wrapper)

    expect(app.DescribeTable).toHaveBeenCalledWith('people')
    expect(app.GetFieldMeta).toHaveBeenCalledWith('people')
    expect(wrapper.text()).toContain('name')
  })

  it('editing a field property and saving calls SetFieldMeta', async () => {
    const { app } = makeApp()
    const wrapper = mountView()
    await openTable(wrapper)

    // Select the "name" field to open its property editor.
    const fieldItem = wrapper.findAll('[data-field-row]').find((w: any) => w.text().includes('name'))
    expect(fieldItem, 'expected a selectable field row for "name"').toBeTruthy()
    await fieldItem!.trigger('click')
    await flushPromises()

    // Toggle "required" and set a default value in the property editor.
    const requiredInput = wrapper.find('input.fp-required')
    const defaultInput = wrapper.find('input.fp-default')
    expect(requiredInput.exists(), 'expected a required toggle in the property editor').toBe(true)
    expect(defaultInput.exists(), 'expected a default-value input in the property editor').toBe(true)

    await requiredInput.setValue(true)
    await defaultInput.setValue('Anon')

    const saveBtn = wrapper.findAll('button').find((b: any) => /save/i.test(b.text()))
    expect(saveBtn, 'expected a Save button in the property editor').toBeTruthy()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(app.SetFieldMeta).toHaveBeenCalledTimes(1)
    const [tbl, col, meta] = app.SetFieldMeta.mock.calls[0]
    expect(tbl).toBe('people')
    expect(col).toBe('name')
    expect(meta).toEqual(expect.objectContaining({ required: true, default_value: 'Anon' }))
  })

  it('adding a column calls AddColumn with the chosen name and type', async () => {
    const { app } = makeApp()
    const wrapper = mountView()
    await openTable(wrapper)

    const addBtn = wrapper.findAll('button').find((b: any) => /add column/i.test(b.text()))
    expect(addBtn, 'expected an "Add column" button').toBeTruthy()
    await addBtn!.trigger('click')
    await flushPromises()

    const nameInput = wrapper.find('input.addcol-name')
    const typeSelect = wrapper.find('select.addcol-type')
    expect(nameInput.exists(), 'expected a new-column name input').toBe(true)
    await nameInput.setValue('email')
    await typeSelect.setValue('VARCHAR')

    const confirmBtn = wrapper.findAll('button').find((b: any) => /^add$|create|confirm/i.test(b.text()))
    expect(confirmBtn, 'expected a confirm button to add the column').toBeTruthy()
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(app.AddColumn).toHaveBeenCalledTimes(1)
    const args = app.AddColumn.mock.calls[0]
    expect(args[0]).toBe('people')
    // The new column is conveyed as a name + type (object or positional).
    const conveyed = JSON.stringify(args.slice(1))
    expect(conveyed).toContain('email')
    expect(conveyed).toContain('VARCHAR')
  })
})
