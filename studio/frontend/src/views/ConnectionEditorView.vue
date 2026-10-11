<script setup lang="ts">
// ConnectionEditorView — add or edit a connection (SPEC 06). A full-page route
// (#/connections/new, #/connections/<name>) so there is room to explain and the
// rail stays visible. The form is generated from the type registry; only the
// secret half of a credential is write-only (password, token, secret key), the
// username and key id are readable. Leaving with unsaved edits asks first.
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import Icon from '../components/Icon.vue'
import ConnTypeTile from '../components/ConnTypeTile.vue'
import ConnectionTypePicker from '../components/ConnectionTypePicker.vue'
import CredentialField from '../components/CredentialField.vue'
import ScopePicker from '../components/ScopePicker.vue'
import TestResult from '../components/TestResult.vue'
import { askConfirm } from '../composables/useConfirm'
import { extensionFor, refreshConnections } from '../composables/useConnections'
import { shortcutLabel } from '../composables/useModal'
import { workspaceInfo } from '../composables/useWorkspace'
import { errorText } from '../lib/errors'
import { NAME_RE, typeLabel, scopeLabel } from '../lib/connections'

const route = useRoute()
const router = useRouter()

const types = ref<ConnTypeSpec[]>([])
const loading = ref(true)
const loadError = ref('')
const isNew = computed(() => route.path === '/connections/new')
const editName = computed(() => (isNew.value ? '' : String(route.params.name ?? '')))
const typeId = ref('')
const spec = computed(() => types.value.find((t) => t.id === typeId.value) ?? null)

// ---- form state ----
const f = reactive({ name: '', host: '', port: '', database: '', path: '', region: '', scope: '', options: {} as Record<string, string> })
const readOnly = ref(true)
const connScope = ref<ConnScope>('local')
const origScope = ref<ConnScope>('local')
const method = ref<'keychain' | 'env' | 'prompt'>('keychain')
const envPrefix = ref('')
const typed = reactive<CredentialInput>({ username: '', keyId: '', password: '', token: '', secret: '' })
const status = ref<ConnCredentialStatus | null>(null)
const detail = ref<ConnectionDetail | null>(null)
const advancedOpen = ref(false)
const initial = ref('')
// the sign-in method as saved, to notice a switch away from the keychain
const origMethod = ref<'keychain' | 'env' | 'prompt'>('keychain')
const hadKeychainSecret = ref(false)

const saving = ref(false)
const saved = ref(false)
const saveError = ref('')
const savedNote = ref('')
const testing = ref(false)
const result = ref<TestResultView | null>(null)
const resultEl = ref<InstanceType<typeof TestResult> | null>(null)
const errorEl = ref<HTMLElement | null>(null)
const keychainBlocked = ref(false)
const keychainDetail = ref('')

const isMD = computed(() => typeId.value === 'motherduck')
const remote = computed(() => !!spec.value?.capabilities.remote)
// A required option (a DuckLake's data folder, R2's account id) is not an
// advanced setting: it sits with the main fields, with its asterisk.
const mainFields = computed(() => (spec.value?.fields ?? []).filter((x) => (!x.option || x.required) && x.name !== 'port'))
const advFields = computed(() => (spec.value?.fields ?? []).filter((x) => x.option && !x.required))
// These sources trust their own metadata (it decides which files are read), so
// only the person adds them, in their own scopes.
const ownScopeOnly = computed(() => ['ducklake', 'iceberg', 'delta'].includes(typeId.value))
const insideWorkspace = computed(() => {
  if (!ownScopeOnly.value) return false
  const root = workspaceInfo.value?.root ?? ''
  const loc = [f.path, f.options['data_path'] ?? ''].map((x) => x.trim()).filter(Boolean)
  return loc.some((x) => {
    if (x.includes('://')) return false
    if (x.startsWith('/') || x.startsWith('~') || /^[A-Za-z]:[\\/]/.test(x)) return !!root && x.startsWith(root)
    return true // relative paths resolve inside the workspace folder
  })
})
// What a qualified table name looks like for this type.
const exampleTail = computed(() => {
  switch (typeId.value) {
    case 'postgres': return '.public.orders'
    case 'mysql': return `.${f.database.trim() || 'database'}.orders`
    case 'duckdb': case 'sqlite': case 'ducklake': case 'motherduck': return '.main.orders'
    default: return ''
  }
})
const portField = computed(() => (spec.value?.fields ?? []).find((x) => x.name === 'port'))
const credFields = computed(() => spec.value?.credentials ?? [])
const readableCreds = computed(() => credFields.value.filter((c) => !c.secret))
const secretCreds = computed(() => credFields.value.filter((c) => c.secret))
const primarySecret = computed(() => secretCreds.value[0]?.name ?? '')
const needsSignIn = computed(() => credFields.value.length > 0)

