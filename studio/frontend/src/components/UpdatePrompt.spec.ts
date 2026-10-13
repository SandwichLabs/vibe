// The first-run consent prompt and the "a release is available" note.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import { mount, flushPromises } from '@vue/test-utils'

const push = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('../wailsjs/runtime/runtime', () => ({ BrowserOpenURL: vi.fn(), EventsOn: () => () => {}, EventsOff: () => {} }))

import UpdatePrompt from './UpdatePrompt.vue'
import { updateState, updateNotice, resetUpdates } from '../composables/useUpdates'
import { confirmVisible } from '../composables/useConfirm'

const STATE: UpdateState = { check: '', lastChecked: '', skippedVersion: '', version: 'v0.1.1', isDev: false, needsConsent: true, managed: 'installer', updatable: true, managedNote: '', appliedVersion: '', canRestart: false }
const RESULT: UpdateResult = { current: 'v0.1.1', latest: 'v0.1.2', updateAvailable: true, skipped: false, releaseUrl: '', notes: '', managed: 'installer', updatable: true, managedNote: '', checkedAt: '' }

function makeApp(initial: Partial<UpdateState> = {}) {
  let state = { ...STATE, ...initial }
  const app = {
    GetUpdateState: vi.fn(async () => state),
    SetUpdateCheck: vi.fn(async (m: string) => { state = { ...state, check: m as UpdateState['check'], needsConsent: false } }),
    AutoCheckForUpdate: vi.fn(async () => null),
    SkipUpdate: vi.fn(async () => {}),
  }
  ;(window as any).go = { main: { App: app } }
  return app
}

let mounted: VueWrapper | null = null
const mountPrompt = () => (mounted = mount(UpdatePrompt, { attachTo: document.body, global: { stubs: { Icon: true } } }))
const tid = (w: VueWrapper, id: string) => w.find(`[data-testid="${id}"]`)

describe('UpdatePrompt', () => {
  beforeEach(() => { resetUpdates(); push.mockClear(); confirmVisible.value = false })
  afterEach(() => { mounted?.unmount(); mounted = null })

  it('shows nothing when the person already answered or this is a dev build', async () => {
    updateState.value = { ...STATE, needsConsent: false, check: 'off' }
    const w = mountPrompt()
    expect(tid(w, 'update-prompt').exists()).toBe(false)
  })

  it('asks the plain question with the two saved answers', async () => {
    const app = makeApp()
    updateState.value = { ...STATE }
    const w = mountPrompt()
    expect(tid(w, 'update-consent').text()).toContain('Check GitHub for new Vibe releases?')
    expect(tid(w, 'update-consent').text()).toContain('nothing but a plain request to github.com')
    await tid(w, 'update-no').trigger('click')
    await flushPromises()
    expect(app.SetUpdateCheck).toHaveBeenCalledWith('off')
    expect(tid(w, 'update-prompt').exists()).toBe(false)
  })

  it('Check weekly saves weekly', async () => {
    const app = makeApp()
    updateState.value = { ...STATE }
    const w = mountPrompt()
    await tid(w, 'update-weekly').trigger('click')
    await flushPromises()
    expect(app.SetUpdateCheck).toHaveBeenCalledWith('weekly')
  })

  it('never shows over a confirm dialog', async () => {
    updateState.value = { ...STATE }
    confirmVisible.value = true
    const w = mountPrompt()
    expect(tid(w, 'update-prompt').exists()).toBe(false)
    confirmVisible.value = false
    await flushPromises()
    expect(tid(w, 'update-prompt').exists()).toBe(true)
  })

  it('a found release: See what\'s new goes to Settings > About; Skip remembers it', async () => {
    const app = makeApp({ needsConsent: false, check: 'weekly' })
    updateState.value = { ...STATE, needsConsent: false, check: 'weekly' }
    updateNotice.value = RESULT
    const w = mountPrompt()
    expect(tid(w, 'update-notice').text()).toContain('v0.1.2 is available')
    await tid(w, 'update-skip').trigger('click')
    await flushPromises()
    expect(app.SkipUpdate).toHaveBeenCalledWith('v0.1.2')
    expect(tid(w, 'update-prompt').exists()).toBe(false)
  })

  it('See what\'s new routes to the About tab', async () => {
    makeApp()
    updateState.value = { ...STATE, needsConsent: false, check: 'weekly' }
    updateNotice.value = RESULT
    const w = mountPrompt()
    await tid(w, 'update-see').trigger('click')
    expect(push).toHaveBeenCalledWith({ path: '/settings', query: { tab: 'about' } })
  })
})
