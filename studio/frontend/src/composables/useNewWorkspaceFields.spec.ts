// useNewWorkspaceFields — the fields shared by New workspace and Import .vibe
// into a new workspace (F6).
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { useNewWorkspaceFields } from './useNewWorkspaceFields'

function setApp(app: Record<string, any>) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useNewWorkspaceFields', () => {
  beforeEach(() => { localStorage.clear() })

  it('slug is the folder the name becomes, the same one the backend makes', () => {
    setApp({})
    const name = ref('  Acme Analytics! ')
    const f = useNewWorkspaceFields(name)
    expect(f.slug.value).toBe('acme-analytics')
    name.value = '***'
    expect(f.slug.value).toBe('workspace')
  })

  it('reset restores the last location and turns git on only when git exists', async () => {
    setApp({ CheckGitAvailable: vi.fn(async () => true) })
    localStorage.setItem('vibe.newWorkspace.parentDir', '/home/me/work')
    const f = useNewWorkspaceFields(ref(''))
    await f.reset()
    expect(f.parentDir.value).toBe('/home/me/work')
    expect(f.git.value).toBe(true)

    setApp({ CheckGitAvailable: vi.fn(async () => { throw new Error('no git') }) })
    await f.reset()
    expect(f.gitAvailable.value).toBe(false)
    expect(f.git.value).toBe(false)
  })

  it('remember keeps the trimmed location for next time', () => {
    setApp({})
    const f = useNewWorkspaceFields(ref('x'))
    f.parentDir.value = ' /srv/ws '
    f.remember()
    expect(localStorage.getItem('vibe.newWorkspace.parentDir')).toBe('/srv/ws')
  })

  it('choose sets the picked folder, ignores a cancel, and reports a failure', async () => {
    const pick = vi.fn()
    setApp({ ChooseFolder: pick })
    const f = useNewWorkspaceFields(ref('x'))
    const onError = vi.fn()
    pick.mockResolvedValueOnce('/picked')
    await f.choose(onError)
    expect(f.parentDir.value).toBe('/picked')
    pick.mockResolvedValueOnce('')
    await f.choose(onError)
    expect(f.parentDir.value).toBe('/picked')
    pick.mockRejectedValueOnce(new Error('failed to open the folder dialog: nope'))
    await f.choose(onError)
    expect(onError).toHaveBeenCalledWith('nope')
  })
})
