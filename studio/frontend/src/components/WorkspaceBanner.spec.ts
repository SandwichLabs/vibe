type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import WorkspaceBanner from './WorkspaceBanner.vue'
import TakeOverDialog from './TakeOverDialog.vue'
import { workspaceInfo, takeOverOpen, _resetForTests } from '../composables/useWorkspace'

const base = { root: '/w/a', name: 'Acme Analytics', sessionId: 's', duplicateIds: [], localUntrusted: false } as unknown as WorkspaceInfo
const held = { ...base, readOnly: true, holder: { app: 'vibe mcp serve', pid: 4182, servesMcp: false, text: 'vibe mcp serve (pid 4182)' } } as WorkspaceInfo

function makeApp(over: MockApp = {}) {
  const app: MockApp = {
    TakeOverWorkspace: vi.fn(async () => ({ status: 'opened', path: '/w/a', info: { ...base, readOnly: false } })),
    RegenerateWorkspaceID: vi.fn(async () => ({ ...base, duplicateIds: [] })),
    TrustLocalSettings: vi.fn(async () => ({ ...base, localUntrusted: false })),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

describe('WorkspaceBanner (read-only)', () => {
  beforeEach(() => { _resetForTests(); document.body.innerHTML = '' })

  it('says who holds the workspace, in plain words, and offers Take over and Keep read-only', async () => {
    makeApp()
    workspaceInfo.value = held
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    const b = w.find('[data-testid="readonly-banner"]')
    expect(b.text()).toContain('Opened read-only.')
    expect(b.text()).toContain('vibe mcp serve (pid 4182) is already running this workspace')
    expect(b.text()).toContain('Notebooks, queries and skills are plain files and stay editable')
    await b.findAll('button').find((x) => x.text() === 'Keep read-only')!.trigger('click')
    expect(w.find('[data-testid="readonly-banner"]').exists()).toBe(false)
  })

  it('does not show when the workspace is writable', () => {
    makeApp()
    workspaceInfo.value = base
    expect(mount(WorkspaceBanner, { global: { stubs: { Icon: true } } }).find('[data-testid="readonly-banner"]').exists()).toBe(false)
  })

  it('Take over… opens the confirm dialog with the #112 copy; confirming takes over', async () => {
    const app = makeApp()
    workspaceInfo.value = held
    const banner = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    const dlg = mount(TakeOverDialog, { attachTo: document.body, global: { stubs: { Icon: true } } })
    await banner.find('[data-testid="take-over"]').trigger('click')
    await flushPromises()
    expect(takeOverOpen.value).toBe(true)
    const d = dlg.find('[role="alertdialog"]')
    expect(d.text()).toContain('Take over Acme Analytics?')
    expect(d.text()).toContain('vibe mcp serve (pid 4182)')
    expect(d.text()).toContain('will need to reconnect, and will then go through Vibe Studio')
    expect(d.text()).toContain('any change Claude makes needs your approval in Vibe Studio')
    await dlg.find('[data-testid="takeover-confirm"]').trigger('click')
    await flushPromises()
    expect(app.TakeOverWorkspace).toHaveBeenCalled()
    expect(takeOverOpen.value).toBe(false)
    expect(workspaceInfo.value?.readOnly).toBe(false)
    dlg.unmount()
  })

  it('a failed take-over stays in the dialog and says why', async () => {
    makeApp({ TakeOverWorkspace: vi.fn(async () => { throw new Error('The other program didn\'t let go in time.') }) })
    workspaceInfo.value = held
    const dlg = mount(TakeOverDialog, { attachTo: document.body, global: { stubs: { Icon: true } } })
    takeOverOpen.value = true
    await flushPromises()
    await dlg.find('[data-testid="takeover-confirm"]').trigger('click')
    await flushPromises()
    expect(dlg.find('[data-testid="takeover-error"]').text()).toContain("didn't let go in time")
    expect(takeOverOpen.value).toBe(true)
    dlg.unmount()
  })

  it('duplicate id: offers Regenerate id', async () => {
    const app = makeApp()
    workspaceInfo.value = { ...base, duplicateIds: ['/orig/acme'] } as WorkspaceInfo
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="dup-banner"]').text()).toContain('/orig/acme')
    await w.find('[data-testid="regenerate-id"]').trigger('click')
    await flushPromises()
    expect(app.RegenerateWorkspaceID).toHaveBeenCalled()
    expect(w.find('[data-testid="dup-banner"]').exists()).toBe(false)
  })

  it('untrusted local settings: re-confirm prompt', async () => {
    const app = makeApp()
    workspaceInfo.value = { ...base, localUntrusted: true } as WorkspaceInfo
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="untrusted-banner"]').text()).toContain("isn't the one your saved settings were made for")
    await w.find('[data-testid="trust-local"]').trigger('click')
    await flushPromises()
    expect(app.TrustLocalSettings).toHaveBeenCalled()
    expect(w.find('[data-testid="untrusted-banner"]').exists()).toBe(false)
  })
})

