// Component tests for the AgentPanel slide-in (W0/W0.5).
//
// The panel composes useAgentChat; these assert the panel's DOM rendering of a
// streamed turn: the agent answer rendered as markdown, tool-use chips that end
// "done", the pin affordance, and the error path. Stream-event/multi-turn logic
// is covered in useAgentChat.spec.ts.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

// Drive the stream by capturing the EventsOn handler.
let streamHandler: ((p: any) => void) | null = null
let closedHandler: (() => void) | null = null
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (n: string, cb: (p: any) => void) => {
    if (n === 'workspace:closed') { closedHandler = cb as () => void; return () => {} }
    streamHandler = cb
    return () => { streamHandler = null }
  },
}))

import AgentPanel from './AgentPanel.vue'
import panelSrc from './AgentPanel.vue?raw'
import { useAgentChat } from '../composables/useAgentChat'

function setBinding(impl: (turnId: string, msg: string, pinned: string, history: any[]) => Promise<any>) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: { AgentChatStream: vi.fn(impl) } } }
}

async function mountPanel() {
  await router.push('/tables')
  await router.isReady()
  return mount(AgentPanel, { props: { open: true }, global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AgentPanel (W0.5)', () => {
  beforeEach(() => { streamHandler = null; useAgentChat().resetAll() })

  it('renders the assistant answer as markdown and a done tool chip', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'query', input: 'SELECT 1' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'query', output: '1 row' })
      streamHandler!({ turnId, kind: 'final', text: 'You have **2** customers.' })
      return { text: 'You have **2** customers.', steps: [] }
    })
    const wrapper = await mountPanel()

    await wrapper.find('[data-testid="agent-input"]').setValue('how many customers?')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()

    // Markdown rendered (bold), not literal asterisks.
    expect(wrapper.find('.agent-md').html()).toContain('<strong>2</strong>')
    const chips = wrapper.findAll('[data-testid="agent-tool-chip"]')
    expect(chips).toHaveLength(1)
    expect(wrapper.find('[data-testid="chip-done"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('query')
  })

  it('renders a graceful error when the stream fails', async () => {
    setBinding(async () => { throw new Error('connection refused') })
    const wrapper = await mountPanel()
    await wrapper.find('[data-testid="agent-input"]').setValue('hi')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Assistant error')
    expect(wrapper.text()).toContain('connection refused')
  })

  it('renders a denied/errored tool call as an error chip, not a done checkmark', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'create_table', input: '{"name":"widgets"}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'create_table', output: 'method "create_table" (category "schema") is not allowed in read-write mode', isError: true })
      streamHandler!({ turnId, kind: 'final', text: 'I could not create that table.' })
      return { text: 'I could not create that table.', steps: [] }
    })
    const wrapper = await mountPanel()

    await wrapper.find('[data-testid="agent-input"]').setValue('create a widgets table')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="chip-done"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="chip-running"]').exists()).toBe(false)
    const errorChip = wrapper.find('[data-testid="chip-error"]')
    expect(errorChip.exists()).toBe(true)
    const chip = wrapper.find('[data-testid="agent-tool-chip"]')
    expect(chip.classes()).toContain('tool-chip-error')
  })

  // --- #91 P-F12/M-F21: tool chip expands to full input + Copy ---
  it('opening a tool chip shows the full SQL and a Copy SQL button', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'run_query', input: '{"sql":"SELECT * FROM customers WHERE region = \'West\'"}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'run_query', output: '3 rows' })
      streamHandler!({ turnId, kind: 'final', text: 'Done.' })
      return { text: 'Done.', steps: [] }
    })
    const wrapper = await mountPanel()

    await wrapper.find('[data-testid="agent-input"]').setValue('west region customers')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()

    const details = wrapper.find('.tool-step')
    expect(details.exists()).toBe(true);
    (details.element as HTMLDetailsElement).open = true
    await flushPromises()

    const body = wrapper.find('[data-testid="agent-tool-chip-body"]')
    expect(body.text()).toContain("SELECT * FROM customers WHERE region = 'West'")
    expect(body.text()).toContain('Copy SQL')
  })

  it('pins the current screen as context', async () => {
    setBinding(async (turnId) => { streamHandler!({ turnId, kind: 'final', text: 'ok' }); return { text: 'ok', steps: [] } })
    const wrapper = await mountPanel()
    expect(wrapper.find('[data-testid="agent-pin"]').exists()).toBe(false)
    await wrapper.find('.agent-pinbtn').trigger('click')
    await flushPromises()
    const pin = wrapper.find('[data-testid="agent-pin"]')
    expect(pin.exists()).toBe(true)
    expect(pin.text()).toContain('tables')
  })
})

