// AiCellDialog.vue component tests (#66 part B review fixes, findings 5 and
// 9): the refine/answer box must never start pre-filled with the previous
// request, the prompt textarea must be focused on open, and the empty-cell
// "generate it with AI" entry point must show "Generate with AI" even
// though it's mechanically an edit-mode dialog.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

let taskHandler: ((p: any) => void) | null = null
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (_name: string, cb: (p: any) => void) => {
    taskHandler = cb
    return () => { taskHandler = null }
  },
}))

import AiCellDialog from './AiCellDialog.vue'
import { useAiCellDialog } from '../composables/useAiCellDialog'

function setBinding(impl: (req: any) => Promise<any>) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: { AgentTask: vi.fn(impl), CancelAgentTask: vi.fn() } } }
}

function makeHost(cells: any[] = []) {
  return {
    cells: { value: cells },
    insertCell: vi.fn((ec: any) => cells.push({ id: 'new', type: ec.type })),
    replaceCell: vi.fn(),
    snapshotCell: vi.fn((id: string) => {
      const c = cells.find((c) => c.id === id)
      return c ? { source: c.source, label: c.label, view: c.view } : null
    }),
    getTables: vi.fn(async () => []),
    getNotebookSummary: vi.fn(() => []),
  }
}

describe('AiCellDialog.vue', () => {
  beforeEach(() => { taskHandler = null })

  it('focuses the prompt textarea on open', async () => {
    const dialog = useAiCellDialog(makeHost())
    const wrapper = mount(AiCellDialog, { props: { dialog }, attachTo: document.body })
    dialog.openCreate()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick() // watcher's own nextTick
    const textarea = wrapper.find('[data-testid="ai-cell-prompt"]').element as HTMLTextAreaElement
    expect(document.activeElement).toBe(textarea)
    wrapper.unmount()
  })

  it('the refine box starts EMPTY, not pre-filled with the original request (finding 5)', async () => {
    setBinding(async () => ({ outcome: 'cell', cell: { type: 'sql', sql: 'SELECT 1' } }))
    const dialog = useAiCellDialog(makeHost())
    const wrapper = mount(AiCellDialog, { props: { dialog } })
    dialog.openCreate()
    await wrapper.vm.$nextTick()

    await wrapper.find('[data-testid="ai-cell-prompt"]').setValue('revenue by region')
    await wrapper.find('[data-testid="ai-cell-generate"]').trigger('click')
    await flushPromises()

    expect(dialog.state.dialogState).toBe('proposal')
    const refineInput = wrapper.find('[data-testid="ai-cell-refine-input"]').element as HTMLInputElement
    expect(refineInput.value).toBe('')
  })

  it('the answer box also starts empty on a needs_info outcome', async () => {
    setBinding(async () => ({ outcome: 'needs_info', questions: ['which table?'] }))
    const dialog = useAiCellDialog(makeHost())
    const wrapper = mount(AiCellDialog, { props: { dialog } })
    dialog.openCreate()
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-testid="ai-cell-prompt"]').setValue('churn by segment')
    await wrapper.find('[data-testid="ai-cell-generate"]').trigger('click')
    await flushPromises()

    const answerInput = wrapper.find('[data-testid="ai-cell-answer-input"]').element as HTMLInputElement
    expect(answerInput.value).toBe('')
  })

  it('titles "Generate with AI" when opened via the empty-cell generate link, even though it is edit mode', async () => {
    const dialog = useAiCellDialog(makeHost([{ id: 'c1', type: 'sql', source: '' }]))
    const wrapper = mount(AiCellDialog, { props: { dialog } })
    dialog.openEdit({ id: 'c1', type: 'sql' }, 'Generate with AI')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.card-title').text()).toContain('Generate with AI')
  })

  it('titles "Edit with AI" for an ordinary edit (no override)', async () => {
    const dialog = useAiCellDialog(makeHost([{ id: 'c1', type: 'sql', source: 'SELECT 1' }]))
    const wrapper = mount(AiCellDialog, { props: { dialog } })
    dialog.openEdit({ id: 'c1', type: 'sql' })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.card-title').text()).toContain('Edit with AI')
  })

  it('shows "Insert as new cell" and an explanation when the proposal type mismatches the target (C2)', async () => {
    setBinding(async () => ({ outcome: 'cell', cell: { type: 'sql', sql: 'SELECT 1' } }))
    const dialog = useAiCellDialog(makeHost([{ id: 'c1', type: 'markdown', source: '# notes' }]))
    const wrapper = mount(AiCellDialog, { props: { dialog } })
    dialog.openEdit({ id: 'c1', type: 'markdown' })
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-testid="ai-cell-prompt"]').setValue('turn this into a query')
    await wrapper.find('[data-testid="ai-cell-generate"]').trigger('click')
    await flushPromises()

    const applyBtn = wrapper.find('[data-testid="ai-cell-apply"]')
    expect(applyBtn.text()).toBe('Insert as new cell')
    expect(wrapper.find('[data-testid="ai-cell-apply-note"]').text()).toContain('different cell type')
  })

  it('shows the ordinary "Apply" label when types match and nothing drifted', async () => {
    setBinding(async () => ({ outcome: 'cell', cell: { type: 'sql', sql: 'SELECT 2' } }))
    const dialog = useAiCellDialog(makeHost([{ id: 'c1', type: 'sql', source: 'SELECT 1' }]))
    const wrapper = mount(AiCellDialog, { props: { dialog } })
    dialog.openEdit({ id: 'c1', type: 'sql' })
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-testid="ai-cell-prompt"]').setValue('x')
    await wrapper.find('[data-testid="ai-cell-generate"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="ai-cell-apply"]').text()).toBe('Apply')
    expect(wrapper.find('[data-testid="ai-cell-apply-note"]').exists()).toBe(false)
  })
})

