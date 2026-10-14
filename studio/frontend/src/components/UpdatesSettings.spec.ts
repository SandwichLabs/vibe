// Settings > About > Updates: toggle, Check now, the four result shapes, confirmed install, restart.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import { mount, flushPromises } from '@vue/test-utils'

const openURL = vi.hoisted(() => vi.fn())
vi.mock('../wailsjs/runtime/runtime', () => ({ BrowserOpenURL: openURL, EventsOn: () => () => {}, EventsOff: () => {} }))

import UpdatesSettings from './UpdatesSettings.vue'
import { resetUpdates } from '../composables/useUpdates'
import { resolveConfirmChoice } from '../composables/useConfirm'

const STATE: UpdateState = { check: 'off', lastChecked: '', skippedVersion: '', version: 'v0.1.1', isDev: false, needsConsent: false, managed: 'installer', updatable: true, managedNote: '', appliedVersion: '', canRestart: false }
const RESULT: UpdateResult = { current: 'v0.1.1', latest: 'v0.1.2', updateAvailable: true, skipped: false, releaseUrl: 'https://github.com/SandwichLabs/vibe/releases/tag/v0.1.2', notes: '## Fixes\n<img src=x onerror=alert(1)>', managed: 'installer', updatable: true, managedNote: '', checkedAt: '2026-10-13T00:00:00Z' }

function makeApp(result: Partial<UpdateResult> = {}) {
  let state = { ...STATE }
  const app = {
    GetUpdateState: vi.fn(async () => state),
    SetUpdateCheck: vi.fn(async (m: string) => { state = { ...state, check: m as UpdateState['check'] } }),
    CheckForUpdate: vi.fn(async () => { state = { ...state, lastChecked: '2026-10-13T00:00:00Z' }; return { ...RESULT, ...result } }),
    SkipUpdate: vi.fn(async () => {}),
    ApplyUpdate: vi.fn(async () => { state = { ...state, appliedVersion: '0.1.2', canRestart: true }; return { from: '0.1.1', to: '0.1.2', output: '0.1.1 -> 0.1.2', studio: true, restart: '/x/vibe-studio' } }),
    RestartAfterUpdate: vi.fn(async () => {}),
  }
  ;(window as any).go = { main: { App: app } }
  return app
}

let mounted: VueWrapper | null = null
async function mountBlock() {
  const w = mount(UpdatesSettings, { attachTo: document.body, global: { stubs: { Icon: true } } })
  mounted = w
  await flushPromises()
  return w
}
const tid = (w: VueWrapper, id: string) => w.find(`[data-testid="${id}"]`)

describe('UpdatesSettings', () => {
  beforeEach(() => { resetUpdates(); openURL.mockClear() })
  afterEach(() => { mounted?.unmount(); mounted = null })

  it('the toggle saves weekly/off', async () => {
    const app = makeApp()
    const w = await mountBlock()
    expect((tid(w, 'updates-weekly').element as HTMLInputElement).checked).toBe(false)
    await tid(w, 'updates-weekly').setValue(true)
    await flushPromises()
    expect(app.SetUpdateCheck).toHaveBeenLastCalledWith('weekly')
    expect((tid(w, 'updates-weekly').element as HTMLInputElement).checked).toBe(true)
  })

  it('Check now with nothing newer says up to date', async () => {
    makeApp({ updateAvailable: false, latest: 'v0.1.1', notes: '' })
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    expect(tid(w, 'updates-uptodate').text()).toContain('v0.1.1 is up to date')
    expect(tid(w, 'updates-last').exists()).toBe(true)
  })

  it('a newer release shows its notes as plain text, never as HTML', async () => {
    makeApp()
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    expect(tid(w, 'updates-available').text()).toContain('v0.1.2 is available')
    const notes = tid(w, 'updates-notes')
    expect(notes.text()).toContain('<img src=x onerror=alert(1)>')
    expect(notes.find('img').exists()).toBe(false)
    expect(w.find('img').exists()).toBe(false)
  })

  it('an external install is notify-only: no Update button, the explanation instead', async () => {
    makeApp({ updatable: false, managed: 'external', managedNote: 'Installed outside the Vibe installer; update it the way you installed it.' })
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    expect(tid(w, 'updates-install').exists()).toBe(false)
    expect(tid(w, 'updates-external').text()).toContain('the way you installed it')
    expect(tid(w, 'updates-release').exists()).toBe(true)
  })

  it('a failed check shows the error', async () => {
    const app = makeApp()
    app.CheckForUpdate.mockRejectedValue(new Error('offline'))
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    expect(tid(w, 'updates-error').text()).toContain('offline')
  })

  it('a skipped version is labelled and Update is still offered', async () => {
    makeApp({ skipped: true })
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    expect(tid(w, 'updates-skipped').exists()).toBe(true)
    expect(tid(w, 'updates-install').exists()).toBe(true)
    expect(tid(w, 'updates-skip').exists()).toBe(false)
  })

  it('Skip this version calls the binding; the release page opens in the system browser', async () => {
    const app = makeApp()
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    await tid(w, 'updates-skip').trigger('click')
    await flushPromises()
    expect(app.SkipUpdate).toHaveBeenCalledWith('v0.1.2')
    await tid(w, 'updates-release').trigger('click')
    expect(openURL).toHaveBeenCalledWith(RESULT.releaseUrl)
  })

  it('Update asks to confirm, installs, then offers Restart now only', async () => {
    const app = makeApp()
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    await tid(w, 'updates-install').trigger('click')
    await flushPromises()
    expect(app.ApplyUpdate).not.toHaveBeenCalled()
    resolveConfirmChoice('confirm')
    await flushPromises()
    expect(app.ApplyUpdate).toHaveBeenCalledWith('v0.1.2')
    expect(tid(w, 'updates-applied').text()).toContain('Updated to v0.1.2')
    await tid(w, 'updates-restart').trigger('click')
    expect(app.RestartAfterUpdate).toHaveBeenCalled()
    // A later Check now (or a reload) never re-offers what was just installed; only Restart now remains.
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    expect(tid(w, 'updates-applied').exists()).toBe(true)
    expect(tid(w, 'updates-install').exists()).toBe(false)
    expect(tid(w, 'updates-later').exists()).toBe(false)
  })

  it('declining the confirmation installs nothing', async () => {
    const app = makeApp()
    const w = await mountBlock()
    await tid(w, 'updates-check').trigger('click')
    await flushPromises()
    await tid(w, 'updates-install').trigger('click')
    await flushPromises()
    resolveConfirmChoice('cancel')
    await flushPromises()
    expect(app.ApplyUpdate).not.toHaveBeenCalled()
  })
})

describe('UpdatesSettings after a reload', () => {
  beforeEach(() => resetUpdates())
  afterEach(() => { mounted?.unmount(); mounted = null })

  it('a session that already installed shows Restart now, not the update again', async () => {
    const app = makeApp()
    app.GetUpdateState.mockResolvedValue({ ...STATE, appliedVersion: '0.1.2', canRestart: true })
    const w = await mountBlock()
    expect(tid(w, 'updates-applied').text()).toContain('Updated to v0.1.2')
    expect(tid(w, 'updates-restart').exists()).toBe(true)
  })

  it('a failed save of the choice says so, not "could not check"', async () => {
    const app = makeApp()
    app.SetUpdateCheck.mockRejectedValue(new Error('disk full'))
    const w = await mountBlock()
    await tid(w, 'updates-weekly').setValue(true)
    await flushPromises()
    expect(tid(w, 'updates-settings-error').text()).toContain('disk full')
    expect(tid(w, 'updates-error').exists()).toBe(false)
  })
})
