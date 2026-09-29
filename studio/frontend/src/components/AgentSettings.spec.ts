import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AgentSettings from './AgentSettings.vue'

function makeApp(cfg: AgentConfig) {
  const app = {
    // A fresh copy each call: AgentSettings mutates cfg.value's fields
    // in place (setMode/setAsk) before persist() runs, so returning the
    // SAME object reference here would let that optimistic mutation leak
    // back into "what the file actually has stored" on the next load().
    GetAgentConfig: vi.fn(async () => ({ ...cfg })),
    SetAgentConfig: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

describe('AgentSettings', () => {
  it('loads and shows the current policy', async () => {
    makeApp({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'read-write', askBeforeWrites: true })
    const wrapper = mount(AgentSettings)
    await flushPromises()

    expect((wrapper.find('[data-testid="agent-sandbox-read-write"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.find('[data-testid="agent-ask-before-writes"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.text()).toContain('local')
    expect(wrapper.text()).toContain('llama')
  })

  it('a radio change calls SetAgentConfig with both fields', async () => {
    const app = makeApp({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'read-write', askBeforeWrites: true })
    const wrapper = mount(AgentSettings)
    await flushPromises()

    await wrapper.find('[data-testid="agent-sandbox-full"]').setValue(true)
    await flushPromises()

    expect(app.SetAgentConfig).toHaveBeenCalledWith({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'full', askBeforeWrites: true })
  })

  it('toggling the ask switch calls SetAgentConfig with the flipped value', async () => {
    const app = makeApp({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'full', askBeforeWrites: true })
    const wrapper = mount(AgentSettings)
    await flushPromises()

    const checkbox = wrapper.find('[data-testid="agent-ask-before-writes"]')
    await checkbox.setValue(false)
    await flushPromises()

    expect(app.SetAgentConfig).toHaveBeenCalledWith({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'full', askBeforeWrites: false })
  })

  it('reverts the choice when SetAgentConfig fails (round 2 review finding 10)', async () => {
    const app = makeApp({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'read-write', askBeforeWrites: true })
    app.SetAgentConfig = vi.fn(async () => { throw new Error('nope') })
    const wrapper = mount(AgentSettings)
    await flushPromises()

    await wrapper.find('[data-testid="agent-sandbox-full"]').setValue(true)
    await flushPromises()
    // persist()'s catch branch awaits a second async call (load(), which
    // itself awaits GetAgentConfig) — give that chained promise its own
    // microtask turn to resolve.
    await flushPromises()

    expect(wrapper.text()).toContain('Error')
    // GetAgentConfig is re-fetched on error, and it still reports the
    // original stored value — the radio must reflect that, not the rejected
    // click.
    expect((wrapper.find('[data-testid="agent-sandbox-read-write"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.find('[data-testid="agent-sandbox-full"]').element as HTMLInputElement).checked).toBe(false)
  })
})
