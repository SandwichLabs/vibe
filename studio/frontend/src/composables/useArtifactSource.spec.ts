// useArtifactSource — the Source tab's flow (#162, step 6a): edit, save with the
// one revision, warnings that save, a refusal that does not, and the conflict
// banner with its three actions over the two-file revision.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'

const handlers: Record<string, (p: any) => void> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: (n: string, f: (p: any) => void) => { handlers[n] = f; return () => {} }, EventsOff: () => {} }))
const confirm = vi.fn(async (_o?: unknown) => true)
vi.mock('./useConfirm', () => ({ askConfirm: (o: unknown) => confirm(o) }))

import { useArtifactSource } from './useArtifactSource'
import { workspaceInfo } from './useWorkspace'

type MockApp = Partial<Record<keyof AppApi, any>>

const src = (over: Partial<ArtifactSource> = {}): ArtifactSource => ({
  name: 'orders', manifest: 'title: Orders\n', view: '{}', manifest_missing: false, view_missing: false, rev: 'r1', updated_at: '', ...over,
})

function makeApp(over: MockApp = {}) {
  const app: MockApp = {
    GetArtifactSource: vi.fn(async () => src()),
    SaveArtifact: vi.fn(async () => ({ rev: 'r2', updated_at: '', warnings: [] })),
    CheckArtifact: vi.fn(async () => []),
    DeleteArtifact: vi.fn(async () => {}),
    ResolveConflict: vi.fn(async () => ({ id: 'orders', rev: 'r3', title: 'Orders' })),
    ...over,
  }
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}

function setup() {
  const hooks = { saved: vi.fn(), gone: vi.fn() }
  let api!: ReturnType<typeof useArtifactSource>
  mount(defineComponent({ setup() { api = useArtifactSource(() => 'orders', hooks); return () => h('div') } }))
  return { api, hooks }
}

