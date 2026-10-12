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
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (_n: string, cb: (p: any) => void) => { streamHandler = cb; return () => { streamHandler = null } },
}))

import AgentPanel from './AgentPanel.vue'
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

  it('renders the agent answer as markdown and a done tool chip', async () => {
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
    expect(wrapper.text()).toContain('Agent error')
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
