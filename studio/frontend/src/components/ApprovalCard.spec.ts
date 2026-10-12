import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ApprovalCard from './ApprovalCard.vue'
import type { PendingApproval } from '../composables/useAgentChat'

function makeApproval(overrides: Partial<PendingApproval> = {}): PendingApproval {
  return { id: 'a1', summary: 'Delete 3 rows from orders', destructive: true, state: 'pending', ...overrides }
}

describe('ApprovalCard', () => {
  it('renders the summary and the SQL detail for run_query', () => {
    const wrapper = mount(ApprovalCard, {
      props: { approval: makeApproval({ summary: 'Run SQL that changes data', sql: "DELETE FROM orders WHERE status = 'Void'" }) },
    })
    expect(wrapper.text()).toContain('Run SQL that changes data')
    expect(wrapper.text()).toContain("DELETE FROM orders WHERE status = 'Void'")
  })

  it('does not render a <pre> block for where alone — it is already folded into the summary', () => {
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval({ where: "status = 'Void'" }) } })
    expect(wrapper.find('.approval-card__detail').exists()).toBe(false)
  })

  it('emits resolve(true) on "Do it" and resolve(false) on "Cancel"', async () => {
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval() } })
    await wrapper.find('[data-testid="approval-do-it"]').trigger('click')
    expect(wrapper.emitted('resolve')?.[0]).toEqual([true])

    const wrapper2 = mount(ApprovalCard, { props: { approval: makeApproval() } })
    await wrapper2.find('[data-testid="approval-cancel"]').trigger('click')
    expect(wrapper2.emitted('resolve')?.[0]).toEqual([false])
  })

  it('applies the destructive button class when destructive', () => {
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval({ destructive: true }) } })
    expect(wrapper.find('[data-testid="approval-do-it"]').classes()).toContain('btn-destructive')

    const wrapper2 = mount(ApprovalCard, { props: { approval: makeApproval({ destructive: false }) } })
    expect(wrapper2.find('[data-testid="approval-do-it"]').classes()).toContain('btn-primary')
  })

  it('disables both buttons once the state leaves pending', () => {
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval({ state: 'sending' }) } })
    expect((wrapper.find('[data-testid="approval-do-it"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((wrapper.find('[data-testid="approval-cancel"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows the outcome line for each terminal state', () => {
    for (const [state, text] of [
      ['declined', 'Declined, nothing changed.'],
      ['expired', 'No answer in time, nothing changed.'],
      ['cancelled', 'Stopped before it ran, nothing changed.'],
      ['gone', 'No longer waiting, nothing changed.'],
      ['approved', 'Approved, running…'],
    ] as const) {
      const wrapper = mount(ApprovalCard, { props: { approval: makeApproval({ state }) } })
      expect(wrapper.text()).toContain(text)
    }
  })

  it('switches the approved line to "Approved." once the matching step is done', () => {
    const running = mount(ApprovalCard, { props: { approval: makeApproval({ state: 'approved' }), done: false } })
    expect(running.text()).toContain('Approved, running…')

    const done = mount(ApprovalCard, { props: { approval: makeApproval({ state: 'approved' }), done: true } })
    expect(done.text()).toContain('Approved.')
    expect(done.text()).not.toContain('running…')
  })
})

describe('ApprovalCard names the connection (#117, SPEC 10)', () => {
  const remote = (over: Partial<PendingApproval> = {}) => makeApproval({
    summary: 'Update 1 row in warehouse', sql: "UPDATE warehouse.public.orders SET status = 'shipped' WHERE id = 10482",
    destructive: false, connections: ['warehouse'], ...over,
  })

  it('says where the change goes in the header, a chip and the button, and that Vibe cannot undo it', () => {
    const w = mount(ApprovalCard, { props: { approval: remote() }, attachTo: document.body })
    expect(w.find('.approval-card__title').text()).toContain('Allow this change to warehouse?')
    expect(w.find('[data-testid="approval-connection"]').text()).toContain('warehouse')
    expect(w.find('[data-testid="approval-connection"]').text()).toContain('not your local data')
    expect(w.find('[data-testid="approval-do-it"]').text()).toBe('Allow change to warehouse')
    expect(w.find('[data-testid="approval-undo-note"]').text()).toContain("so Vibe can't undo it for you")
  })

  it('a destructive statement says so, in the title, the button and the warning', () => {
    const w = mount(ApprovalCard, { props: { approval: remote({ destructive: true, rows: 1204, summary: 'Delete rows from warehouse', sql: 'DELETE FROM warehouse.public.events WHERE ts < 2020' }) } })
    expect(w.find('.approval-card__title').text()).toContain('Allow delete in warehouse?')
    expect(w.find('[data-testid="approval-do-it"]').text()).toBe('Allow delete in warehouse')
    expect(w.find('[data-testid="approval-do-it"]').classes()).toContain('btn-destructive')
    expect(w.find('[data-testid="approval-undo-note"]').text()).toContain('It affects about 1,204 rows.')
  })

  it('says nothing about a row count when the policy gave none, and lists every target connection', () => {
    const w = mount(ApprovalCard, { props: { approval: remote({ destructive: true, rows: -1, connections: ['warehouse', 'pg'], summary: 'Delete rows', sql: 'DELETE FROM x' }) } })
    expect(w.find('[data-testid="approval-undo-note"]').text()).not.toMatch(/affects about|number of rows/)
    expect(w.find('.approval-card__title').text()).toContain('warehouse, pg')
    expect(w.find('[data-testid="approval-connection"]').text()).toContain('warehouse, pg')
  })

  it('a local change keeps the plain card', () => {
    const w = mount(ApprovalCard, { props: { approval: makeApproval({ connections: undefined }) } })
    expect(w.find('.approval-card__title').text()).toContain('Allow this change?')
    expect(w.find('[data-testid="approval-connection"]').exists()).toBe(false)
    expect(w.find('[data-testid="approval-do-it"]').text()).toBe('Do it')
  })

  it('focus starts on Cancel, never on Allow, and Escape cancels', async () => {
    const w = mount(ApprovalCard, { props: { approval: remote() }, attachTo: document.body })
    await new Promise((r) => setTimeout(r, 0))
    expect(document.activeElement).toBe(w.find('[data-testid="approval-cancel"]').element)
    await w.find('[data-testid="approval-card"]').trigger('keydown', { key: 'Escape' })
    expect(w.emitted('resolve')?.[0]).toEqual([false])
  })
})
