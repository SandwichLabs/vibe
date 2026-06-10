// Supplementary component tests for the Schema Builder (parity W3).
//
// These are NOT the acceptance gate — that is Rodney driving :34115 against the
// real backend (see docs/parity-remediation-plan.md §1). These cover the logic
// that is awkward to prove through the live DOM: the propose_schema stream →
// draft-card pipeline, the existing-table skip on commit, the CreateTable →
// AddForeignKey ordering, and the discard path.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

// Drive the agent stream by capturing the EventsOn handler (same harness as
// AgentPanel.spec.ts).
let streamHandler: ((p: any) => void) | null = null
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (_n: string, cb: (p: any) => void) => { streamHandler = cb; return () => { streamHandler = null } },
}))

import SchemaBuilderView from './SchemaBuilderView.vue'

const PROPOSAL = {
  tables: [
    // customers already exists — commit must skip it.
    { name: 'customers', columns: [{ name: 'id', type: 'ID' }, { name: 'name', type: 'Text' }] },
    { name: 'technicians', columns: [{ name: 'id', type: 'ID' }, { name: 'name', type: 'Text', not_null: true }, { name: 'hired', type: 'Date' }] },
  ],
  relationships: [
    { child_table: 'service_calls', child_column: 'tech_id', parent_table: 'technicians', parent_column: 'id' },
  ],
  note: 'Add the technician roster',
}

