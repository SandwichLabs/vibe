import { describe, it, expect, beforeEach, vi } from 'vitest'
import { resolveConfirmChoice, confirmVisible } from './useConfirm'
import { register, _resetForTests as resetSaves } from './usePendingSaves'

const handlers: Record<string, (p?: any) => void> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p?: any) => void) => { handlers[name] = cb; return () => { delete handlers[name] } },
  EventsOff: () => {},
}))

import {
  workspaceInfo, workspaceOpen, openWorkspacePath, closeWorkspace, createWorkspace, startWorkspaceTracking,
  relativeTime, slugify, baseName, launcherNotice, _resetForTests,
} from './useWorkspace'

const INFO = { root: '/w/a', name: 'A', sessionId: 's1' } as unknown as WorkspaceInfo

function makeApp(over: Record<string, any> = {}) {
  const app = {
    OpenWorkspace: vi.fn(async (path: string) => ({ status: 'opened', path, info: { ...INFO, root: path } })),
    CreateWorkspace: vi.fn(async (req: any) => ({ status: 'opened', path: req.dir, info: { ...INFO, root: req.dir } })),
    CloseWorkspace: vi.fn(async () => {}),
    IsWorkspaceOpen: vi.fn(async () => false),
    GetWorkspaceInfo: vi.fn(async () => INFO),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}

describe('useWorkspace', () => {
  beforeEach(() => {
    _resetForTests()
    resetSaves()
    for (const k of Object.keys(handlers)) delete handlers[k]
  })

  it('opens a workspace and mirrors its info', async () => {
    const app = makeApp()
    const res = await openWorkspacePath('/w/a')
    expect(res.ok).toBe(true)
    expect(app.OpenWorkspace).toHaveBeenCalledWith('/w/a')
    expect(workspaceOpen.value).toBe(true)
    expect(workspaceInfo.value?.root).toBe('/w/a')
  })

  it('flushes pending saves BEFORE switching, in that order', async () => {
    const calls: string[] = []
    const app = makeApp({ OpenWorkspace: vi.fn(async (p: string) => { calls.push('open'); return { status: 'opened', path: p, info: INFO } }) })
    workspaceInfo.value = INFO
    register(() => 'nb', async () => { calls.push('flush') })
    await openWorkspacePath('/w/b')
    expect(calls).toEqual(['flush', 'open'])
    expect(app.OpenWorkspace).toHaveBeenCalled()
  })

  it('a failed flush asks before switching; staying leaves the workspace alone', async () => {
    const app = makeApp()
    workspaceInfo.value = INFO
    register(() => 'Weekly revenue', async () => { throw new Error('disk full') })
    const p = openWorkspacePath('/w/b')
    await vi.waitFor(() => expect(confirmVisible.value).toBe(true))
    resolveConfirmChoice('cancel')
    const res = await p
    expect(res.ok).toBe(false)
    expect(app.OpenWorkspace).not.toHaveBeenCalled()
    expect(workspaceInfo.value?.root).toBe('/w/a')
  })

  it('a folder with no vibe.yaml asks "Make this folder a workspace?" and creates it in place', async () => {
    const app = makeApp({ OpenWorkspace: vi.fn(async (path: string) => ({ status: 'not-workspace', path })) })
    const p = openWorkspacePath('/plain')
    await vi.waitFor(() => expect(confirmVisible.value).toBe(true))
    resolveConfirmChoice('confirm')
    const res = await p
    expect(res.ok).toBe(true)
    expect(app.CreateWorkspace).toHaveBeenCalledWith(expect.objectContaining({ dir: '/plain', inPlace: true }))
    expect(workspaceInfo.value?.root).toBe('/plain')
  })

  it('declining the plain-folder prompt creates nothing', async () => {
    const app = makeApp({ OpenWorkspace: vi.fn(async (path: string) => ({ status: 'not-workspace', path })) })
    const p = openWorkspacePath('/plain')
    await vi.waitFor(() => expect(confirmVisible.value).toBe(true))
    resolveConfirmChoice('cancel')
    expect((await p).ok).toBe(false)
    expect(app.CreateWorkspace).not.toHaveBeenCalled()
  })

  it('a failed open reports the error and reconciles with the backend (the old workspace is closed)', async () => {
    const app = makeApp({ OpenWorkspace: vi.fn(async () => { throw new Error('Couldn\'t open this workspace: bad yaml') }) })
    workspaceInfo.value = INFO
    const res = await openWorkspacePath('/w/bad')
    expect(res.ok).toBe(false)
    expect(res.error).toContain('bad yaml')
    expect(app.IsWorkspaceOpen).toHaveBeenCalled()
    expect(workspaceInfo.value).toBeNull()
  })

  it('createWorkspace passes the request through', async () => {
    const app = makeApp()
    const res = await createWorkspace({ name: 'N', dir: '', parentDir: '/p', git: true, template: 'contacts', inPlace: false })
    expect(res.ok).toBe(true)
    expect(app.CreateWorkspace).toHaveBeenCalledWith(expect.objectContaining({ template: 'contacts', git: true }))
  })

  it('closeWorkspace flushes then closes', async () => {
    const app = makeApp()
    workspaceInfo.value = INFO
    const res = await closeWorkspace()
    expect(res.ok).toBe(true)
    expect(app.CloseWorkspace).toHaveBeenCalled()
    expect(workspaceInfo.value).toBeNull()
  })

  it('events keep the info in sync; a switch close does not flash the launcher', () => {
    makeApp()
    startWorkspaceTracking()
    handlers['workspace:opened']({ ...INFO, name: 'First' })
    expect(workspaceInfo.value?.name).toBe('First')
    handlers['workspace:closed']({ reason: 'switch' })
    expect(workspaceInfo.value?.name).toBe('First')
    handlers['workspace:closed']({ reason: 'released' })
    expect(workspaceInfo.value).toBeNull()
    expect(launcherNotice.value).toContain('let go')
  })

  it('relativeTime, slugify and baseName', () => {
    const now = Date.parse('2026-10-06T12:00:00Z')
    expect(relativeTime('2026-10-06T10:00:00Z', now)).toBe('2 hours ago')
    expect(relativeTime('2026-10-05T11:00:00Z', now)).toBe('Yesterday')
    expect(relativeTime('2026-10-03T12:00:00Z', now)).toBe('3 days ago')
    expect(relativeTime('', now)).toBe('')
    expect(slugify('  Acme Analytics! ')).toBe('acme-analytics')
    expect(slugify('***')).toBe('workspace')
    expect(baseName('/a/b/c/')).toBe('c')
  })
})
