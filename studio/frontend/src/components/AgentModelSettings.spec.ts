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
  return { provider: 'anthropic', source: 'none', set: false, fileKey: false, canMigrate: false, keychainAvailable: true, keychainHasKey: false, ...over }
}

const PROVIDERS = [
  { id: 'local', label: 'Local server', description: 'd', needsKey: false, keyOptional: true, keyLabel: 'API key', keyEnvVars: [], hasEndpoint: true, defaultEndpoint: 'http://localhost:8080/v1', hasAccount: false, defaultModel: 'local', suggestedModels: [] },
  { id: 'ollama', label: 'Ollama', description: 'd', needsKey: false, keyOptional: true, keyLabel: 'API key', keyEnvVars: ['OLLAMA_API_KEY'], hasEndpoint: true, defaultEndpoint: 'http://localhost:11434/v1', hasAccount: false, defaultModel: '', suggestedModels: [] },
  { id: 'openai', label: 'OpenAI', description: 'd', needsKey: true, keyOptional: false, keyLabel: 'API key', keyEnvVars: ['OPENAI_API_KEY'], hasEndpoint: false, hasAccount: false, defaultModel: 'gpt-5.5', suggestedModels: ['gpt-5.5'] },
  { id: 'anthropic', label: 'Anthropic', description: 'd', needsKey: true, keyOptional: false, keyLabel: 'API key', keyEnvVars: ['ANTHROPIC_API_KEY'], hasEndpoint: false, hasAccount: false, defaultModel: 'claude-sonnet-5-5', suggestedModels: ['claude-sonnet-5-5'] },
  { id: 'cloudflare', label: 'Cloudflare AI Gateway', description: 'd', needsKey: true, keyOptional: false, keyLabel: 'Cloudflare API token', keyEnvVars: ['CLOUDFLARE_API_TOKEN'], hasEndpoint: false, hasAccount: true, defaultModel: 'openai/gpt-5.5', suggestedModels: ['openai/gpt-5.5'] },
]

function settingsView(over: Partial<LLMSettingsView> = {}): LLMSettingsView {
  return { provider: 'anthropic', model: '', effectiveModel: 'claude-sonnet-5-5', defaultModel: 'claude-sonnet-5-5', endpoint: 'http://localhost:8080/v1', accountId: '', gateway: '', accountIdFromEnv: false, envOverrides: {}, providers: PROVIDERS as any, ...over }
}

function setup(first: LLMKeyStatus, next?: LLMKeyStatus, view: LLMSettingsView = settingsView()) {
  let cur = first
  const app = {
    GetLLMSettings: vi.fn(async () => view),
    SetLLMSettings: vi.fn(async () => {}),
    ListLLMModels: vi.fn(async () => ({ models: ['qwen3:0.6b', 'llama3'] })),
    GetLLMKeyStatus: vi.fn(async (p: string) => ({ ...cur, provider: p || view.provider })),
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
  })})

