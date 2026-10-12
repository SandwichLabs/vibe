/**
 * useLlmSettings — Settings > App > The agent's model (F10): the saved provider /
 * model / endpoint / Cloudflare account (never the key), the form that changes
 * them, the model list for local servers, and the write-only API key: saved to
 * the OS keychain, replaced or cleared, never read back. A field set by a
 * VIBE_LLM_* variable is disabled with the variable's name. The component only
 * renders it.
 */
import { ref, computed, onMounted, nextTick } from 'vue'
import { App } from '../lib/bindings'
import { askConfirm } from './useConfirm'
import { errorText } from '../lib/errors'

export function useLlmSettings() {
  const view = ref<LLMSettingsView | null>(null)
  const keyStatus = ref<LLMKeyStatus | null>(null)
  const editing = ref(false)        // key replace form
  const changing = ref(false)       // provider/model form
  const form = ref<LLMSettingsInput>({ provider: 'local', model: '', endpoint: '', accountId: '', gateway: '' })
  const draft = ref('')
  const busy = ref(false)
  const message = ref('')
  const settingsMsg = ref('')
  const settingsErr = ref('')
  const keyInput = ref<HTMLInputElement | null>(null)
  const models = ref<{ loading: boolean; list: string[]; error: string; unreachable: boolean; loaded: boolean }>({ loading: false, list: [], error: '', unreachable: false, loaded: false })

  const providers = computed(() => view.value?.providers ?? [])
  const savedInfo = computed(() => providers.value.find((p) => p.id === view.value?.provider))
  const formInfo = computed(() => providers.value.find((p) => p.id === form.value.provider))
  // The key section follows the provider being picked while the form is open.
  const keyProviderId = computed(() => (changing.value ? form.value.provider : view.value?.provider ?? ''))
  const keyProviderArg = computed(() => (keyProviderId.value === view.value?.provider ? '' : keyProviderId.value))
  const keyInfo = computed(() => providers.value.find((p) => p.id === keyProviderId.value))
  const env = computed(() => view.value?.envOverrides ?? {})

  async function refreshKey() {
    const asked = keyProviderId.value
    try {
      const st = await App.GetLLMKeyStatus(keyProviderArg.value)
      if (asked === keyProviderId.value) keyStatus.value = st // a newer pick wins over a late answer
    } catch { if (asked === keyProviderId.value) keyStatus.value = null }
  }
  async function loadView() {
    try { view.value = await App.GetLLMSettings() } catch { view.value = null }
  }

  const source = computed(() => keyStatus.value?.source ?? 'none')
  const showForm = computed(() => editing.value || (source.value === 'none' || source.value === 'file') && !!keyStatus.value?.keychainAvailable)
  const keyLabel = computed(() => keyInfo.value?.keyLabel || 'API key')
  const keyOptionalNote = computed(() => (keyInfo.value?.keyOptional ? ' (optional)' : ''))

  // The saved provider's file key would be sent to the picked provider.
  const keyPending = computed(() => changing.value && form.value.provider !== view.value?.provider && keyStatus.value?.provider !== form.value.provider)
  const fileKeyHazard = computed(() => changing.value && form.value.provider !== view.value?.provider && keyStatus.value?.provider === form.value.provider && source.value === 'file')
  const needsModel = computed(() => form.value.provider === 'ollama' && !form.value.model.trim() && !env.value.model)

  const effectiveShown = computed(() => {
    const v = view.value
    if (!v) return ''
    if (!v.effectiveModel) return ''
    return v.model ? v.effectiveModel : `${v.effectiveModel} \u00b7 default`
  })

  function beginChange() {
    const v = view.value!
    form.value = { provider: v.provider, model: v.model, endpoint: v.endpoint, accountId: v.accountIdFromEnv ? '' : v.accountId, gateway: v.gateway }
    settingsErr.value = ''
    settingsMsg.value = ''
    models.value = { loading: false, list: [], error: '', unreachable: false, loaded: false }
    changing.value = true
    void refreshKey()
    void maybeLoadModels()
  }
  function cancelChange() {
    changing.value = false
    settingsErr.value = ''
    void refreshKey()
  }

  function pickProvider(id: string) {
    if (env.value.provider || id === form.value.provider) return
    const v = view.value!
    const next = providers.value.find((p) => p.id === id)!
    const defaults = providers.value.map((p) => p.defaultEndpoint).filter(Boolean)
    if (next.hasEndpoint && (!form.value.endpoint || defaults.includes(form.value.endpoint))) form.value.endpoint = next.defaultEndpoint ?? ''
    // A model id belongs to its provider: back to the saved one restores it, anything else starts empty.
    if (!env.value.model) form.value.model = id === v.provider ? v.model : ''
    form.value.provider = id
    models.value = { loading: false, list: [], error: '', unreachable: false, loaded: false }
    void refreshKey()
    void maybeLoadModels()
  }
  function onProviderKey(e: KeyboardEvent) {
    const ids = providers.value.map((p) => p.id)
    const i = ids.indexOf(form.value.provider)
    let n = i
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') n = (i + 1) % ids.length
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') n = (i + ids.length - 1) % ids.length
    else return
    e.preventDefault()
    pickProvider(ids[n])
    ;(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[role=radio]')[n]?.focus()
  }

  const listsModels = computed(() => form.value.provider === 'local' || form.value.provider === 'ollama')
  async function maybeLoadModels() {
    if (!listsModels.value) return
    await loadModels()
  }
  async function loadModels() {
    const prov = form.value.provider
    models.value = { ...models.value, loading: true }
    try {
      const r = await App.ListLLMModels(prov, form.value.endpoint)
      if (prov !== form.value.provider) return // the person moved on
      models.value = { loading: false, list: r.models ?? [], error: r.error ?? '', unreachable: !!r.unreachable, loaded: true }
    } catch (e: any) {
      models.value = { loading: false, list: [], error: errorText(e), unreachable: false, loaded: true }
    }
  }

  const suggestions = computed(() => {
    const list = formInfo.value?.suggestedModels ?? []
    return models.value.list.length ? models.value.list : list
  })
  const modelPlaceholder = computed(() => {
    const d = formInfo.value?.defaultModel
    return d ? `Default: ${d}` : 'Model id'
  })
  const ollamaSelect = computed(() => form.value.provider === 'ollama' && models.value.list.length > 0)
  const ollamaOptions = computed(() => {
    const l = [...models.value.list]
    if (form.value.model && !l.includes(form.value.model)) l.unshift(form.value.model)
    return l
  })
  const accountError = computed(() => {
    const a = form.value.accountId.trim()
    return form.value.provider === 'cloudflare' && a && !/^[0-9a-fA-F]{32}$/.test(a) ? 'A Cloudflare account ID is 32 hexadecimal characters.' : ''
  })
  const gatewayError = computed(() => {
    const g = form.value.gateway.trim()
    return form.value.provider === 'cloudflare' && g && !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(g) ? 'A gateway ID is letters, digits, dashes and underscores.' : ''
  })
  const needsAccount = computed(() => form.value.provider === 'cloudflare' && !form.value.accountId.trim() && !view.value?.accountIdFromEnv && !env.value.accountId)

  // A provider without an endpoint keeps whatever endpoint is saved (so switching
  // back to a local server finds it), but a built-in default is not written out.
  function keptEndpoint(): string {
    const e = view.value?.endpoint ?? ''
    return providers.value.some((p) => p.defaultEndpoint === e) ? '' : e
  }

  async function saveSettings() {
    if (accountError.value || gatewayError.value || needsModel.value || keyPending.value) return
    const label = formInfo.value?.label ?? form.value.provider
    if (fileKeyHazard.value) {
      const ok = await askConfirm({
        title: `Send your ~/.vibe.yaml key to ${label}?`,
        message: `The plaintext key in ~/.vibe.yaml is used for whichever provider is selected, so it would be sent to ${label}. Cancel and move it to the keychain first, or save a key for ${label} below.`,
        confirmLabel: `Use it for ${label}`,
      })
      if (!ok) return
    }
    busy.value = true
    settingsErr.value = ''
    settingsMsg.value = ''
    try {
      await App.SetLLMSettings({
        provider: form.value.provider,
        model: form.value.model.trim(),
        endpoint: formInfo.value?.hasEndpoint ? form.value.endpoint.trim() : keptEndpoint(),
        accountId: form.value.accountId.trim(),
        gateway: form.value.gateway.trim(),
      })
      changing.value = false
      await loadView()
      await refreshKey()
      settingsMsg.value = `Saved. The agent now uses ${savedInfo.value?.label ?? form.value.provider}.`
    } catch (e: any) {
      settingsErr.value = errorText(e)
    } finally { busy.value = false }
  }

  async function startReplace() {
    editing.value = true
    draft.value = ''
    await nextTick()
    keyInput.value?.focus()
  }

  async function save() {
    const key = draft.value.trim()
    if (!key) return
    busy.value = true
    message.value = ''
    try {
      await App.SetLLMKey(keyProviderArg.value, key)
      draft.value = ''
      editing.value = false
      message.value = 'Saved to your keychain.'
      await refreshKey()
    } catch (e: any) {
      message.value = 'Error: ' + errorText(e)
    } finally { busy.value = false }
  }

  async function clearKey() {
    const ok = await askConfirm({ title: 'Remove the API key?', message: 'Vibe will forget the key saved in your keychain. You can paste it again later.', confirmLabel: 'Remove key' })
    if (!ok) return
    busy.value = true
    message.value = ''
    try {
      await App.ClearLLMKey(keyProviderArg.value)
      editing.value = false
      message.value = 'Removed from your keychain.'
      await refreshKey()
    } catch (e: any) {
      message.value = 'Error: ' + errorText(e)
    } finally { busy.value = false }
  }

  async function migrate() {
    let overwrite = false
    if (keyStatus.value?.keychainHasKey) {
      const ok = await askConfirm({ title: 'Replace the key in your keychain?', message: 'Your keychain already has an API key. Moving the one from ~/.vibe.yaml replaces it.', confirmLabel: 'Replace key' })
      if (!ok) return
      overwrite = true
    }
    busy.value = true
    message.value = ''
    try {
      await App.MigrateLLMKeyToKeychain(overwrite)
      message.value = 'Moved to your keychain, and blanked in ~/.vibe.yaml.'
      await refreshKey()
    } catch (e: any) {
      message.value = 'Error: ' + errorText(e)
    } finally { busy.value = false }
  }

  onMounted(async () => {
    await loadView()
    await refreshKey()
  })

  return {
    view, keyStatus, editing, changing, form, draft, busy, message, settingsMsg, settingsErr, keyInput, models, providers, savedInfo, formInfo, keyProviderArg, keyInfo, env, source, showForm, keyLabel, keyOptionalNote, keyPending, fileKeyHazard, needsModel, effectiveShown, beginChange, cancelChange, pickProvider, onProviderKey, listsModels, maybeLoadModels, loadModels, suggestions, modelPlaceholder, ollamaSelect, ollamaOptions, accountError, gatewayError, needsAccount, saveSettings, startReplace, save, clearKey, migrate,
  }
}