const KEY: Record<string, keyof CredentialInput> = { user: 'username', key_id: 'keyId', password: 'password', token: 'token', secret: 'secret' }
const ENVSUF: Record<string, string> = { user: 'USER', key_id: 'KEY_ID', password: 'PASSWORD', token: 'TOKEN', secret: 'SECRET' }

const methodString = computed(() => (method.value === 'env' ? `env:${envPrefix.value.trim() || 'VIBE'}` : method.value))
const envVars = computed(() => {
  const p = envPrefix.value.trim() || 'VIBE'
  const known = new Map((status.value?.env ?? []).map((e) => [e.name, e.set]))
  return credFields.value.map((c) => {
    const n = `${p}_${ENVSUF[c.name] ?? c.name.toUpperCase()}`
    return { name: n, set: known.has(n) ? known.get(n)! : null }
  })
})

function val(field: ConnField): string {
  if (field.option) return f.options[field.name] ?? ''
  if (field.name === 'scope') return f.scope
  return (f as any)[field.name] ?? ''
}
function setVal(field: ConnField, v: string) {
  if (field.option) f.options[field.name] = v
  else if (field.name === 'scope') f.scope = v
  else (f as any)[field.name] = v
}

function buildDef(): ConnectionDef {
  const d: ConnectionDef = { name: f.name.trim(), type: typeId.value }
  const set = (k: keyof ConnectionDef, v: string) => { if (v.trim() !== '') (d as any)[k] = v.trim() }
  for (const fd of spec.value?.fields ?? []) {
    if (fd.option) continue
    const v = val(fd)
    if (fd.name === 'port') { if (v.trim() !== '') d.port = parseInt(v, 10) || 0 }
    else set(fd.name as keyof ConnectionDef, v)
  }
  const opts: Record<string, string> = {}
  for (const fd of advFields.value) {
    const v = (f.options[fd.name] ?? '').trim()
    if (v) opts[fd.name] = v
  }
  if (Object.keys(opts).length) d.options = opts
  if (!isMD.value && !readOnly.value) d.read_only = false
  return d
}

// fingerprintOf is what a saved sign-in is bound to: the type and the fields the
// registry lists as fingerprint fields (host, port, database, path, scope, region
// and option:<name>). When it differs from the loaded definition, the saved
// secret will not be used for this server.
function fingerprintOf(d: ConnectionDef | undefined, fields: string[]): string {
  if (!d) return ''
  const get = (f: string): string => {
    if (f === 'type') return d.type ?? ''
    if (f.startsWith('option:')) return (d.options ?? {})[f.slice(7)] ?? ''
    const v = (d as unknown as Record<string, unknown>)[f]
    return v === undefined || v === null ? '' : String(v).trim()
  }
  return fields.map((f) => `${f}=${get(f)}`).join('|')
}
const fingerprintChanged = computed(() => {
  if (isNew.value || !detail.value || !spec.value) return false
  const fp = spec.value.fingerprintFields.length ? spec.value.fingerprintFields : ['type', 'host', 'port', 'database', 'path', 'scope', 'region']
  return fingerprintOf(buildDef(), fp) !== fingerprintOf(detail.value.def, fp)
})

// bindingChanged: the saved sign-in is not bound to this definition any more,
// because a pull changed it (the binding says so) or because it is edited here.
// An environment sign-in would follow the new server, so Save must ask.
const bindingChanged = computed(() => !isNew.value && !!detail.value && (
  !detail.value.credential.fingerprintMatches || !!detail.value.info.serverChanged || fingerprintChanged.value))

const snapshot = () => JSON.stringify([typeId.value, buildDef(), connScope.value, methodString.value, typed.username, typed.keyId, typed.password, typed.token, typed.secret])
const dirty = computed(() => !loading.value && !!typeId.value && !saved.value && snapshot() !== initial.value)

// ---- loading ----
function applyDef(d: ConnectionDef) {
  f.name = d.name
  f.host = d.host ?? ''
  f.port = d.port ? String(d.port) : ''
  f.database = d.database ?? ''
  f.path = d.path ?? ''
  f.region = d.region ?? ''
  f.scope = d.scope ?? ''
  f.options = { ...(d.options ?? {}) }
  readOnly.value = d.read_only === undefined || d.read_only === null ? d.type !== 'motherduck' : !!d.read_only
  advancedOpen.value = Object.keys(f.options).length > 0
}