function makeApp(opts: { chat?: (turnId: string) => Promise<any> } = {}) {
  const tables: Record<string, any> = {
    customers: { name: 'customers', columns: [{ name: 'id', type: 'BIGINT', pk: false }, { name: 'name', type: 'VARCHAR', pk: false }] },
    service_calls: { name: 'service_calls', columns: [{ name: 'id', type: 'BIGINT', pk: false }, { name: 'customer_id', type: 'BIGINT', pk: false }, { name: 'tech_id', type: 'BIGINT', pk: false }, { name: 'total', type: 'DECIMAL(12,2)', pk: false }] },
  }
  const app = {
    ListTables: vi.fn(async () => Object.keys(tables)),
    DescribeTable: vi.fn(async (n: string) => tables[n]),
    ListRelationships: vi.fn(async () => [
      { id: 'r1', parent_table: 'customers', parent_column: 'id', child_table: 'service_calls', child_column: 'customer_id', on_delete: 'RESTRICT', enforce_integrity: true },
    ]),
    CreateTable: vi.fn(async (name: string, cols: any[]) => {
      tables[name] = { name, columns: cols.map((c) => ({ name: c.name, type: c.type, pk: c.pk })) }
    }),
    AddForeignKey: vi.fn(async () => 'rel-new'),
    AgentChatStream: vi.fn(opts.chat ?? (async (turnId: string) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'propose_schema', input: JSON.stringify(PROPOSAL) })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'propose_schema', output: 'proposed 2 table(s)' })
      streamHandler!({ turnId, kind: 'final', text: 'Here is a draft — one question: hourly or salaried techs?' })
      return { text: 'Here is a draft.', steps: [] }
    })),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountBuilder() {
  await router.push('/schema')
  await router.isReady()
  const wrapper = mount(SchemaBuilderView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

async function propose(wrapper: any) {
  await wrapper.find('[data-testid="schema-chat-input"]').setValue('track my technicians too')
  await wrapper.find('[data-testid="schema-chat-send"]').trigger('click')
  await flushPromises()
}

describe('SchemaBuilderView (W3)', () => {
  beforeEach(() => {
    streamHandler = null
    vi.clearAllMocks()
  })

  it('renders the live schema as diagram cards with friendly types and an FK arrow', async () => {
    makeApp()
    const wrapper = await mountBuilder()

    const cards = wrapper.findAll('[data-testid="schema-card"]')
    expect(cards).toHaveLength(2)
    expect(wrapper.text()).toContain('customers')
    expect(wrapper.text()).toContain('service_calls')
    // Friendly vocabulary: id → ID, FK column → Link, VARCHAR → Text, DECIMAL → Money.
    const callsCard = cards.find((c: any) => c.attributes('data-table') === 'service_calls')!
    expect(callsCard.text()).toContain('ID')
    expect(callsCard.text()).toContain('Link')
    expect(callsCard.text()).toContain('Money')
    // The committed relationship draws exactly one arrow.
    expect(wrapper.findAll('[data-testid="schema-arrow"]')).toHaveLength(1)
    // No draft yet: build is disabled, no badge.
    expect(wrapper.find('[data-testid="schema-draft-badge"]').exists()).toBe(false)
    expect((wrapper.find('[data-testid="schema-build"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('renders a propose_schema draft as appearing cards with a Draft badge', async () => {
    makeApp()
    const wrapper = await mountBuilder()
    await propose(wrapper)

    // Only technicians is new — customers already exists, so 3 cards total.
    const cards = wrapper.findAll('[data-testid="schema-card"]')
    expect(cards).toHaveLength(3)
    const draftCard = cards.find((c: any) => c.attributes('data-table') === 'technicians')!
    expect(draftCard.classes()).toContain('appearing')
    expect(draftCard.text()).toContain('Date')

    const badge = wrapper.find('[data-testid="schema-draft-badge"]')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('1 table')
    // The proposed FK to the live service_calls table draws a second arrow.
    expect(wrapper.findAll('[data-testid="schema-arrow"]')).toHaveLength(2)
    expect((wrapper.find('[data-testid="schema-build"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('commits the draft: creates only the new tables (mapped types), then the FKs', async () => {
    const app = makeApp()
    const wrapper = await mountBuilder()
    await propose(wrapper)

    await wrapper.find('[data-testid="schema-build"]').trigger('click')
    await flushPromises()

    // customers exists → skipped; only technicians is created.
    expect(app.CreateTable).toHaveBeenCalledTimes(1)
    const [name, cols] = app.CreateTable.mock.calls[0]
    expect(name).toBe('technicians')
    expect(cols).toEqual([
      { name: 'id', type: 'BIGINT', pk: true, not_null: true },
      { name: 'name', type: 'VARCHAR', pk: false, not_null: true },
      { name: 'hired', type: 'DATE', pk: false, not_null: false },
    ])
    // FK declared after the table exists, against the real binding shape.
    expect(app.AddForeignKey).toHaveBeenCalledWith({
      child_table: 'service_calls',
      child_column: 'tech_id',
      parent_table: 'technicians',
      parent_column: 'id',
      on_delete: 'RESTRICT',
      on_update: 'NO ACTION',
    })
    // CreateTable for every pending table happens before any AddForeignKey.
    expect(app.CreateTable.mock.invocationCallOrder[0])
      .toBeLessThan(app.AddForeignKey.mock.invocationCallOrder[0])

    // Draft cleared; the new table is now a committed (solid) card.
    expect(wrapper.find('[data-testid="schema-draft-badge"]').exists()).toBe(false)
    const tech = wrapper.findAll('[data-testid="schema-card"]')
      .find((c: any) => c.attributes('data-table') === 'technicians')!
    expect(tech.classes()).not.toContain('appearing')
  })

  it('does not re-declare a relationship that already exists', async () => {
    const app = makeApp({
      chat: async (turnId: string) => {
        streamHandler!({
          turnId, kind: 'tool', id: 't1', tool: 'propose_schema',
          input: JSON.stringify({
            tables: [{ name: 'technicians', columns: [{ name: 'id', type: 'ID' }] }],
            relationships: [
              // Duplicate of the committed customers ← service_calls FK.
              { child_table: 'service_calls', child_column: 'customer_id', parent_table: 'customers', parent_column: 'id' },
            ],
          }),
        })
        streamHandler!({ turnId, kind: 'final', text: 'ok' })
        return { text: 'ok', steps: [] }
      },
    })
    const wrapper = await mountBuilder()
    await propose(wrapper)
    await wrapper.find('[data-testid="schema-build"]').trigger('click')
    await flushPromises()
    expect(app.AddForeignKey).not.toHaveBeenCalled()
  })

  it('discard drops the draft without touching the database', async () => {
    const app = makeApp()
    const wrapper = await mountBuilder()
    await propose(wrapper)
    expect(wrapper.findAll('[data-testid="schema-card"]')).toHaveLength(3)

    await wrapper.find('[data-testid="schema-discard"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="schema-card"]')).toHaveLength(2)
    expect(wrapper.find('[data-testid="schema-draft-badge"]').exists()).toBe(false)
    expect(app.CreateTable).not.toHaveBeenCalled()
    expect(app.AddForeignKey).not.toHaveBeenCalled()
  })

  it('a newer proposal replaces the previous draft wholesale', async () => {
    let call = 0
    makeApp({
      chat: async (turnId: string) => {
        call += 1
        const input = call === 1
          ? JSON.stringify({ tables: [{ name: 'alpha', columns: [{ name: 'id', type: 'ID' }] }] })
          : JSON.stringify({ tables: [{ name: 'beta', columns: [{ name: 'id', type: 'ID' }] }] })
        streamHandler!({ turnId, kind: 'tool', id: `t${call}`, tool: 'propose_schema', input })
        streamHandler!({ turnId, kind: 'final', text: 'ok' })
        return { text: 'ok', steps: [] }
      },
    })
    const wrapper = await mountBuilder()
    await propose(wrapper)
    expect(wrapper.find('[data-table="alpha"]').exists()).toBe(true)

    await propose(wrapper)
    expect(wrapper.find('[data-table="alpha"]').exists()).toBe(false)
    expect(wrapper.find('[data-table="beta"]').exists()).toBe(true)
  })
})