describe('AgentModelSettings provider picker', () => {
  it('shows the effective model, never blank', async () => {
    setup(status())
    const w = await mountIt()
    expect(w.get('[data-testid="llm-effective-model"]').text()).toContain('claude-sonnet-5-5')
    expect(w.get('[data-testid="llm-effective-model"]').text()).toContain('default')
  })

  it('says to pick a model when the provider has none (Ollama)', async () => {
    setup(status(), undefined, settingsView({ provider: 'ollama', effectiveModel: '', defaultModel: '' }))
    const w = await mountIt()
    expect(w.get('[data-testid="llm-effective-model"]').text()).toContain('pick a model')
  })

  it('picks Cloudflare, shows its fields and the token label, saves', async () => {
    const app = setup(status(), undefined, settingsView())
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    expect(w.findAll('[role="radio"]')).toHaveLength(5)
    await w.get('[data-testid="llm-provider-cloudflare"]').trigger('click')
    await flushPromises()
    expect(app.GetLLMKeyStatus).toHaveBeenLastCalledWith('cloudflare')
    expect(w.text()).toContain('Cloudflare API token')
    expect(w.find('[data-testid="llm-endpoint"]').exists()).toBe(false)
    expect(w.get('[data-testid="llm-model"]').attributes('placeholder')).toBe('Default: openai/gpt-5.5')
    expect(w.get('[data-testid="llm-gateway"]').attributes('placeholder')).toBe('default')

    await w.get('[data-testid="llm-account"]').setValue('nope')
    expect(w.text()).toContain('32 hexadecimal')
    expect(w.get('[data-testid="llm-save-settings"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="llm-account"]').setValue('0123456789abcdef0123456789abcdef')
    await w.get('[data-testid="llm-form"]').trigger('submit')
    await flushPromises()
    expect(app.SetLLMSettings).toHaveBeenCalledWith(expect.objectContaining({ provider: 'cloudflare', accountId: '0123456789abcdef0123456789abcdef', model: '', gateway: '' }))
  })

  it('Ollama lists installed models in a select', async () => {
    const app = setup(status(), undefined, settingsView({ provider: 'local', effectiveModel: 'local' }))
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    await w.get('[data-testid="llm-provider-ollama"]').trigger('click')
    await flushPromises()
    expect(app.ListLLMModels).toHaveBeenCalledWith('ollama', 'http://localhost:11434/v1')
    const sel = w.get('select[data-testid="llm-model"]')
    expect(sel.findAll('option').map((o) => o.text())).toEqual(['Pick a model', 'qwen3:0.6b', 'llama3'])
  })

  it('Ollama not running and no models are clear states', async () => {
    const app = setup(status(), undefined, settingsView({ provider: 'openai' }))
    app.ListLLMModels.mockResolvedValueOnce({ models: [], unreachable: true, error: 'nothing answered' } as any)
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    await w.get('[data-testid="llm-provider-ollama"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="llm-ollama-down"]').text()).toContain("Ollama isn't running at http://localhost:11434/v1")
    app.ListLLMModels.mockResolvedValueOnce({ models: [] } as any)
    await w.get('[data-testid="llm-load-models"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="llm-ollama-empty"]').text()).toContain('ollama pull')
  })

  it('an env-overridden field is disabled and names its variable', async () => {
    setup(status(), undefined, settingsView({ envOverrides: { model: 'VIBE_LLM_MODEL' } }))
    const w = await mountIt()
    expect(w.text()).toContain('set by VIBE_LLM_MODEL')
    await w.get('[data-testid="llm-change"]').trigger('click')
    expect(w.get('[data-testid="llm-model"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('VIBE_LLM_MODEL')
  })

  it('warns before the plaintext file key would go to another provider, and asks on save', async () => {
    const app = setup(status({ source: 'file', set: true, fileKey: true, canMigrate: true }))
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    expect(w.find('[data-testid="llm-file-key-warning"]').exists()).toBe(false) // same provider
    await w.get('[data-testid="llm-provider-cloudflare"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="llm-file-key-warning"]').text()).toContain('would be sent to Cloudflare AI Gateway')
    ;(askConfirm as any).mockClear()
    ;(askConfirm as any).mockResolvedValueOnce(false)
    await w.get('[data-testid="llm-form"]').trigger('submit')
    await flushPromises()
    expect(askConfirm).toHaveBeenCalled()
    expect(app.SetLLMSettings).not.toHaveBeenCalled()
  })

  it('shows a save error', async () => {
    const app = setup(status())
    app.SetLLMSettings.mockRejectedValueOnce(new Error('the endpoint must be an http or https URL with a host'))
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    await w.get('[data-testid="llm-form"]').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="llm-settings-error"]').text()).toContain('http or https')
  })

  it('Ollama cannot be saved without a model, and says why', async () => {
    const app = setup(status(), undefined, settingsView({ provider: 'openai' }))
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    await w.get('[data-testid="llm-provider-ollama"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="llm-save-settings"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="llm-save-why"]').text()).toContain('Pick a model')
    await w.get('[data-testid="llm-form"]').trigger('submit')
    expect(app.SetLLMSettings).not.toHaveBeenCalled()
    await w.get('select[data-testid="llm-model"]').setValue('qwen3:0.6b')
    expect(w.get('[data-testid="llm-save-settings"]').attributes('disabled')).toBeUndefined()
  })

  it('does not trust a key status fetched for another provider (Save waits for it)', async () => {
    const app = setup(status({ source: 'file', set: true, fileKey: true, canMigrate: true }))
    const w = await mountIt()
    await w.get('[data-testid="llm-change"]').trigger('click')
    let release!: (v: any) => void
    app.GetLLMKeyStatus.mockImplementationOnce(() => new Promise((r) => { release = r }))
    await w.get('[data-testid="llm-provider-openai"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="llm-save-settings"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="llm-file-key-warning"]').exists()).toBe(false)
    release({ ...status({ source: 'file', set: true, fileKey: true, canMigrate: true }), provider: 'openai' })
    await flushPromises()
    expect(w.find('[data-testid="llm-file-key-warning"]').exists()).toBe(true)
    expect(w.get('[data-testid="llm-save-settings"]').attributes('disabled')).toBeUndefined()
  })

  it('lists key variables as separate code elements and names the provider the key moves to', async () => {
    setup(status({ source: 'file', set: true, fileKey: true, canMigrate: true }), undefined, settingsView({ provider: 'gemini', providers: [...PROVIDERS, { ...PROVIDERS[3], id: 'gemini', label: 'Google Gemini', keyEnvVars: ['GEMINI_API_KEY', 'GOOGLE_API_KEY'] }] as any }))
    const w = await mountIt()
    expect(w.html()).not.toContain('&lt;/code&gt;')
    expect(w.findAll('code').map((c) => c.text())).toEqual(expect.arrayContaining(['GEMINI_API_KEY', 'GOOGLE_API_KEY']))
    expect(w.get('[data-testid="llm-key-migrate"]').text()).toContain('for Google Gemini')
  })
})
