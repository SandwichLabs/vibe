// Component tests for RelationshipsView (P2.8) — the Schema Builder ER-diagram
// view that translates the #3 schema-main mockup into Vue.
//
// These mount the real component against a mocked window.go.main.App and assert
// the acceptance behaviour end-to-end at the component boundary:
//   - existing relationships from ListRelationships render as parent -> child
//     with the cascade (ON DELETE) label
//   - the add-relationship form submit calls AddForeignKey with the chosen
//     parent/child tables + columns and the on_delete (cascade) option
//   - a rejected AddForeignKey surfaces a coral error toast (var(--sh-error))
//   - remove calls RemoveForeignKey with the relationship id
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import RelationshipsView from './RelationshipsView.vue'

interface Rel {
  id: string
  parent_table: string
  parent_column: string
  child_table: string
  child_column: string
  on_delete: string
  on_update?: string
  enforce_integrity?: boolean
  created_at?: string
}

function makeApp(opts: {
  relationships?: Rel[]
  tables?: string[]
  columns?: Record<string, { name: string; type: string; not_null: boolean; pk: boolean }[]>
  addForeignKey?: (...a: any[]) => Promise<any>
} = {}) {
  const tables = opts.tables ?? ['customers', 'orders']
  const columns = opts.columns ?? {
    customers: [
      { name: 'id', type: 'INTEGER', not_null: true, pk: true },
      { name: 'name', type: 'VARCHAR', not_null: false, pk: false },
    ],
    orders: [
      { name: 'id', type: 'INTEGER', not_null: true, pk: true },
      { name: 'customer_id', type: 'INTEGER', not_null: false, pk: false },
    ],
  }
  // store is the mutable relationship set so add/remove reflect on reload.
  const store: Rel[] = (opts.relationships ?? [{
    id: 'rel-1',
    parent_table: 'customers',
    parent_column: 'id',
    child_table: 'orders',
    child_column: 'customer_id',
    on_delete: 'CASCADE',
    on_update: 'NO ACTION',
    enforce_integrity: true,
    created_at: '',
  }]).map(r => ({ ...r }))

  const app = {
    ListTables: vi.fn(async () => tables),
    DescribeTable: vi.fn(async (t: string) => ({ name: t, columns: columns[t] ?? [] })),
    ListRelationships: vi.fn(async () => store.map(r => ({ ...r }))),
    AddForeignKey: vi.fn(async (spec: any) => {
      if (opts.addForeignKey) return opts.addForeignKey(spec)
      const id = 'rel-' + (store.length + 1)
      store.push({
        id,
        parent_table: spec.parent_table,
        parent_column: spec.parent_column,
        child_table: spec.child_table,
        child_column: spec.child_column,
        on_delete: spec.on_delete,
      })
      return id
    }),
    RemoveForeignKey: vi.fn(async (id: string) => {
      const i = store.findIndex(r => r.id === id)
      if (i >= 0) store.splice(i, 1)
    }),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, store }
}

function mountView() {
  return mount(RelationshipsView, {
    global: {
      stubs: {
        Icon: true,
        ConfirmDialog: true,
      },
    },
  })
}

describe('RelationshipsView (P2.8)', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('renders existing relationships as parent -> child with a cascade label', async () => {
    const { app } = makeApp()
    const wrapper = mountView()
    await flushPromises()

    expect(app.ListRelationships).toHaveBeenCalled()
    const text = wrapper.text()
    // parent and child tables both surfaced
    expect(text).toContain('customers')
    expect(text).toContain('orders')
    // the relationship column endpoints surfaced
    expect(text).toContain('id')
    expect(text).toContain('customer_id')
    // the cascade (ON DELETE) label surfaced
    expect(text).toContain('CASCADE')
  })

  it('submitting the add-relationship form calls AddForeignKey with the chosen tables/columns/on_delete', async () => {
    const { app } = makeApp({ relationships: [] })
    const wrapper = mountView()
    await flushPromises()

    // Open the add-relationship form.
    const addBtn = wrapper.findAll('button').find((b: any) => b.text().includes('Add relationship'))
    expect(addBtn, 'expected an "Add relationship" button').toBeTruthy()
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill the parent/child table + column selects and cascade option.
    const parentTable = wrapper.find('select.rel-parent-table')
    const parentColumn = wrapper.find('select.rel-parent-column')
    const childTable = wrapper.find('select.rel-child-table')
    const childColumn = wrapper.find('select.rel-child-column')
    const onDelete = wrapper.find('select.rel-on-delete')
    expect(parentTable.exists()).toBe(true)
    expect(childTable.exists()).toBe(true)

    await parentTable.setValue('customers')
    await flushPromises()
    await parentColumn.setValue('id')
    await childTable.setValue('orders')
    await flushPromises()
    await childColumn.setValue('customer_id')
    await onDelete.setValue('CASCADE')

    const saveBtn = wrapper.findAll('button').find((b: any) =>
      /save|create|add foreign key|^add$/i.test(b.text()))
    expect(saveBtn, 'expected a save/create button in the add form').toBeTruthy()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(app.AddForeignKey).toHaveBeenCalledTimes(1)
    expect(app.AddForeignKey).toHaveBeenCalledWith(expect.objectContaining({
      parent_table: 'customers',
      parent_column: 'id',
      child_table: 'orders',
      child_column: 'customer_id',
      on_delete: 'CASCADE',
    }))
  })

  it('a rejected AddForeignKey surfaces a coral error toast', async () => {
    makeApp({
      relationships: [],
      addForeignKey: async () => { throw { message: 'FK conflict: orphan rows exist' } },
    })
    const wrapper = mountView()
    await flushPromises()

    const addBtn = wrapper.findAll('button').find((b: any) => b.text().includes('Add relationship'))
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('select.rel-parent-table').setValue('customers')
    await flushPromises()
    await wrapper.find('select.rel-parent-column').setValue('id')
    await wrapper.find('select.rel-child-table').setValue('orders')
    await flushPromises()
    await wrapper.find('select.rel-child-column').setValue('customer_id')
    await wrapper.find('select.rel-on-delete').setValue('RESTRICT')

    const saveBtn = wrapper.findAll('button').find((b: any) =>
      /save|create|add foreign key|^add$/i.test(b.text()))
    await saveBtn!.trigger('click')
    await flushPromises()

    const toast = wrapper.find('.error-toast')
    expect(toast.exists()).toBe(true)
    expect(toast.text()).toContain('FK conflict')
  })

  it('removing a relationship calls RemoveForeignKey with the relationship id', async () => {
    const { app } = makeApp()
    const wrapper = mountView()
    await flushPromises()

    const removeBtn = wrapper.findAll('button').find((b: any) =>
      /remove|delete/i.test(b.text()))
    expect(removeBtn, 'expected a remove button on the relationship').toBeTruthy()
    await removeBtn!.trigger('click')
    await flushPromises()

    expect(app.RemoveForeignKey).toHaveBeenCalledWith('rel-1')
  })
})