describe('useArtifactSource', () => {
  beforeEach(() => {
    for (const k of Object.keys(handlers)) delete handlers[k]
    confirm.mockClear()
    confirm.mockResolvedValue(true)
  })

  it('loads the files as text and is not dirty until edited', async () => {
    makeApp()
    const { api } = setup()
    await flushPromises()
    expect(api.manifest.value).toBe('title: Orders\n')
    expect(api.rev.value).toBe('r1')
    expect(api.dirty.value).toBe(false)
    api.view.value = '{"title":"x"}'
    expect(api.dirty.value).toBe(true)
  })

  it('saves both files with the revision, keeps the new one, and lists policy warnings', async () => {
    const app = makeApp({ SaveArtifact: vi.fn(async () => ({ rev: 'r2', updated_at: '', warnings: ['query "q": writes are not allowed'] })) })
    const { api, hooks } = setup()
    await flushPromises()
    api.manifest.value = 'title: Edited\n'
    await api.save()
    expect(app.SaveArtifact).toHaveBeenCalledWith('orders', 'title: Edited\n', '{}', 'r1')
    expect(api.rev.value).toBe('r2')
    expect(api.dirty.value).toBe(false)
    expect(api.warnings.value).toEqual(['query "q": writes are not allowed'])
    expect(hooks.saved).toHaveBeenCalledOnce()
  })

  it('a structural refusal keeps the text, shows the reason and raises no banner', async () => {
    makeApp({ SaveArtifact: vi.fn(async () => { throw new Error('invalid artifact: artifact.yaml: unknown key "bogus"') }) })
    const { api, hooks } = setup()
    await flushPromises()
    api.manifest.value = 'bogus: 1\n'
    await api.save()
    expect(api.saveError.value).toContain('unknown key "bogus"')
    expect(api.conflict.value).toBeNull()
    expect(api.dirty.value).toBe(true)
    expect(hooks.saved).not.toHaveBeenCalled()
  })

  it('a stale revision raises the banner and Reload takes the disk version', async () => {
    const GetArtifactSource = vi.fn(async () => src())
    const app = makeApp({ GetArtifactSource, SaveArtifact: vi.fn(async () => { throw new Error('conflict: changed') }) })
    const { api } = setup()
    await flushPromises()
    api.view.value = '{"mine":1}'
    await api.save()
    expect(api.conflict.value).toEqual({ deleted: false })
    GetArtifactSource.mockResolvedValue(src({ view: '{"disk":1}', rev: 'r9' }))
    await api.resolveConflict('reload')
    expect(app.ResolveConflict).toHaveBeenCalledWith({ kind: 'artifact', id: 'orders', action: 'reload', rev: expect.any(String), data: { manifest: 'title: Orders\n', view: '{"mine":1}' } })
    expect(api.view.value).toBe('{"disk":1}')
    expect(api.rev.value).toBe('r9')
    expect(api.conflict.value).toBeNull()
    expect(api.dirty.value).toBe(false)
  })

  it('Keep mine sends the revision the banner was raised against and adopts the result', async () => {
    const GetArtifactSource = vi.fn(async () => src())
    const app = makeApp({ GetArtifactSource, SaveArtifact: vi.fn(async () => { throw new Error('conflict: changed') }) })
    const { api } = setup()
    await flushPromises()
    api.view.value = '{"mine":1}'
    await api.save()
    GetArtifactSource.mockResolvedValue(src({ rev: 'r7' }))
    handlers['workspace:changed-on-disk']?.({ kind: 'artifact', id: 'orders', op: 'changed' })
    await flushPromises()
    await api.resolveConflict('overwrite')
    expect((app.ResolveConflict as any).mock.calls.at(-1)[0]).toMatchObject({ action: 'overwrite', rev: 'r7' })
    expect(api.rev.value).toBe('r3')
    expect(api.dirty.value).toBe(false)
    expect(api.conflict.value).toBeNull()
  })

  it('Save as copy says where it went and keeps the disk version open', async () => {
    const GetArtifactSource = vi.fn(async () => src())
    makeApp({
      GetArtifactSource, SaveArtifact: vi.fn(async () => { throw new Error('conflict: changed') }),
      ResolveConflict: vi.fn(async () => ({ id: 'orders-copy', rev: 'c1', title: 'Orders' })),
    })
    const { api } = setup()
    await flushPromises()
    api.view.value = '{"mine":1}'
    await api.save()
    GetArtifactSource.mockResolvedValue(src({ view: '{"disk":1}', rev: 'r8' }))
    await api.resolveConflict('saveCopy')
    expect(api.notice.value).toBe('Saved your version as "orders-copy".')
    expect(api.view.value).toBe('{"disk":1}')
    expect(api.conflict.value).toBeNull()
  })

  it('an outside change follows quietly when untouched and raises the banner when edited', async () => {
    const GetArtifactSource = vi.fn(async () => src())
    makeApp({ GetArtifactSource })
    const { api } = setup()
    await flushPromises()
    GetArtifactSource.mockResolvedValue(src({ view: '{"disk":1}', rev: 'r2' }))
    handlers['workspace:changed-on-disk']({ kind: 'artifact', id: 'orders', op: 'changed' })
    await flushPromises()
    expect(api.view.value).toBe('{"disk":1}')
    expect(api.conflict.value).toBeNull()

    api.manifest.value = 'title: mine\n'
    GetArtifactSource.mockResolvedValue(src({ view: '{"disk":2}', rev: 'r3' }))
    handlers['workspace:changed-on-disk']({ kind: 'artifact', id: 'orders', op: 'changed' })
    await flushPromises()
    expect(api.conflict.value).toEqual({ deleted: false })
    expect(api.manifest.value).toBe('title: mine\n')

    handlers['workspace:changed-on-disk']({ kind: 'artifact', id: 'other', op: 'changed' })
  })

  it('ignores an event for a revision it already has, and one for another artifact', async () => {
    makeApp()
    const { api } = setup()
    await flushPromises()
    api.manifest.value = 'title: mine\n'
    handlers['workspace:changed-on-disk']({ kind: 'artifact', id: 'orders', op: 'changed' })
    handlers['workspace:changed-on-disk']({ kind: 'artifact', id: 'other', op: 'deleted' })
    await flushPromises()
    expect(api.conflict.value).toBeNull()
  })

  it('a deleted-on-disk banner offers Close it, which leaves the editor', async () => {
    makeApp()
    const { api, hooks } = setup()
    await flushPromises()
    handlers['workspace:changed-on-disk']({ kind: 'artifact', id: 'orders', op: 'deleted' })
    await flushPromises()
    expect(api.conflict.value).toEqual({ deleted: true })
    await api.resolveConflict('reload')
    expect(hooks.gone).toHaveBeenCalledOnce()
  })

  it('delete asks first, sends the revision it saw, and leaves; a refusal says so', async () => {
    const DeleteArtifact = vi.fn(async () => {})
    makeApp({ DeleteArtifact })
    const { api, hooks } = setup()
    await flushPromises()
    confirm.mockResolvedValueOnce(false)
    await api.remove()
    expect(DeleteArtifact).not.toHaveBeenCalled()
    await api.remove()
    expect(DeleteArtifact).toHaveBeenCalledWith('orders', 'r1')
    expect(hooks.gone).toHaveBeenCalledOnce()

    DeleteArtifact.mockRejectedValueOnce(new Error('conflict: changed'))
    await api.remove()
    expect(api.saveError.value).toMatch(/nothing was deleted/)
    expect(hooks.gone).toHaveBeenCalledOnce()
  })
})

