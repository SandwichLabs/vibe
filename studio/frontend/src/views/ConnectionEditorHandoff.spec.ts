// The lake -> storage -> lake hand-off under a REAL router and the real leave
// guards (the mocked specs hid that /connections/new and /connections/:name are
// separate route records, so the leave guard runs between them).
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { defineComponent } from 'vue'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import ConnectionEditorView from './ConnectionEditorView.vue'
import { _resetConnectionsForTests } from '../composables/useConnections'
import { _resetForTests as resetWorkspace, workspaceInfo } from '../composables/useWorkspace'
import { confirmVisible, resolveConfirmChoice } from '../composables/useConfirm'
import { currentDraft, clearDraft } from '../composables/useConnectionDraft'

const field = (o: Partial<ConnField>): ConnField => ({ name: 'x', label: 'X', kind: 'text', required: false, secret: false, perPerson: false, ...o })
const caps = { catalog: true, writable: true, readOnlyAttach: false, remote: true, test: true }
const TYPES: ConnTypeSpec[] = [
  { id: 'ducklake', label: 'DuckLake', description: 'lake', fields: [field({ name: 'path', label: 'Catalog', kind: 'path', required: true }), field({ name: 'data_path', label: 'Data folder', required: true, option: true })], credentials: [], requiresExtension: [], capabilities: caps, fingerprintFields: [], signInMethods: [], soon: false },
  { id: 's3', label: 'Amazon S3', description: 's3', fields: [field({ name: 'scope', label: 'Scope', kind: 'url', required: true }), field({ name: 'region', label: 'Region', default: 'us-east-1' })],
    credentials: [field({ name: 'key_id', label: 'Key id', perPerson: true }), field({ name: 'secret', label: 'Secret', secret: true, perPerson: true })], requiresExtension: [], capabilities: { ...caps, catalog: false }, fingerprintFields: [], signInMethods: ['keychain'], soon: false },
]
const lakeDef = (): ConnectionDef => ({ name: 'my_lake', type: 'ducklake', path: 's3://lake/c.sqlite', options: { data_path: 's3://lake/data/' } })
const detailOf = (def: ConnectionDef): ConnectionDetail => ({
  name: def.name, scope: 'local', def, fingerprint: JSON.stringify(def), shadows: [], signIn: 'keychain',
  credential: { method: 'keychain', secretSet: false, fingerprintMatches: true, available: true },
  info: { name: def.name, type: def.type, scope: 'local', readOnly: true, default: false, status: 'ok', remote: true },
} as ConnectionDetail)

let saved: ConnectionDef
function makeApp() {
  saved = lakeDef()
  ;(window as any).go = { main: { App: {
    ListConnectionTypes: vi.fn(async () => TYPES),
    ListConnections: vi.fn(async () => ({ connections: [], relaunchRequired: false })),
    ConnectionFingerprint: vi.fn(async (d: ConnectionDef) => JSON.stringify(d)),
    GetConnectionInScope: vi.fn(async () => detailOf(saved)),
    SaveConnection: vi.fn(async (r: any) => detailOf(r.def)),
    SetConnectionCredentials: vi.fn(async () => null),
    ProbeKeychain: vi.fn(async () => ({ available: true })),
    FindStorageFor: vi.fn(async (req: FindStorageRequest) => ({ needs: req.urls.map((u): StorageNeedView => ({ url: u, suggest: { type: 's3', scope: 's3://lake/', bucket_scope: 's3://lake/', name: 'lake_s3' }, group: 1 })) })),
  } } }
}