function applyMethod(m: string) {
  if (m.startsWith('env:')) { method.value = 'env'; envPrefix.value = m.slice(4) }
  else if (m === 'prompt') method.value = 'prompt'
  else method.value = 'keychain'
}

function chooseType(id: string, from?: ConnectionDef) {
  typeId.value = id
  const t = types.value.find((x) => x.id === id)
  for (const fd of t?.fields ?? []) {
    if (fd.default && !from) setVal(fd, fd.default)
  }
  // Settings the type cannot do without (a DuckLake's data folder, Azure's
  // sign-in) live under Advanced: show them open on a new connection.
  if (!from && (t?.fields ?? []).some((fd) => fd.option && (fd.required || fd.default))) advancedOpen.value = true
  if (id === 'motherduck') readOnly.value = false
  if (['ducklake', 'iceberg', 'delta'].includes(id) && connScope.value === 'shared') connScope.value = 'local'
  envPrefix.value = id === 'motherduck' ? 'MOTHERDUCK' : ''
  initial.value = snapshot()
  void nextTick(() => document.getElementById('f-name')?.focus())
}

async function load() {
  resetMD()
  loading.value = true
  loadError.value = ''
  try {
    types.value = await window.go.main.App.ListConnectionTypes()
    saved.value = false
    result.value = null
    saveError.value = ''
    Object.assign(typed, { username: '', keyId: '', password: '', token: '', secret: '' })
    if (isNew.value) {
      const from = typeof route.query.from === 'string' ? route.query.from : ''
      if (from) {
        const d = await window.go.main.App.GetConnection(from)
        typeId.value = d.def.type
        applyDef({ ...d.def, name: `${d.def.name}_copy` })
        // A duplicate never inherits a sign-in method: it starts at the keychain,
        // needing credentials, so nothing stored for the original is used for it.
        method.value = 'keychain'
        envPrefix.value = ''
        origMethod.value = 'keychain'
        hadKeychainSecret.value = false
        connScope.value = 'local'
        origScope.value = 'local'
        status.value = null
        detail.value = null
        initial.value = snapshot()
      } else {
        typeId.value = ''
        applyDef({ name: '', type: '' })
        connScope.value = 'local'
        origScope.value = 'local'
        method.value = 'keychain'
        status.value = null
        detail.value = null
        initial.value = ''
      }
    } else {
      const scope = typeof route.query.scope === 'string' ? (route.query.scope as ConnScope) : ('' as const)
      const d = await window.go.main.App.GetConnectionInScope({ name: editName.value, scope })
      detail.value = d
      typeId.value = d.def.type
      applyDef(d.def)
      applyMethod(d.signIn)
      origMethod.value = method.value
      hadKeychainSecret.value = method.value === 'keychain' && d.credential.secretSet && !d.credential.sessionOnly
      connScope.value = d.scope
      origScope.value = d.scope
      status.value = d.credential
      typed.username = d.credential.username ?? ''
      typed.keyId = d.credential.keyId ?? ''
      initial.value = snapshot()
    }
  } catch (e: unknown) {
    loadError.value = errorText(e)
  }
  loading.value = false
  if (!isNew.value && route.query.test === '1' && !loadError.value) void runTest()
}

onMounted(() => { void load(); window.addEventListener('keydown', onKey) })
onUnmounted(() => window.removeEventListener('keydown', onKey))
watch(() => [route.path, route.params.name, route.query.scope, route.query.from], () => { void load() })