describe('useArtifactSource warnings and refusals', () => {
  it('a refusal clears when the text is edited, and the policy check runs once, on first show', async () => {
    const app = makeApp({ SaveArtifact: vi.fn(async () => { throw new Error('invalid artifact: nope') }), CheckArtifact: vi.fn(async () => ['query "q": no writes']) })
    const { api } = setup()
    await flushPromises()
    expect(app.CheckArtifact).not.toHaveBeenCalled()
    await api.checkOnce(); await api.checkOnce()
    expect(app.CheckArtifact).toHaveBeenCalledTimes(1)
    expect(api.warnings.value).toEqual(['query "q": no writes'])
    api.manifest.value = 'x'
    await api.save()
    expect(api.saveError.value).toContain('nope')
    api.manifest.value = 'xy'
    await flushPromises()
    expect(api.saveError.value).toBe('')
  })
})

describe('useArtifactSource leaving with unsaved text', () => {
  it('asks before a route change, stays when declined, and leaves freely when clean', async () => {
    makeApp()
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/a', component: { render: () => h('i') } }, { path: '/b', component: { render: () => h('i') } }] })
    await router.push('/a')
    let api!: ReturnType<typeof useArtifactSource>
    mount(defineComponent({ setup() { api = useArtifactSource(() => 'orders', { saved: vi.fn(), gone: vi.fn() }); return () => h('div') } }), { global: { plugins: [router] } })
    await flushPromises()
    await router.push('/b')
    expect(router.currentRoute.value.path).toBe('/b') // clean: no question
    await router.push('/a')
    api.manifest.value = 'edited'
    confirm.mockResolvedValueOnce(false)
    await router.push('/b')
    expect(router.currentRoute.value.path).toBe('/a')
    expect(confirm).toHaveBeenCalled()
    await router.push('/b')
    expect(router.currentRoute.value.path).toBe('/b') // confirmed
  })

  it('lets a workspace switch through without a second question', async () => {
    makeApp()
    workspaceInfo.value = { sessionId: 's1' } as WorkspaceInfo
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/a', component: { render: () => h('i') } }, { path: '/', component: { render: () => h('i') } }] })
    await router.push('/a')
    let api!: ReturnType<typeof useArtifactSource>
    mount(defineComponent({ setup() { api = useArtifactSource(() => 'orders', { saved: vi.fn(), gone: vi.fn() }); return () => h('div') } }), { global: { plugins: [router] } })
    await flushPromises()
    api.manifest.value = 'edited'
    confirm.mockClear()
    workspaceInfo.value = { sessionId: 's2' } as WorkspaceInfo // the switch already asked
    await router.push('/')
    expect(router.currentRoute.value.path).toBe('/')
    expect(confirm).not.toHaveBeenCalled()
    workspaceInfo.value = null
  })
})
