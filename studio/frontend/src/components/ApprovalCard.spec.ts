import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ApprovalCard from './ApprovalCard.vue'
import type { PendingApproval } from '../composables/useAgentChat'
import { fileDirty } from '../composables/useFileDirty'

function makeApproval(overrides: Partial<PendingApproval> = {}): PendingApproval {
  return { id: 'a1', summary: 'Delete 3 rows from orders', destructive: true, state: 'pending', ...overrides }
}

describe('ApprovalCard', () => {
  afterEach(() => {
    fileDirty.value = false
  })

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
      ['declined', 'Cancelled, nothing changed.'],
      ['expired', 'No answer in time, nothing changed.'],
      ['cancelled', 'The file was closed or reverted, nothing changed.'],
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

  it('"Save, then do it" is absent when the file is clean', () => {
    fileDirty.value = false
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval() } })
    expect(wrapper.find('[data-testid="approval-save-do-it"]').exists()).toBe(false)
  })

  it('"Save, then do it" is present when dirty and pending, absent once not pending', () => {
    fileDirty.value = true
    const pending = mount(ApprovalCard, { props: { approval: makeApproval({ state: 'pending' }) } })
    expect(pending.find('[data-testid="approval-save-do-it"]').exists()).toBe(true)
    expect(pending.text()).toContain('unsaved table changes')

    const sending = mount(ApprovalCard, { props: { approval: makeApproval({ state: 'sending' }) } })
    expect(sending.find('[data-testid="approval-save-do-it"]').exists()).toBe(false)
  })

  it('clicking "Save, then do it" emits resolve(true, true)', async () => {
    fileDirty.value = true
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval() } })
    await wrapper.find('[data-testid="approval-save-do-it"]').trigger('click')
    expect(wrapper.emitted('resolve')?.[0]).toEqual([true, true])
  })

  it('renders the error line when the approval carries one', () => {
    const wrapper = mount(ApprovalCard, { props: { approval: makeApproval({ error: "Couldn't save: disk full" }) } })
    expect(wrapper.text()).toContain("Couldn't save: disk full")
  })
})