const Away = defineComponent({ template: '<div data-testid="away">away</div>' })
async function mountAt(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: Away },
    { path: '/connections/new', name: 'connection-new', component: ConnectionEditorView },
    { path: '/connections/:name', name: 'connection-edit', component: ConnectionEditorView },
  ] })
  await router.push(path)
  const w = mount({ template: '<router-view />' }, { attachTo: document.body, global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return { router, w }
}
const el = (id: string) => document.querySelector(`[data-testid="${id}"], #${id}`) as HTMLInputElement | null
const settle = async (ms = 400) => { await new Promise((r) => setTimeout(r, ms)); await flushPromises() }
async function type(id: string, v: string) { const e = el(id)!; e.value = v; e.dispatchEvent(new Event('input')); await flushPromises() }

describe('storage hand-off with the real router and guards', () => {
  beforeEach(() => {
    _resetConnectionsForTests(); resetWorkspace(); clearDraft(); document.body.innerHTML = ''
    workspaceInfo.value = { isGit: true, root: '/w', name: 'A', sessionId: 's' } as unknown as WorkspaceInfo
    makeApp()
  })

  async function toStorage(edit: boolean) {
    const r = await mountAt('/connections/my_lake')
    if (edit) { await type('f-path', 's3://lake/edited.sqlite'); }
    await settle()
    el('storage-setup')!.click()
    await settle()
    return r
  }

  it('an unmodified saved lake opens its storage form with the hand-off note and keeps the draft', async () => {
    const { router } = await toStorage(false)
    expect(confirmVisible.value).toBe(false)
    expect(router.currentRoute.value.path).toBe('/connections/new')
    expect(el('storage-handoff')).not.toBeNull()
    expect(el('prefill-note')).toBeNull()
    expect(currentDraft.value?.editName).toBe('my_lake')
  })

  it('an edited saved lake: no prompt going out, Save comes back to it with the edit, a note that is not "Saved to", and focus on the field', async () => {
    const { router } = await toStorage(true)
    expect(confirmVisible.value).toBe(false)
    expect(el('storage-handoff')).not.toBeNull()
    await el('save-btn')!.click()
    await settle()
    expect(router.currentRoute.value.path).toBe('/connections/my_lake')
    expect(router.currentRoute.value.query.resume).toBe('1')
    expect((el('f-path'))!.value).toBe('s3://lake/edited.sqlite')
    expect(document.body.textContent).not.toContain('Saved to')
    expect(document.body.textContent).toContain('my_lake is not saved yet')
    expect(document.activeElement?.id).toBe('f-path')
    expect(currentDraft.value).toBeNull()
  })

  it('Cancel on a dirty storage form asks once, then returns with the lake edits', async () => {
    const { router } = await toStorage(true)
    await type('f-name', 'lake_s3b')
    el('handoff-back')!.click()
    await settle(50)
    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('confirm')
    await settle()
    expect(confirmVisible.value).toBe(false) // no second prompt from the leave guard
    expect(router.currentRoute.value.path).toBe('/connections/my_lake')
    expect(el('f-path')!.value).toBe('s3://lake/edited.sqlite')
  })

  it('a new lake round trip keeps its fields', async () => {
    const r = await mountAt('/connections/new')
    el('type-ducklake')!.click(); await flushPromises()
    await type('f-name', 'fresh'); await type('f-path', 's3://lake/c.sqlite'); await type('f-data_path', 's3://lake/d/')
    await settle()
    el('storage-setup')!.click(); await settle()
    expect(confirmVisible.value).toBe(false)
    await el('save-btn')!.click(); await settle()
    expect(r.router.currentRoute.value.query.resume).toBe('1')
    expect(el('f-name')!.value).toBe('fresh')
    expect(el('f-data_path')!.value).toBe('s3://lake/d/')
  })

  it('leaving a pristine storage form for elsewhere asks first, naming the lake, and keeps the draft on No', async () => {
    const { router } = await toStorage(false)
    const go = router.push('/')
    await settle(50)
    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('cancel')
    await go
    await settle()
    expect(router.currentRoute.value.path).toBe('/connections/new')
    expect(currentDraft.value).not.toBeNull()

    const go2 = router.push('/')
    await settle(50)
    resolveConfirmChoice('confirm')
    await go2
    await settle()
    expect(router.currentRoute.value.path).toBe('/')
    expect(currentDraft.value).toBeNull()
  })

  it('moving to a plain new-connection page (the rail plus, history back) from the storage form asks too', async () => {
    const { router } = await toStorage(false)
    const go = router.push('/connections/new')
    await settle(50)
    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('cancel')
    await go
    expect(router.currentRoute.value.query.return).toBe('lake-draft')
  })

  it('warns when the lake changed on disk while the person was in the storage form', async () => {
    await toStorage(false)
    saved = { ...saved, path: 's3://lake/other.sqlite' }
    await el('save-btn')!.click(); await settle()
    expect(el('changed-away')).not.toBeNull()
  })

  it('a destination query alone is not a hand-off: a dirty form still asks', async () => {
    const { router } = await mountAt('/connections/my_lake')
    await type('f-path', 's3://lake/edited.sqlite')
    for (const to of ['/connections/other?resume=1', '/?return=lake-draft']) {
      const go = router.push(to)
      await settle(50)
      expect(confirmVisible.value).toBe(true)
      resolveConfirmChoice('cancel')
      await go
      await settle()
      expect(router.currentRoute.value.path).toBe('/connections/my_lake')
    }
  })

  it('a resumed (dirty) new lake asks before the rail plus or history back replaces it', async () => {
    const r = await mountAt('/connections/new')
    el('type-ducklake')!.click(); await flushPromises()
    await type('f-name', 'fresh'); await type('f-path', 's3://lake/c.sqlite'); await type('f-data_path', 's3://lake/d/')
    await settle()
    el('storage-setup')!.click(); await settle()
    await el('save-btn')!.click(); await settle()
    expect(r.router.currentRoute.value.query.resume).toBe('1')
    const go = r.router.push('/connections/new')
    await settle(50)
    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('cancel')
    await go
    expect(el('f-name')!.value).toBe('fresh')
  })

  it('a ?scope= switch on a dirty saved connection asks, and passes when clean', async () => {
    const { router } = await mountAt('/connections/my_lake')
    let go = router.push('/connections/my_lake?scope=shared')
    await settle()
    expect(confirmVisible.value).toBe(false) // clean: no prompt
    await go
    await type('f-path', 's3://lake/edited.sqlite')
    go = router.push('/connections/my_lake?scope=local')
    await settle(50)
    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('cancel')
    await go
    expect(router.currentRoute.value.query.scope).toBe('shared')
  })
})
