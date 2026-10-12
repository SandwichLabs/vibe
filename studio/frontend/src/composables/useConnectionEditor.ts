/**
 * useConnectionEditor — what the connection editor does with its form (F10):
 * load it from the route, Test, Save (with the three confirmations: allow
 * changes to a remote database, re-bind an environment sign-in to a changed
 * server, delete the keychain secret you no longer use), clear a saved secret,
 * remove, and the keyboard and leave-with-unsaved-edits guards.
 */
import { ref, nextTick, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import { App } from '../lib/bindings'
import { askConfirm } from './useConfirm'
import { extensionFor, refreshConnections } from './useConnections'
import { errorText } from '../lib/errors'
import { scopeLabel } from '../lib/connections'
import type { ConnectionForm } from './useConnectionForm'
import type { MotherDuckPicker } from './useMotherDuckPicker'

export function useConnectionEditor(form: ConnectionForm, md: MotherDuckPicker) {
  const route = useRoute()
  const router = useRouter()
  const {
    f, typed, method, envPrefix, status, detail, typeId, spec, readOnly, connScope, origScope, origMethod,
    hadKeychainSecret, saved, saveError, result, isNew, isMD, remote, needsSignIn, methodString, bindingChanged,
    dirty,
  } = form

  const saving = ref(false)
  const savedNote = ref('')
  const testing = ref(false)
  const resultEl = ref<{ focus: () => void } | null>(null)
  const errorEl = ref<HTMLElement | null>(null)
  const keychainBlocked = ref(false)
  const keychainDetail = ref('')

  async function load() {
    md.resetMD()
    await form.loadForm()
    if (!isNew.value && route.query.test === '1' && !form.loadError.value) void runTest()
  }

  // ---- test ----
  async function runTest() {
    if (!spec.value || testing.value) return
    testing.value = true
    result.value = null
    try {
      result.value = await App.TestConnection({
        scope: connScope.value,
        def: { ...form.buildDef(), name: f.name.trim() || 'test' },
        signIn: methodString.value,
        credentials: { ...typed },
      })
    } catch (e: unknown) {
      result.value = { ok: false, status: 'error', ms: 0, count: 0, error: { code: 'error', message: errorText(e), details: '' } }
    }
    testing.value = false
    await nextTick()
    resultEl.value?.focus()
  }

  async function installFor(ext: string) {
    extensionFor.value = { connection: f.name, extension: ext }
  }

  // ---- save ----
  function askRebind(name: string): Promise<boolean> {
    return askConfirm({
      title: `Use ${envPrefix.value}_* for the new server?`,
      message: `The server of ${name} changed since you chose environment variables for it. The variables ${envPrefix.value}_* will be sent to the new server. Only continue if they are meant for it.`,
      variant: 'destructive',
      confirmLabel: 'Use them for the new server',
    })
  }

  async function save() {
    saveError.value = ''
    savedNote.value = ''
    const bad = form.validate()
    if (bad) { saveError.value = bad; await nextTick(); errorEl.value?.focus(); return }
    const name = f.name.trim()
    // Letting the agent and you change a remote database deserves one more look.
    if (remote.value && !isMD.value && !readOnly.value && (isNew.value || detail.value?.def.read_only !== false)) {
      const ok = await askConfirm({ title: `Allow changes to ${name}?`, message: 'Changes are allowed on this connection. You and the agent can edit data there. The agent will still ask you before every change.', variant: 'primary', confirmLabel: 'Allow changes' })
      if (!ok) return
    }
    // An environment sign-in is bound to the server it was chosen for. Saving a
    // changed server re-binds it, so the variables would go to the new one: ask.
    let confirmRebind = false
    await form.refreshDraftFingerprint() // Save must not read a stale answer
    if (!isNew.value && method.value === 'env' && bindingChanged.value) {
      if (!(await askRebind(name))) return
      confirmRebind = true
    }
    const wantsSecret = method.value !== 'env' && !!(typed.password || typed.token || typed.secret)
    const credsChanged = method.value !== 'env' && (wantsSecret || typed.username !== (status.value?.username ?? '') || typed.keyId !== (status.value?.keyId ?? ''))
    if (method.value === 'keychain' && wantsSecret) {
      const probe = await App.ProbeKeychain()
      if (!probe.available) { keychainDetail.value = probe.detail ?? ''; keychainBlocked.value = true; return }
    }
    saving.value = true
    try {
      if (!isNew.value && connScope.value !== origScope.value) {
        await App.MoveConnection({ name, to: connScope.value })
      }
      const saveReq = { scope: connScope.value, def: form.buildDef(), isNew: isNew.value, signIn: needsSignIn.value ? methodString.value : '', confirmRebind }
      let d: ConnectionDetail
      try {
        d = await App.SaveConnection(saveReq)
      } catch (e: unknown) {
        // The backend refuses to re-bind an environment sign-in without a yes, whatever
        // the screen thought: ask now and send it again.
        if (!errorText(e).startsWith('confirm-rebind:') || !(await askRebind(name))) throw e
        confirmRebind = true
        d = await App.SaveConnection({ ...saveReq, confirmRebind })
      }
      if (needsSignIn.value && credsChanged && (typed.username || typed.keyId || wantsSecret)) {
        await App.SetConnectionCredentials({ name, method: methodString.value, ...typed, remember: false, confirmRebind })
      }
      Object.assign(typed, { password: '', token: '', secret: '' })
      // Switching away from the keychain leaves its saved sign-in unused: offer to
      // delete it rather than leave a password nobody can see or reach.
      if (!isNew.value && origMethod.value === 'keychain' && method.value !== 'keychain' && hadKeychainSecret.value) {
        const del = await askConfirm({
          title: 'Delete the saved sign-in?',
          message: `${name} no longer signs in from your keychain, so the password saved there is not used any more. Delete it?`,
          variant: 'destructive',
          confirmLabel: 'Delete it',
        })
        if (del) await App.ClearConnectionCredentials({ name, field: '' })
      }
      saved.value = true
      savedNote.value = `Saved to ${scopeLabel(connScope.value)}.`
      await refreshConnections()
      if (isNew.value) await router.replace(`/connections/${name}`)
      else { await load(); saved.value = true }
      savedNote.value = `Saved to ${scopeLabel(d.scope)}.`
    } catch (e: unknown) {
      saveError.value = errorText(e)
      await nextTick()
      errorEl.value?.focus()
    }
    saving.value = false
  }

  // ---- credentials ----
  async function clearSecret(field: string) {
    const ok = await askConfirm({
      title: 'Clear the saved sign-in?',
      message: `Clear the saved ${field === 'password' ? 'password' : field === 'token' ? 'token' : 'secret key'} for ${f.name}? You'll be asked for it next time you connect.`,
      variant: 'destructive',
      confirmLabel: 'Clear',
    })
    if (!ok) return
    try {
      await App.ClearConnectionCredentials({ name: f.name, field })
      status.value = await App.GetConnectionCredentialStatus(f.name)
      await refreshConnections()
    } catch (e: unknown) { saveError.value = errorText(e) }
  }

  function useEnvInstead() {
    keychainBlocked.value = false
    method.value = 'env'
    if (!envPrefix.value) envPrefix.value = f.name.toUpperCase() || 'VIBE'
  }

  // ---- remove ----
  async function removeMine() {
    try {
      await App.DeleteConnection({ name: f.name, scope: 'local' })
      await refreshConnections()
      saved.value = true
      await router.replace(`/connections/${f.name}`)
    } catch (e: unknown) { saveError.value = errorText(e) }
  }

  async function remove() {
    const ok = await askConfirm({
      title: `Remove ${f.name}?`,
      message: `This removes the connection from ${scopeLabel(connScope.value)}. Your database isn't touched. Saved credentials in your keychain are also deleted.`,
      variant: 'destructive',
      confirmLabel: 'Remove',
    })
    if (!ok) return
    try {
      await App.DeleteConnection({ name: f.name, scope: connScope.value })
      await refreshConnections()
      saved.value = true
      await router.replace('/')
    } catch (e: unknown) { saveError.value = errorText(e) }
  }

  // ---- leaving ----
  async function cancel() {
    await router.push('/')
  }
  onBeforeRouteLeave(async () => {
    if (!dirty.value) return true
    return askConfirm({
      title: isNew.value ? 'Discard this connection?' : 'Discard your changes?',
      message: isNew.value ? "It hasn't been saved yet." : 'Your edits to this connection will be lost.',
      variant: 'destructive',
      confirmLabel: 'Discard',
    })
  })

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && typeId.value) { e.preventDefault(); void runTest() }
    else if (e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('[role=dialog], [role=alertdialog]')) { void cancel() }
  }

  onMounted(() => { void load(); window.addEventListener('keydown', onKey) })
  onUnmounted(() => window.removeEventListener('keydown', onKey))
  watch(() => [route.path, route.params.name, route.query.scope, route.query.from], () => { void load() })

  return {
    saving, savedNote, testing, resultEl, errorEl, keychainBlocked, keychainDetail,
    runTest, installFor, save, clearSecret, useEnvInstead, removeMine, remove, cancel,
  }
}