describe('AgentPanel prefill (library L7)', () => {
  beforeEach(() => { streamHandler = null; useAgentChat().resetAll() })
  const input = (w: any) => w.find('[data-testid="agent-input"]').element as HTMLTextAreaElement

  it('fills an empty box with the draft and sends nothing', async () => {
    setBinding(async () => ({ text: '', steps: [] }))
    const w = await mountPanel()
    await w.setProps({ prefill: { text: 'Build a live view: x', n: 1 } })
    await flushPromises()
    expect(input(w).value).toBe('Build a live view: x')
    expect((window as any).go.main.App.AgentChatStream).not.toHaveBeenCalled()
  })

  it('never overwrites text the person typed, but replaces an unchanged earlier prefill', async () => {
    setBinding(async () => ({ text: '', steps: [] }))
    const w = await mountPanel()
    await w.setProps({ prefill: { text: 'first', n: 1 } })
    await w.setProps({ prefill: { text: 'second', n: 2 } })
    expect(input(w).value).toBe('second')
    await w.find('[data-testid="agent-input"]').setValue('my own question')
    await w.setProps({ prefill: { text: 'third', n: 3 } })
    expect(input(w).value).toBe('my own question')
    // The refused draft is kept behind a notice and replaces the box only on click.
    expect(w.get('[data-testid="agent-pending-draft"]').text()).toContain('waiting')
    await w.get('[data-testid="agent-use-draft"]').trigger('click')
    expect(input(w).value).toBe('third')
    expect(w.find('[data-testid="agent-pending-draft"]').exists()).toBe(false)
  })

  it('keeps a draft refused while sending behind the notice; a turn ending never fills or focuses the box', async () => {
    let release: (v: any) => void = () => {}
    setBinding(() => new Promise((r) => { release = r }))
    const w = await mountPanel()
    await w.find('[data-testid="agent-input"]').setValue('go')
    await w.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    await w.setProps({ prefill: { text: 'late draft', n: 1 } })
    expect(input(w).value).toBe('')
    expect(w.find('[data-testid="agent-pending-draft"]').exists()).toBe(true)
    const other = document.createElement('input')
    document.body.appendChild(other)
    other.focus()
    release({ text: 'done', steps: [] })
    await flushPromises()
    expect(input(w).value).toBe('')
    expect(document.activeElement).toBe(other)
    expect(w.find('[data-testid="agent-pending-draft"]').exists()).toBe(true)
    expect((window as any).go.main.App.AgentChatStream).toHaveBeenCalledTimes(1)
    other.remove()
  })

  it('does not land a pending draft over text typed in the meantime', async () => {
    let release: (v: any) => void = () => {}
    setBinding(() => new Promise((r) => { release = r }))
    const w = await mountPanel()
    await w.find('[data-testid="agent-input"]').setValue('go')
    await w.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    await w.setProps({ prefill: { text: 'late draft', n: 1 } })
    await w.find('[data-testid="agent-input"]').setValue('typed while waiting')
    release({ text: 'done', steps: [] })
    await flushPromises()
    expect(input(w).value).toBe('typed while waiting')
    expect(w.find('[data-testid="agent-pending-draft"]').exists()).toBe(true)
  })

  it('puts the caret at the start after a prefill', async () => {
    setBinding(async () => ({ text: '', steps: [] }))
    const w = await mountPanel()
    await w.setProps({ prefill: { text: 'a long drafted request', n: 1 } })
    await flushPromises()
    expect(input(w).selectionStart).toBe(0)
    expect(input(w).selectionEnd).toBe(0)
  })

  it('drops the draft and the pending notice when the workspace closes', async () => {
    ;(window as any).runtime = {}
    setBinding(async () => ({ text: '', steps: [] }))
    const w = await mountPanel()
    delete (window as any).runtime
    await w.find('[data-testid="agent-input"]').setValue('mine')
    await w.setProps({ prefill: { text: 'x', n: 1 } })
    expect(w.find('[data-testid="agent-pending-draft"]').exists()).toBe(true)
    closedHandler!()
    await flushPromises()
    expect(input(w).value).toBe('')
    expect(w.find('[data-testid="agent-pending-draft"]').exists()).toBe(false)
  })

  it('Enter does not send on key repeat or while composing', async () => {
    setBinding(async () => ({ text: 'ok', steps: [] }))
    const w = await mountPanel()
    await w.find('[data-testid="agent-input"]').setValue('hello')
    await w.find('[data-testid="agent-input"]').trigger('keydown', { key: 'Enter', repeat: true })
    await w.find('[data-testid="agent-input"]').trigger('keydown', { key: 'Enter', isComposing: true })
    expect((window as any).go.main.App.AgentChatStream).not.toHaveBeenCalled()
    await w.find('[data-testid="agent-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect((window as any).go.main.App.AgentChatStream).toHaveBeenCalledTimes(1)
  })

  it('submit is the only caller of send', () => {
    const calls = panelSrc.match(/\bsend\(/g) ?? []
    expect(calls).toHaveLength(1)
    const submit = panelSrc.slice(panelSrc.indexOf('async function submit()'))
    expect(submit.slice(0, submit.indexOf('\n}\n'))).toContain('await send(text)')
  })
})