describe('WorkspaceBanner relaunch prompt (#117, D10)', () => {
  beforeEach(() => { _resetForTests(); document.body.innerHTML = '' })

  it('asks to reopen the workspace when MotherDuck needs a fresh session, and reopening is a close-and-open of the same folder', async () => {
    const { relaunch } = await import('../composables/useConnections')
    const app = makeApp({ OpenWorkspace: vi.fn(async () => ({ status: 'opened', path: '/w/a', info: base })), IsWorkspaceOpen: vi.fn(async () => true), ListConnections: vi.fn(async () => ({ connections: [], relaunchRequired: false })) })
    workspaceInfo.value = base
    relaunch.value = { required: true, message: 'The MotherDuck extension is installed now. Reopen the workspace so it can sign in.' }
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    const b = w.find('[data-testid="relaunch-banner"]')
    expect(b.text()).toContain('Reopen this workspace to finish connecting.')
    expect(b.text()).toContain('The MotherDuck extension is installed now.')
    await w.find('[data-testid="relaunch"]').trigger('click')
    await flushPromises()
    expect(app.OpenWorkspace).toHaveBeenCalledWith('/w/a')
    relaunch.value = { required: false, message: '' }
  })
})

describe('WorkspaceBanner (.env, #168)', () => {
  const undecided: DotEnvStatus = {
    state: 'undecided', path: '/w/a/.env', names: ['MOTHERDUCK_TOKEN', 'OTHER'], trackedByGit: false, loaded: false,
    satisfies: [{ connection: 'md', signIn: 'env:MOTHERDUCK', variables: ['MOTHERDUCK_TOKEN'] }],
  }
  beforeEach(async () => { _resetForTests(); (await import('../composables/useDotEnv'))._resetDotEnvForTests(); document.body.innerHTML = '' })

  it('lists variable names and the connections they would sign in, never values', async () => {
    const app = makeApp({ GetDotEnvStatus: vi.fn(async () => undecided) })
    workspaceInfo.value = base
    const { dotenv } = await import('../composables/useDotEnv')
    dotenv.value = undecided
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    const b = w.find('[data-testid="dotenv-banner"]')
    expect(b.exists()).toBe(true)
    expect(b.find('[data-testid="dotenv-names"]').text()).toContain('MOTHERDUCK_TOKEN')
    expect(b.find('[data-testid="dotenv-feeds"]').text()).toContain('md')
    expect(b.find('[data-testid="dotenv-tracked"]').exists()).toBe(false)
    expect(app.GetDotEnvStatus).toBeDefined()
  })

  it('warns when git tracks the file, and Not now hides the prompt', async () => {
    makeApp()
    workspaceInfo.value = base
    const { dotenv } = await import('../composables/useDotEnv')
    dotenv.value = { ...undecided, trackedByGit: true }
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="dotenv-tracked"]').text()).toContain('rotate the credential')
    await w.find('[data-testid="dotenv-notnow"]').trigger('click')
    expect(w.find('[data-testid="dotenv-banner"]').exists()).toBe(false)
  })

  it('Allow calls the binding and the prompt goes away', async () => {
    const allowed: DotEnvStatus = { ...undecided, state: 'allowed', loaded: true }
    const app = makeApp({
      AllowDotEnv: vi.fn(async () => allowed),
      ListConnections: vi.fn(async () => ({ connections: [], relaunchRequired: false, relaunchMessage: '' })),
    })
    workspaceInfo.value = base
    const { dotenv } = await import('../composables/useDotEnv')
    dotenv.value = { ...undecided, trackedByGit: false }
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="dotenv-allow"]').trigger('click')
    await flushPromises()
    expect(app.AllowDotEnv).toHaveBeenCalledTimes(1)
    expect(w.find('[data-testid="dotenv-banner"]').exists()).toBe(false)
  })

  it('a changed file asks again', async () => {
    makeApp()
    workspaceInfo.value = base
    const { dotenv } = await import('../composables/useDotEnv')
    dotenv.value = { ...undecided, state: 'changed' }
    const w = mount(WorkspaceBanner, { global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="dotenv-banner"]').text()).toContain('changed since you allowed it')
  })
})
