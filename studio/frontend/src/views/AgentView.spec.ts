// Regression for SandwichLabs/vibe#2: leaving the Agent view mid-turn used to
// drop the conversation (the chat state lived in the view's setup) while the
// Go-side turn kept running. The chat is now a module singleton that keeps
// listening to the agent events while no view is mounted.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const handlers = new Map<string, Set<(p: any) => void>>()
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p: any) => void) => {
    if (!handlers.has(name)) handlers.set(name, new Set())
    handlers.get(name)!.add(cb)
    return () => { handlers.get(name)?.delete(cb) }
  },
}))
function emit(name: string, p: any) { handlers.get(name)?.forEach((cb) => cb(p)) }

import AgentView from './AgentView.vue'
import { useAgentChat } from '../composables/useAgentChat'

let release!: (v: any) => void
let turnId = ''
let binding: ReturnType<typeof vi.fn>

function mountView() {
  return mount(AgentView, { global: { stubs: { Icon: true }, mocks: { $router: { push: vi.fn() } } } })
}

describe('AgentView keeps its turn across unmount (#2)', () => {
  beforeEach(() => {
    handlers.clear()
    ;(window as any).runtime = {}
    useAgentChat().resetAll()
    binding = vi.fn((id: string) => { turnId = id; return new Promise((r) => { release = r }) })
    ;(window as any).go = { main: { App: { AgentChatStream: binding, GetLLMSettings: vi.fn(async () => ({ provider: 'local', endpoint: 'x' })) } } }
  })

  it('shows the transcript and in-flight state after unmount + remount', async () => {
    const first = mountView()
    await first.find('[data-testid="agent-input"]').setValue('build the dashboard')
    await first.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    emit('agent:stream', { turnId, kind: 'delta', text: 'working' })
    emit('agent:stream', { turnId, kind: 'tool', id: 't1', tool: 'create_dashboard', input: '{}' })
    first.unmount()

    // Events keep arriving while no view is mounted.
    emit('agent:stream', { turnId, kind: 'delta', text: ' on it' })

    const second = mountView()
    await flushPromises()
    expect(second.text()).toContain('build the dashboard')
    expect(second.text()).toContain('working on it')
    expect(second.text()).toContain('create_dashboard')
    expect(second.find('[data-testid="agent-send"]').text()).toContain('Sending')
    expect(second.find('[data-testid="agent-send"]').attributes('disabled')).toBeDefined()

    // The turn completes while remounted; a second send is not possible earlier.
    emit('agent:stream', { turnId, kind: 'tool_done', id: 't1', tool: 'create_dashboard', output: '{}' })
    release({ text: 'Done.', steps: [] })
    await flushPromises()
    expect(second.text()).toContain('Done.')
    expect(second.find('[data-testid="agent-send"]').text()).not.toContain('Sending')
    expect(binding).toHaveBeenCalledTimes(1)
  })

  it('a turn that finishes while the view is unmounted is visible on return', async () => {
    const first = mountView()
    await first.find('[data-testid="agent-input"]').setValue('hi')
    await first.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    first.unmount()
    release({ text: 'All finished', steps: [] })
    await flushPromises()
    const second = mountView()
    expect(second.text()).toContain('All finished')
    expect(useAgentChat().sending.value).toBe(false)
  })

  it('shows Stop while sending, in the remounted view too, and cancelling ends the turn', async () => {
    const cancelBinding = vi.fn(async () => {})
    ;(window as any).go.main.App.CancelAgentQuery = cancelBinding
    const first = mountView()
    expect(first.find('[data-testid="agent-cancel"]').exists()).toBe(false)
    await first.find('[data-testid="agent-input"]').setValue('go')
    await first.find('[data-testid="agent-send"]').trigger('click')
    await flushPromises()
    first.unmount()

    const second = mountView()
    await second.find('[data-testid="agent-cancel"]').trigger('click')
    await flushPromises()
    expect(cancelBinding).toHaveBeenCalledWith(turnId)
    emit('agent:stream', { turnId, kind: 'cancelled' })
    release({ text: '', steps: [] })
    await flushPromises()
    expect(second.find('[data-testid="agent-cancelled"]').text()).toBe('Cancelled')
    expect(second.find('[data-testid="agent-cancel"]').exists()).toBe(false)
  })
})
