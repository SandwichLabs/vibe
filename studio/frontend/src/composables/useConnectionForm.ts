/**
 * useConnectionForm — the connection editor's form (F10): the definition and
 * sign-in being edited, the fields the type registry generates, the draft's
 * fingerprint (does a saved sign-in still belong to this server?), validation,
 * and loading a connection (or a duplicate, or a blank) into the form. Test,
 * save and remove are in useConnectionEditor; the MotherDuck picker is in
 * useMotherDuckPicker.
 */
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { App } from '../lib/bindings'
import { workspaceInfo } from './useWorkspace'
import { errorText } from '../lib/errors'
import { NAME_RE, CRED_ENV_SUFFIX } from '../lib/connections'

type SignIn = 'keychain' | 'env' | 'prompt'
const emptyCreds = (): CredentialInput => ({ username: '', keyId: '', password: '', token: '', secret: '' })

export function useConnectionForm() {
  const route = useRoute()

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
  const method = ref<SignIn>('keychain')
  const envPrefix = ref('')
  const typed = reactive<CredentialInput>(emptyCreds())
  const status = ref<ConnCredentialStatus | null>(null)
  const detail = ref<ConnectionDetail | null>(null)
  const advancedOpen = ref(false)
  const initial = ref('')
  // the sign-in method as saved, to notice a switch away from the keychain
  const origMethod = ref<SignIn>('keychain')
  const hadKeychainSecret = ref(false)
  // what the last save / test said
  const saved = ref(false)
  const saveError = ref('')
  const result = ref<TestResultView | null>(null)

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

  const methodString = computed(() => (method.value === 'env' ? `env:${envPrefix.value.trim() || 'VIBE'}` : method.value))
  const envVars = computed(() => {
    const p = envPrefix.value.trim() || 'VIBE'
    const known = new Map((status.value?.env ?? []).map((e) => [e.name, e.set]))
    return credFields.value.map((c) => {
      const n = `${p}_${CRED_ENV_SUFFIX[c.name] ?? c.name.toUpperCase()}`
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
    for (const fd of (spec.value?.fields ?? []).filter((x) => x.option)) { // required options too
      const v = (f.options[fd.name] ?? '').trim()
      if (v) opts[fd.name] = v
    }
    if (Object.keys(opts).length) d.options = opts
    if (!isMD.value && !readOnly.value) d.read_only = false
    return d
  }

  // What a saved sign-in is bound to is Go's call (workspace.Fingerprint: type,
  // host, port, database, scope, path, region and every option). The loaded
  // definition's fingerprint arrives with the detail; the draft's comes from the
  // ConnectionFingerprint binding, which sees no credential. When they differ,
  // the saved secret will not be used for this server.
  const draftFingerprint = ref('')
  let fpSeq = 0
  async function refreshDraftFingerprint(): Promise<void> {
    const seq = ++fpSeq
    if (isNew.value || !detail.value || !typeId.value) { draftFingerprint.value = ''; return }
    try {
      const fp = await App.ConnectionFingerprint(buildDef())
      if (seq === fpSeq) draftFingerprint.value = fp
    } catch {
      if (seq === fpSeq) draftFingerprint.value = ''
    }
  }
  watch(() => [isNew.value, detail.value?.fingerprint, JSON.stringify(buildDef())], () => { void refreshDraftFingerprint() }, { immediate: true })
  const fingerprintChanged = computed(() => !isNew.value && !!detail.value && draftFingerprint.value !== '' && draftFingerprint.value !== detail.value.fingerprint)

  // bindingChanged: the saved sign-in is not bound to this definition any more,
  // because a pull changed it (the binding says so) or because it is edited here.
  // An environment sign-in would follow the new server, so Save must ask.
  const bindingChanged = computed(() => !isNew.value && !!detail.value && (
    !detail.value.credential.fingerprintMatches || !!detail.value.info.serverChanged || fingerprintChanged.value))

  const snapshot = () => JSON.stringify([typeId.value, buildDef(), connScope.value, methodString.value, typed.username, typed.keyId, typed.password, typed.token, typed.secret])
  const dirty = computed(() => !loading.value && !!typeId.value && !saved.value && snapshot() !== initial.value)

  // The fields a saved sign-in is bound to, in words, from the type's own list.
  const fingerprintHelp = computed(() => {
    const labels: Record<string, string> = { host: 'host', port: 'port', database: 'database', scope: 'scope', region: 'region' }
    const have = new Set((spec.value?.fields ?? []).filter((x) => !x.option).map((x) => x.name))
    const names = (spec.value?.fingerprintFields ?? []).filter((f) => labels[f] && have.has(f)).map((f) => labels[f])
    if (!names.length) return ''
    const list = names.length === 1 ? names[0] : `${names.slice(0, -1).join(', ')} or ${names[names.length - 1]}`
    return `If the ${list} change${names.length === 1 ? 's' : ''}, everyone is asked to sign in again to confirm.`
  })

  const secretIsSet = (name: string) => !isNew.value && !fingerprintChanged.value && name === primarySecret.value && !!status.value?.secretSet && status.value.fingerprintMatches && method.value !== 'env' && (status.value.method === methodString.value || status.value.method === method.value)

  // A saved secret that will not be used because the server changed says so where
  // it would have been shown.
  const serverChangedHelp = computed(() => (!isNew.value && (fingerprintChanged.value || (!!status.value?.secretSet && status.value.fingerprintMatches === false)) && method.value === 'keychain'
    ? "The server changed, so your saved password won't be used. Enter it again." : ''))

  // ---- shadowing ----
  const privateCopy = computed(() => !isNew.value && detail.value && detail.value.scope === 'local' && detail.value.shadows.length > 0 && !detail.value.shadowedBy)
  const shadowedBy = computed(() => detail.value?.shadowedBy ?? '')

  const failedTest = computed(() => !!result.value && !result.value.ok)
  const title = computed(() => (isNew.value ? 'New connection' : `Edit ${f.name}`))

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
      // An option's default is what leaving it blank means (the select's
      // "Default" entry), so it is not written into the YAML as noise.
      if (fd.default && !from && !fd.option) setVal(fd, fd.default)
    }
    // Settings the type cannot do without (a DuckLake's data folder) or that
    // most people must look at (Azure's sign-in, flagged prominent) live under
    // Advanced: show them open on a new connection.
    if (!from && (t?.fields ?? []).some((fd) => fd.option && (fd.required || fd.prominent))) advancedOpen.value = true
    if (id === 'motherduck') readOnly.value = false
    if (['ducklake', 'iceberg', 'delta'].includes(id) && connScope.value === 'shared') connScope.value = 'local'
    envPrefix.value = id === 'motherduck' ? 'MOTHERDUCK' : ''
    initial.value = snapshot()
    void nextTick(() => document.getElementById('f-name')?.focus())
  }

  // loadForm fills the form from the route: a blank form, a duplicate (?from=),
  // or the connection being edited.
  async function loadForm() {
    loading.value = true
    loadError.value = ''
    try {
      types.value = await App.ListConnectionTypes()
      saved.value = false
      result.value = null
      saveError.value = ''
      Object.assign(typed, emptyCreds())
      if (isNew.value) {
        const from = typeof route.query.from === 'string' ? route.query.from : ''
        if (from) {
          const d = await App.GetConnection(from)
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
        const d = await App.GetConnectionInScope({ name: editName.value, scope })
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
  }

  // validate returns what is wrong with the form, or '' when it can be saved.
  function validate(): string {
    if (!spec.value) return 'Choose a connection type first.'
    if (isNew.value && !NAME_RE.test(f.name.trim())) return 'Give it a name with lowercase letters, numbers and underscores, like prod_pg.'
    for (const fd of spec.value.fields) {
      if (fd.required && !fd.option && val(fd).trim() === '') return `${fd.label} is needed.`
    }
    for (const fd of (spec.value.fields ?? []).filter((x) => x.option)) {
      if (fd.required && !(f.options[fd.name] ?? '').trim()) return `${fd.label} is needed.`
    }
    return ''
  }

  return {
    types, loading, loadError, isNew, editName, typeId, spec, f, readOnly, connScope, origScope, method, envPrefix,
    typed, status, detail, advancedOpen, origMethod, hadKeychainSecret, saved, saveError, result, isMD, remote,
    mainFields, advFields, ownScopeOnly, insideWorkspace, exampleTail, portField, credFields, readableCreds,
    secretCreds, needsSignIn, methodString, envVars, val, setVal, buildDef, refreshDraftFingerprint,
    fingerprintChanged, bindingChanged, dirty, fingerprintHelp, secretIsSet, serverChangedHelp, privateCopy,
    shadowedBy, failedTest, title, chooseType, loadForm, validate,
  }
}

export type ConnectionForm = ReturnType<typeof useConnectionForm>
