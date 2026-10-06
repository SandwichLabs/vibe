// #79: AgentSandboxIntro must update live when the file's agent policy
// changes — via SetAgentConfig's "agent:config" event (no re-fetch needed)
// and via "workspace:opened" (a different workspace, needs a fresh GetAgentConfig).
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const handlers: Record<string, ((payload?: any) => void) | undefined> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (payload?: any) => void) => {
    handlers[name] = cb
    return () => { delete handlers[name] }
  },
  EventsOff: () => {},
}))

import AgentSandboxIntro from './AgentSandboxIntro.vue'

function makeApp(sandbox: string, askBeforeWrites: boolean) {
  const app = { GetAgentConfig: vi.fn(async () => ({ sandbox, askBeforeWrites })) }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}

describe('AgentSandboxIntro', () => {
  it('fetches the current policy on mount', async () => {
    makeApp('read-write', true)
    const wrapper = mount(AgentSandboxIntro)
    await flushPromises()
    expect(wrapper.text()).toContain('It asks you before each change.')
  })

  it('updates in place when "agent:config" fires, without a re-fetch', async () => {
    const app = makeApp('read-write', true)
    const wrapper = mount(AgentSandboxIntro)
    await flushPromises()
    app.GetAgentConfig.mockClear()

    handlers['agent:config']?.({ sandbox: 'read-only', askBeforeWrites: true })
    await flushPromises()

    expect(wrapper.text()).toContain("can't change anything")
    expect(app.GetAgentConfig).not.toHaveBeenCalled()
  })

  it('re-fetches when "workspace:opened" fires', async () => {
    const app = makeApp('read-write', true)
    const wrapper = mount(AgentSandboxIntro)
    await flushPromises()

    app.GetAgentConfig.mockResolvedValueOnce({ sandbox: 'full', askBeforeWrites: false })
    handlers['workspace:opened']?.()
    await flushPromises()

    expect(wrapper.text()).toContain('It makes changes without asking.')
  })
})
