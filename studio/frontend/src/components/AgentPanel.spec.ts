// Component tests for the AgentPanel slide-in (W0).
//
// The panel drives the AgentChat binding and renders: the user message, the
// agent's answer, the tool-use chips for each tool the agent invoked, a pin-
// context affordance, and a graceful error when the LLM endpoint is unreachable.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import AgentPanel from './AgentPanel.vue'

function setApp(agentChat: any) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: { AgentChat: agentChat } } }
}

async function mountPanel() {
  await router.push('/tables')
  await router.isReady()
  return mount(AgentPanel, {
    props: { open: true },
    global: { plugins: [router], stubs: { Icon: true } },
  })
}

describe('AgentPanel (W0)', () => {
  beforeEach(() => { vi.useRealTimers() })

  it('sends a message and renders the agent answer + tool chips', async () => {
    const AgentChat = vi.fn(async () => ({
      text: 'You have 2 customers in Denver and Vail.',
      steps: [
        { tool: 'list_tables', input: '', output: 'customers' },
        { tool: 'query', input: 'SELECT count(*) FROM customers', output: '2 row(s)' },
      ],
    }))
    setApp(AgentChat)
    const wrapper = await mountPanel()

    await wrapper.find('[data-testid="agent-input"]').setValue('how many customers?')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()

    expect(AgentChat).toHaveBeenCalledWith('how many customers?', '')
    expect(wrapper.text()).toContain('You have 2 customers')
    const chips = wrapper.findAll('[data-testid="agent-tool-chip"]')
    expect(chips.length, 'one chip per tool step').toBe(2)
    expect(wrapper.text()).toContain('list_tables')
    expect(wrapper.text()).toContain('query')
  })

  it('renders a graceful error when the LLM endpoint is unreachable', async () => {
    const AgentChat = vi.fn(async () => { throw new Error('connection refused') })
    setApp(AgentChat)
    const wrapper = await mountPanel()

    await wrapper.find('[data-testid="agent-input"]').setValue('hi')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Agent error')
    expect(wrapper.text()).toContain('connection refused')
  })

  it('pins the current screen as context and sends it with the message', async () => {
    const AgentChat = vi.fn(async () => ({ text: 'ok', steps: [] }))
    setApp(AgentChat)
    const wrapper = await mountPanel()

    // No pin chip initially.
    expect(wrapper.find('[data-testid="agent-pin"]').exists()).toBe(false)

    // Click the pin button, then a pin chip appears referencing the route.
    await wrapper.find('.agent-pinbtn').trigger('click')
    await flushPromises()
    const pin = wrapper.find('[data-testid="agent-pin"]')
    expect(pin.exists()).toBe(true)
    expect(pin.text()).toContain('tables')

    await wrapper.find('[data-testid="agent-input"]').setValue('what is here?')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()

    const [, pinnedArg] = AgentChat.mock.calls[0]
    expect(pinnedArg).toContain('tables')
  })

  it('does not send empty messages', async () => {
    const AgentChat = vi.fn(async () => ({ text: 'ok', steps: [] }))
    setApp(AgentChat)
    const wrapper = await mountPanel()

    await wrapper.find('[data-testid="agent-input"]').setValue('   ')
    await wrapper.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    expect(AgentChat).not.toHaveBeenCalled()
  })
})
