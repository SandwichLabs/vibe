// The agent API key field is write-only (#114, SPEC 09): set / replace / clear
// and a one-time migration banner. No binding returns the key, and the field
// never shows one.
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../composables/useConfirm', () => ({ askConfirm: vi.fn(async () => true) }))

import AgentModelSettings from './AgentModelSettings.vue'
import { askConfirm } from '../composables/useConfirm'

const SECRET = 'sk-ant-NEEDLE-should-never-render'

function status(over: Partial<LLMKeyStatus> = {}): LLMKeyStatus {
  return { provider: 'anthropic', source: 'none', set: false, fileKey: false, canMigrate: false, keychainAvailable: true, ...over }
}

function setup(first: LLMKeyStatus, next?: LLMKeyStatus) {
  let cur = first
  const app = {
    GetAgentConfig: vi.fn(async () => ({ provider: 'anthropic', model: 'm', endpoint: 'http://x', sandbox: 'read-write', askBeforeWrites: true })),
    GetLLMKeyStatus: vi.fn(async () => cur),
    SetLLMKey: vi.fn(async () => { if (next) cur = next }),
    ClearLLMKey: vi.fn(async () => { cur = status() }),
    MigrateLLMKeyToKeychain: vi.fn(async () => { if (next) cur = next }),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountIt() {
  const w = mount(AgentModelSettings, { global: { stubs: { Icon: true } } })
  await flushPromises()
  return w
}

describe('AgentModelSettings API key', () => {
  it('with no key offers a password field and saves to the keychain', async () => {
    const app = setup(status(), status({ source: 'keychain', set: true }))
    const w = await mountIt()
    const input = w.get('[data-testid="llm-key-input"]')
    expect(input.attributes('type')).toBe('password')
    await input.setValue(SECRET)
    await w.get('[data-testid="llm-key-save"]').trigger('submit')
    await flushPromises()
    expect(app.SetLLMKey).toHaveBeenCalledWith('', SECRET)
    // After saving: dots and Replace / Clear, field gone, secret nowhere in the DOM.
    expect(w.find('[data-testid="llm-key-saved"]').exists()).toBe(true)
    expect(w.find('[data-testid="llm-key-input"]').exists()).toBe(false)
    expect(w.html()).not.toContain('NEEDLE')
  })

  it('keychain state is write-only: Replace reveals an empty field, Clear confirms first', async () => {
    const app = setup(status({ source: 'keychain', set: true }))
    const w = await mountIt()
    expect(w.text()).toContain('In your keychain')
    expect(w.text()).toContain('Replace')
    await w.get('[data-testid="llm-key-replace"]').trigger('click')
    expect((w.get('[data-testid="llm-key-input"]').element as HTMLInputElement).value).toBe('')
    await w.get('form').trigger('submit') // empty: nothing saved
    expect(app.SetLLMKey).not.toHaveBeenCalled()

    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(w.find('[data-testid="llm-key-saved"]').exists()).toBe(true)
  })

  it('Clear asks, then clears', async () => {
    const app = setup(status({ source: 'keychain', set: true }))
    const w = await mountIt()
    await w.get('[data-testid="llm-key-clear"]').trigger('click')
    await flushPromises()
    expect(askConfirm).toHaveBeenCalled()
    expect(app.ClearLLMKey).toHaveBeenCalledWith('')
    expect(w.find('[data-testid="llm-key-input"]').exists()).toBe(true)
  })

  it('shows the migration banner for a plaintext key and moves it', async () => {
    const app = setup(
      status({ source: 'file', set: true, fileKey: true, canMigrate: true }),
      status({ source: 'keychain', set: true }),
    )
    const w = await mountIt()
    expect(w.find('[data-testid="llm-key-migrate"]').exists()).toBe(true)
    expect(w.text()).toContain('plaintext')
    await w.get('[data-testid="llm-key-migrate-btn"]').trigger('click')
    await flushPromises()
    expect(app.MigrateLLMKeyToKeychain).toHaveBeenCalledWith(false)
    expect(w.find('[data-testid="llm-key-migrate"]').exists()).toBe(false)
    expect(w.text()).toContain('Moved to your keychain')
  })

  it('asks before replacing a key already in the keychain', async () => {
    const app = setup(status({ source: 'env', set: true, fileKey: true, canMigrate: true, keychainHasKey: true }), status({ source: 'keychain', set: true }))
    const w = await mountIt()
    ;(askConfirm as any).mockClear()
    ;(askConfirm as any).mockResolvedValueOnce(false)
    await w.get('[data-testid="llm-key-migrate-btn"]').trigger('click')
    await flushPromises()
    expect(askConfirm).toHaveBeenCalled()
    expect(app.MigrateLLMKeyToKeychain).not.toHaveBeenCalled()
    await w.get('[data-testid="llm-key-migrate-btn"]').trigger('click')
    await flushPromises()
    expect(app.MigrateLLMKeyToKeychain).toHaveBeenCalledWith(true)
  })

  it('no keychain: says so, offers the env alternative and no field', async () => {
    setup(status({ keychainAvailable: false }))
    const w = await mountIt()
    expect(w.find('[data-testid="llm-key-nokeychain"]').exists()).toBe(true)
    expect(w.text()).toContain('VIBE_LLM_API_KEY')
    expect(w.find('[data-testid="llm-key-input"]').exists()).toBe(false)
  })

  it('an environment key is reported, not shown', async () => {
    setup(status({ source: 'env', set: true }))
    const w = await mountIt()
    expect(w.find('[data-testid="llm-key-env"]').exists()).toBe(true)
    expect(w.find('[data-testid="llm-key-migrate"]').exists()).toBe(false)
  })

  it('surfaces an error from the keychain', async () => {
    const app = setup(status())
    app.SetLLMKey.mockRejectedValueOnce(new Error('keychain locked or not responding'))
    const w = await mountIt()
    await w.get('[data-testid="llm-key-input"]').setValue('abcdef')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="status"]').text()).toContain('keychain locked')
  })
})