// ---- test ----
async function runTest() {
  if (!spec.value || testing.value) return
  testing.value = true
  result.value = null
  try {
    result.value = await window.go.main.App.TestConnection({
      scope: connScope.value,
      def: { ...buildDef(), name: f.name.trim() || 'test' },
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

// ---- MotherDuck database picker ----
// The list comes from the account the token belongs to, loaded in a separate
// process like Test. A typed token goes over the bridge once and is never
// returned; typing the name by hand stays possible whatever happens here.
const mdDbs = ref<MotherDuckDatabase[]>([])
const mdLoading = ref(false)
const mdLoaded = ref(false)
const mdError = ref('')
const mdInstall = ref('')
const mdTokenReady = computed(() => !!typed.token.trim() || method.value === 'env' || (!isNew.value && method.value !== 'prompt' && !!status.value?.secretSet))
let mdReq = 0
function resetMD() { mdReq++; mdLoading.value = false; mdLoaded.value = false; mdDbs.value = []; mdError.value = ''; mdInstall.value = '' }
async function loadDatabases() {
  if (mdLoading.value) return
  const my = ++mdReq
  const key = JSON.stringify([route.path, route.params.name, methodString.value, typed.token])
  const stale = () => my !== mdReq || key !== JSON.stringify([route.path, route.params.name, methodString.value, typed.token])
  mdLoading.value = true
  mdError.value = ''
  mdInstall.value = ''
  try {
    const r = await window.go.main.App.ListMotherDuckDatabases({
      name: isNew.value ? '' : f.name,
      scope: connScope.value,
      signIn: methodString.value,
      credentials: method.value === 'env' ? { username: '', keyId: '', password: '', token: '', secret: '' } : { ...typed },
      install: false,
    })
    if (stale()) return
    mdLoaded.value = r.ok
    mdDbs.value = r.databases ?? []
    if (!r.ok) { mdError.value = r.message ?? 'Couldn\'t load your databases.'; mdInstall.value = r.installExtension ?? '' }
  } catch (e: unknown) {
    if (stale()) return
    mdLoaded.value = false
    mdError.value = errorText(e)
  }
  mdLoading.value = false
}
function pickDatabase(name: string) {
  f.database = name
  result.value = null
}
// A different token or sign-in means a different account: the old list is stale.
watch(() => [typed.token, methodString.value], resetMD)

async function installFor(ext: string) {
  extensionFor.value = { connection: f.name, extension: ext }
}

// ---- save ----
function validate(): string {
  if (!spec.value) return 'Choose a connection type first.'
  if (isNew.value && !NAME_RE.test(f.name.trim())) return 'Give it a name with lowercase letters, numbers and underscores, like prod_pg.'
  for (const fd of spec.value.fields) {
    if (fd.required && !fd.option && val(fd).trim() === '') return `${fd.label} is needed.`
  }
  for (const fd of advFields.value) {
    if (fd.required && !(f.options[fd.name] ?? '').trim()) return `${fd.label} is needed.`
  }
  return ''
}

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
  const bad = validate()
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
  if (!isNew.value && method.value === 'env' && bindingChanged.value) {
    if (!(await askRebind(name))) return
    confirmRebind = true
  }
  const wantsSecret = method.value !== 'env' && !!(typed.password || typed.token || typed.secret)
  const credsChanged = method.value !== 'env' && (wantsSecret || typed.username !== (status.value?.username ?? '') || typed.keyId !== (status.value?.keyId ?? ''))
  if (method.value === 'keychain' && wantsSecret) {
    const probe = await window.go.main.App.ProbeKeychain()
    if (!probe.available) { keychainDetail.value = probe.detail ?? ''; keychainBlocked.value = true; return }
  }
  saving.value = true
  try {
    if (!isNew.value && connScope.value !== origScope.value) {
      await window.go.main.App.MoveConnection({ name, to: connScope.value })
    }
    const saveReq = { scope: connScope.value, def: buildDef(), isNew: isNew.value, signIn: needsSignIn.value ? methodString.value : '', confirmRebind }
    let d: ConnectionDetail
    try {
      d = await window.go.main.App.SaveConnection(saveReq)
    } catch (e: unknown) {
      // The backend refuses to re-bind an environment sign-in without a yes, whatever
      // the screen thought: ask now and send it again.
      if (!errorText(e).startsWith('confirm-rebind:') || !(await askRebind(name))) throw e
      confirmRebind = true
      d = await window.go.main.App.SaveConnection({ ...saveReq, confirmRebind })
    }
    if (needsSignIn.value && credsChanged && (typed.username || typed.keyId || wantsSecret)) {
      await window.go.main.App.SetConnectionCredentials({ name, method: methodString.value, ...typed, remember: false, confirmRebind })
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
      if (del) await window.go.main.App.ClearConnectionCredentials({ name, field: '' })
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
    await window.go.main.App.ClearConnectionCredentials({ name: f.name, field })
    status.value = await window.go.main.App.GetConnectionCredentialStatus(f.name)
    await refreshConnections()
  } catch (e: unknown) { saveError.value = errorText(e) }
}

const secretIsSet = (name: string) => !isNew.value && !fingerprintChanged.value && name === primarySecret.value && !!status.value?.secretSet && status.value.fingerprintMatches && method.value !== 'env' && (status.value.method === methodString.value || status.value.method === method.value)

// A saved secret that will not be used because the server changed says so where
// it would have been shown.
const serverChangedHelp = computed(() => (!isNew.value && (fingerprintChanged.value || (!!status.value?.secretSet && status.value.fingerprintMatches === false)) && method.value === 'keychain'
  ? "The server changed, so your saved password won't be used. Enter it again." : ''))

// ---- shadowing, remove, duplicate ----
const privateCopy = computed(() => !isNew.value && detail.value && detail.value.scope === 'local' && detail.value.shadows.length > 0 && !detail.value.shadowedBy)
const shadowedBy = computed(() => detail.value?.shadowedBy ?? '')

async function removeMine() {
  try {
    await window.go.main.App.DeleteConnection({ name: f.name, scope: 'local' })
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
    await window.go.main.App.DeleteConnection({ name: f.name, scope: connScope.value })
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

function useEnvInstead() {
  keychainBlocked.value = false
  method.value = 'env'
  if (!envPrefix.value) envPrefix.value = f.name.toUpperCase() || 'VIBE'
}

// The fields a saved sign-in is bound to, in words, from the type's own list.
const fingerprintHelp = computed(() => {
  const labels: Record<string, string> = { host: 'host', port: 'port', database: 'database', scope: 'scope', region: 'region' }
  const have = new Set((spec.value?.fields ?? []).filter((x) => !x.option).map((x) => x.name))
  const names = (spec.value?.fingerprintFields ?? []).filter((f) => labels[f] && have.has(f)).map((f) => labels[f])
  if (!names.length) return ''
  const list = names.length === 1 ? names[0] : `${names.slice(0, -1).join(', ')} or ${names[names.length - 1]}`
  return `If the ${list} change${names.length === 1 ? 's' : ''}, everyone is asked to sign in again to confirm.`
})

const failedTest = computed(() => !!result.value && !result.value.ok)
const title = computed(() => (isNew.value ? 'New connection' : `Edit ${f.name}`))
</script>

<template>
  <div class="page-narrow" style="height: 100%; overflow-y: auto; padding: 32px;" data-testid="connection-editor">
    <div class="headrow">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Connections <span class="sep">/</span> {{ isNew ? 'New' : editName }}</div>
        <h1>{{ title }}</h1>
      </div>
      <div v-if="!isNew" style="display: flex; gap: 8px;">
        <router-link class="btn btn-sm" :to="{ path: '/connections/new', query: { from: editName } }" data-testid="editor-duplicate"><Icon name="copy" :size="14" /> Duplicate</router-link>
        <button type="button" class="btn btn-sm btn-destructive" data-testid="editor-remove" @click="remove"><Icon name="trash" :size="14" /> Remove…</button>
      </div>
    </div>

    <div v-if="loading" class="help">Loading…</div>
    <div v-else-if="loadError" class="note attn" role="alert"><Icon name="alert" :size="16" /><span>{{ loadError }}</span></div>

    <!-- Step 1: type -->
    <section v-else-if="!typeId" aria-labelledby="pick-h">
      <h2 id="pick-h" style="font-size: 20px; margin-bottom: 12px;">Where does the data live?</h2>
      <ConnectionTypePicker :types="types" @choose="chooseType" />
      <div style="margin-top: 20px;"><button type="button" class="btn" @click="cancel">Cancel</button></div>
    </section>

    <!-- Step 2: the generated form -->
    <form v-else class="card" style="margin-top: 16px;" novalidate @submit.prevent="save">
      <div v-if="privateCopy" class="note info" style="margin: 16px 24px 0;" data-testid="shadow-banner">
        <Icon name="layers" :size="16" />
        <span><b>This is your private copy of {{ f.name }}.</b> On this computer it overrides the {{ scopeLabel(detail!.shadows[0]) }} <code>{{ f.name }}</code>.
          <button type="button" class="linkbtn" @click="router.push({ path: `/connections/${f.name}`, query: { scope: detail!.shadows[0] } })">Look at the {{ scopeLabel(detail!.shadows[0]) }} one</button>
          <button type="button" class="linkbtn danger" @click="removeMine">Remove my copy</button></span>
      </div>
      <div v-if="shadowedBy" class="note warn" style="margin: 16px 24px 0;">
        <Icon name="layers" :size="16" />
        <span>This {{ scopeLabel(detail!.scope) }} copy is overridden on this computer by your private copy.
          <button type="button" class="linkbtn" @click="router.push(`/connections/${f.name}`)">Open my copy</button></span>
      </div>

      <!-- Connection -->
      <div class="form-sec">
        <h3><ConnTypeTile :type="typeId" /> {{ typeLabel(typeId) }} <span class="label">Connection</span></h3>
        <div class="field">
          <label class="field-label" for="f-name">Name</label>
          <input id="f-name" v-model="f.name" class="field-input" :readonly="!isNew" autocomplete="off" spellcheck="false" aria-describedby="f-name-h" data-testid="f-name" />
          <div id="f-name-h" class="help">How you'll refer to it in SQL: <code>{{ f.name || 'prod_pg' }}<template v-if="spec?.capabilities.catalog">{{ exampleTail }}</template></code>. <template v-if="isNew">Lowercase letters, numbers and underscores. It can't be changed after you save.</template><template v-else>It can't be changed after saving.</template></div>
        </div>

        <div v-for="fd in mainFields" :key="fd.name" :class="fd.name === 'host' && portField ? 'form-grid' : ''">
          <div class="field">
            <label class="field-label" :for="`f-${fd.name}`">{{ fd.label }}<span v-if="fd.required" aria-hidden="true"> *</span></label>
            <div style="display: flex; gap: 8px;">
              <input :id="`f-${fd.name}`" :value="val(fd)" class="field-input" style="flex: 1;" :type="fd.kind === 'number' ? 'number' : 'text'" autocomplete="off" spellcheck="false" :aria-describedby="fd.help || fd.name === 'database' ? `f-${fd.name}-h` : undefined" :data-testid="`f-${fd.name}`" @input="setVal(fd, ($event.target as HTMLInputElement).value)" />
            </div>
            <div v-if="fd.help || fd.name === 'database'" :id="`f-${fd.name}-h`" class="help">{{ fd.help }}<template v-if="fd.name === 'database' && remote && fingerprintHelp">{{ ' ' + fingerprintHelp }}</template></div>
            <div v-if="isMD && fd.name === 'database'" class="md-picker" data-testid="md-picker">
              <div class="md-picker-bar">
                <button type="button" class="btn btn-sm" :disabled="mdLoading || !mdTokenReady" data-testid="md-load" @click="loadDatabases">
                  <Icon name="refresh" :size="14" /> {{ mdLoading ? 'Loading databases…' : mdLoaded ? 'Refresh databases' : 'Load databases' }}
                </button>
                <span v-if="!mdTokenReady && !mdLoading" class="help" data-testid="md-hint">Enter your token below to pick from your databases, or type the name.</span>
              </div>
              <div v-if="mdLoading" class="help" role="status" data-testid="md-loading">Asking MotherDuck which databases this token can reach…</div>
              <div v-else-if="mdError" class="note attn" role="alert" data-testid="md-error">
                <Icon name="alert" :size="16" />
                <span>{{ mdError }}
                  <button v-if="mdInstall" type="button" class="linkbtn" data-testid="md-install" @click="installFor(mdInstall)">Install the {{ mdInstall }} extension…</button></span>
              </div>
              <div v-else-if="mdLoaded && !mdDbs.length" class="help" role="status" data-testid="md-empty">This account has no databases yet. You can still type a name.</div>
              <div v-else-if="mdLoaded" role="group" aria-label="Your MotherDuck databases" class="md-dbs" data-testid="md-dbs">
                <button v-for="d in mdDbs" :key="d.name" type="button" class="md-db" :class="{ on: f.database === d.name }" :aria-pressed="f.database === d.name" :data-testid="`md-db-${d.name}`" @click="pickDatabase(d.name)">
                  {{ d.name }}<span v-if="d.share" class="md-share">shared</span>
                </button>
              </div>
            </div>
          </div>
          <div v-if="fd.name === 'host' && portField" class="field">
            <label class="field-label" for="f-port">{{ portField.label }}</label>
            <input id="f-port" :value="val(portField)" class="field-input" type="number" :placeholder="portField.default" data-testid="f-port" @input="setVal(portField, ($event.target as HTMLInputElement).value)" />
          </div>
        </div>

        <details v-if="advFields.length" :open="advancedOpen" @toggle="advancedOpen = ($event.target as HTMLDetailsElement).open">
          <summary style="cursor: pointer; font-weight: 600;">Advanced options</summary>
          <div style="display: flex; flex-direction: column; gap: 14px; margin-top: 12px;">
            <div v-for="fd in advFields" :key="fd.name" class="field">
              <label class="field-label" :for="`f-${fd.name}`">{{ fd.label }}</label>
              <select v-if="fd.kind === 'enum'" :id="`f-${fd.name}`" class="field-input" :value="val(fd)" :data-testid="`f-${fd.name}`" @change="setVal(fd, ($event.target as HTMLSelectElement).value)">
                <option value="">Default</option>
                <option v-for="c in fd.choices" :key="c" :value="c">{{ c }}</option>
              </select>
              <input v-else :id="`f-${fd.name}`" class="field-input" :value="val(fd)" autocomplete="off" :data-testid="`f-${fd.name}`" @input="setVal(fd, ($event.target as HTMLInputElement).value)" />
              <div v-if="fd.help" class="help">{{ fd.help }}</div>
            </div>
          </div>
        </details>
      </div>

      <!-- Sign-in -->
      <div v-if="needsSignIn" class="form-sec">
        <h3>Sign-in <span class="label">Just you · this computer</span></h3>
        <div class="opt-row" role="radiogroup" aria-label="How you sign in">
          <button type="button" role="radio" class="opt-card" :class="{ on: method === 'keychain' }" :aria-checked="method === 'keychain'" data-testid="method-keychain" @click="method = 'keychain'">
            <span><span class="ot">My keychain</span><span class="od">Saved once on this computer.</span></span>
          </button>
          <button type="button" role="radio" class="opt-card" :class="{ on: method === 'env' }" :aria-checked="method === 'env'" data-testid="method-env" @click="method = 'env'; envPrefix = envPrefix || f.name.toUpperCase()">
            <span><span class="ot">Environment variables</span><span class="od">Read from <code>{{ envVars[0]?.name }}</code> and friends when Vibe starts. Good for scripts and servers.</span></span>
          </button>
          <button type="button" role="radio" class="opt-card" :class="{ on: method === 'prompt' }" :aria-checked="method === 'prompt'" data-testid="method-prompt" @click="method = 'prompt'">
            <span><span class="ot">Ask me each time</span><span class="od">Vibe asks when it connects and forgets it when you close the workspace.</span></span>
          </button>
        </div>

        <div v-if="method === 'env' && bindingChanged" class="note warn" role="status" data-testid="env-server-changed">
          <Icon name="alert" :size="16" />
          <span><b>The server changed since you chose these variables.</b>{{ ' ' }}{{ detail?.info.serverChanged ? `(${detail.info.serverChanged.old} to ${detail.info.serverChanged.new}). ` : '' }}They are not used for it until you confirm: Save asks first, and nothing is sent in the meantime.</span>
        </div>
        <template v-if="method === 'keychain'">
          <div v-for="c in readableCreds" :key="c.name" class="field">
            <label class="field-label" :for="`f-c-${c.name}`">{{ c.label }}<span v-if="c.required" aria-hidden="true"> *</span></label>
            <input :id="`f-c-${c.name}`" class="field-input" autocomplete="off" spellcheck="false" :value="typed[KEY[c.name]]" :data-testid="`f-c-${c.name}`" @input="typed[KEY[c.name]] = ($event.target as HTMLInputElement).value" />
            <div v-if="c.help" class="help">{{ c.help }}</div>
          </div>
          <CredentialField
            v-for="c in secretCreds"
            :id="`f-c-${c.name}`"
            :key="c.name"
            :label="c.label"
            :is-set="secretIsSet(c.name)"
            :where="status?.sessionOnly ? 'For this session only' : undefined"
            :required="c.required"
            :help="serverChangedHelp || c.help"
            :model-value="typed[KEY[c.name]]"
            @update:model-value="typed[KEY[c.name]] = $event"
            @clear="clearSecret(c.name)"
          />
          <div class="note info"><Icon name="shieldCheck" :size="16" /><span>These go in your computer's keychain. They're never written to the workspace folder, to git, or to logs.</span></div>
        </template>
        <template v-else-if="method === 'env'">
          <div class="field">
            <label class="field-label" for="f-envprefix">Variable prefix</label>
            <input id="f-envprefix" v-model="envPrefix" class="field-input" autocomplete="off" spellcheck="false" data-testid="f-envprefix" />
          </div>
          <div v-for="v in envVars" :key="v.name" class="envvar" data-testid="envvar">
            <code>{{ v.name }}</code>
            <span v-if="v.set !== null" class="set" :class="v.set ? 'yes' : 'no'"><Icon :name="v.set ? 'circleCheck' : 'alert'" :size="14" /> {{ v.set ? 'Set' : 'Not set' }}</span>
          </div>
          <p class="help">Vibe shows whether each variable is set, never what it is. Set them in the environment Vibe starts from.</p>
        </template>
        <p v-else class="help">When you connect, Vibe asks for {{ credFields.map((c) => c.label.toLowerCase()).join(' and ') }} and forgets {{ credFields.length > 1 ? 'them' : 'it' }} when you close the workspace. The prompt can offer to remember {{ credFields.length > 1 ? 'them' : 'it' }} in your keychain, off by default.</p>
        <p class="help">The team's file only says which details are needed. How you sign in is your own choice and stays on this computer, so teammates can use a different way.</p>
      </div>

      <!-- Safety -->
      <div v-if="spec?.capabilities.catalog" class="form-sec">
        <h3>Safety</h3>
        <template v-if="isMD">
          <div class="note info" data-testid="md-note"><Icon name="info" :size="16" /><span>MotherDuck connections can't be set read-only here. For read-only access, sign in with a <b>read-scaling token</b>. The agent still asks before every change.</span></div>
        </template>
        <template v-else>
          <div class="switch-row">
            <button type="button" role="switch" class="toggle" :class="{ off: !readOnly }" :aria-checked="readOnly" aria-labelledby="ro-t" aria-describedby="ro-d" data-testid="f-readonly" @click="readOnly = !readOnly"></button>
            <span>
              <span id="ro-t" class="st-t">Read-only{{ readOnly ? ' (recommended)' : '' }}</span>
              <span id="ro-d" class="st-d">
                <template v-if="readOnly">Vibe will refuse to run changes on this connection, from you or the agent. This is a safety belt inside Vibe, not a lock on the database: a determined query can still get around it.</template>
                <template v-else>Changes are allowed. You and the agent can edit data here. The agent will still ask you before every change.</template>
              </span>
            </span>
          </div>
          <div v-if="readOnly && remote && typeId !== 'ducklake'" class="note warn"><Icon name="alert" :size="16" /><span><b>For production data, also use a read-only database user.</b> Ask your database admin for a role that can only read. That is the only way to be certain nothing can change.</span></div>
        </template>
      </div>

      <!-- Scope -->
      <div class="form-sec">
        <h3>Who can use this connection</h3>
        <div v-if="ownScopeOnly" class="note warn" data-testid="trust-note"><Icon name="alert" :size="16" /><span><b>Only point this at data you trust.</b> The metadata of a {{ spec?.label ?? 'lake or table' }} decides which files are read, so a crafted one can read any file on this computer. That is why only you can add it, not the team.<template v-if="insideWorkspace">{{ ' ' }}<b>This location is inside the workspace folder</b>, where files that came with a cloned repository may be crafted tables.</template></span></div>
        <ScopePicker v-model="connScope" :name="f.name" :own-only="ownScopeOnly" />
      </div>

      <!-- Result and actions -->
      <div v-if="saveError" ref="errorEl" tabindex="-1" class="note attn" role="alert" style="margin: 16px 24px 0;" data-testid="save-error"><Icon name="alert" :size="16" /><span>{{ saveError }}</span></div>
      <div v-if="savedNote" class="note" role="status" style="margin: 16px 24px 0;" data-testid="saved-note"><Icon name="circleCheck" :size="16" /><span>{{ savedNote }}</span></div>
      <div style="padding: 16px 24px 0;">
        <TestResult ref="resultEl" :result="result" :running="testing" :read-only="readOnly && !isMD" @install="installFor" />
      </div>
      <div class="test-bar">
        <button type="button" class="btn" :disabled="testing" data-testid="test-btn" @click="runTest"><Icon name="zap" :size="14" /> {{ testing ? 'Testing…' : 'Test connection' }}</button>
        <span class="help">Tries these settings without saving. Stops after 10 seconds.<template v-if="isMD"> MotherDuck is tested in a separate process.</template> <kbd>{{ shortcutLabel('Enter') }}</kbd></span>
        <button type="button" class="btn" @click="cancel">Cancel</button>
        <button type="submit" :class="failedTest ? 'btn' : 'btn btn-primary'" :disabled="saving" data-testid="save-btn">{{ saving ? 'Saving…' : failedTest ? 'Save anyway' : 'Save' }}</button>
      </div>
    </form>

    <!-- A2: keychain unreachable blocks saving a password -->
    <div v-if="keychainBlocked" class="modal-overlay" role="presentation">
      <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="kb-h" data-testid="keychain-dialog">
        <div class="d-head"><h2 id="kb-h">Can't reach your keychain</h2></div>
        <div class="d-body">
          <p>Vibe stores passwords in this computer's keychain, but it isn't answering. That's common over SSH or when no one is signed in to the desktop.</p>
          <p v-if="keychainDetail" class="help">{{ keychainDetail }}</p>
          <div class="note info"><Icon name="info" :size="16" /><span>You can still connect. Set the variables in the environment Vibe starts from, then choose <b>Environment variables</b> for this connection. Vibe never writes passwords to a file instead.</span></div>
        </div>
        <div class="d-foot">
          <button class="btn" type="button" @click="keychainBlocked = false">Try again</button>
          <button class="btn btn-primary" type="button" @click="useEnvInstead">Use environment variables</button>
        </div>
      </div>
    </div>
  </div>
</template>