async function flushPromises() {
  await new Promise((r) => setTimeout(r, 0))
  await new Promise((r) => setTimeout(r, 0))
}

describe('AiCellDialog.vue keyboard and prefill', () => {
  it('starts the prompt with the prefill, and the next plain open starts empty', async () => {
    const dialog = useAiCellDialog(makeHost())
    const wrapper = mount(AiCellDialog, { props: { dialog }, attachTo: document.body })
    dialog.openCreate('weekly sales')
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    const box = () => wrapper.find('[data-testid="ai-cell-prompt"]').element as HTMLTextAreaElement
    expect(box().value).toBe('weekly sales')
    dialog.close()
    dialog.openCreate()
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    expect(box().value).toBe('')
    wrapper.unmount()
  })

  it('is a modal: Tab wraps inside it, and Escape closes it and returns focus to the opener', async () => {
    const opener = document.createElement('button')
    document.body.appendChild(opener)
    opener.focus()
    const dialog = useAiCellDialog(makeHost())
    const wrapper = mount(AiCellDialog, { props: { dialog }, attachTo: document.body })
    expect(wrapper.find('[data-testid="ai-cell-dialog"]').exists()).toBe(false)
    dialog.openCreate()
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="ai-cell-dialog"]').attributes('aria-modal')).toBe('true')
    // Generate is disabled while empty, so the last stop is the prompt box; Tab wraps to the close button.
    const prompt = wrapper.get('[data-testid="ai-cell-prompt"]').element as HTMLElement
    expect(document.activeElement).toBe(prompt)
    const tab = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(tab)
    expect(tab.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(wrapper.get('[data-testid="ai-cell-close"]').element)
    const esc = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    document.dispatchEvent(esc)
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    expect(esc.defaultPrevented).toBe(true)
    expect(dialog.state.dialogState).toBe('closed')
    expect(document.activeElement).toBe(opener)
    wrapper.unmount()
    opener.remove()
  })
})

describe('AiCellDialog.vue fallback focus', () => {
  it('restores focus to the fallback when the opener was removed meanwhile', async () => {
    const opener = document.createElement('button')
    const fallback = document.createElement('button')
    document.body.append(opener, fallback)
    opener.focus()
    const dialog = useAiCellDialog(makeHost())
    const wrapper = mount(AiCellDialog, { props: { dialog, fallbackFocus: () => fallback }, attachTo: document.body })
    dialog.openCreate()
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    opener.remove()
    dialog.close()
    await wrapper.vm.$nextTick(); await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(fallback)
    wrapper.unmount()
    fallback.remove()
  })
})
