// App.vue's "app:before-close" handling (#81): a clean file quits straight
// away as before; a dirty file asks Save/Don't save/Cancel first, via the
// same askSaveChanges prompt AppShell's Close button uses.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from './router/index'
import { resolveConfirmChoice } from './composables/useConfirm'
import { _resetForTests } from './composables/useFileDirty'

let beforeCloseHandler: ((payload?: { dirty?: boolean }) => void) | null = null
vi.mock('./wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (...args: any[]) => void) => {
    if (name === 'app:before-close') beforeCloseHandler = cb
    return () => { if (name === 'app:before-close') beforeCloseHandler = null }
  },
  EventsOff: () => {},
  WindowSetTitle: () => {},
}))

import App from './App.vue'

function makeApp(opts: { dirty?: boolean } = {}) {
  const app = {
    IsFileOpen: vi.fn(async () => true),
    GetInitialFile: vi.fn(async () => ''),
    GetFileInfo: vi.fn(async () => ({ path: '/tmp/acme.vibe' })),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => {}),
    CloseFileWithoutSaving: vi.fn(async () => {}),
    // closeWithPrompt (useFileDirty.ts) re-reads IsDirty() itself — this
    // must agree with the "dirty" payload the test is simulating, since
    // App.vue no longer trusts the payload alone for the prompt-or-not
    // decision (required item 5: the decision lives in one shared place).
    IsDirty: vi.fn(async () => !!opts.dirty),
    Quit: vi.fn(async () => {}),
    CancelQuit: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return { app }
}

function mountApp() {
  return mount(App, { global: { plugins: [router], stubs: { Icon: true, AgentPanel: true } } })
}

describe('App.vue app:before-close handling', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    beforeCloseHandler = null
    _resetForTests()
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('quits with no dialog when the payload is clean', async () => {
    const { app } = makeApp()
    mountApp()
    await flushPromises()

    beforeCloseHandler?.({ dirty: false })
    await flushPromises()

    expect(app.Quit).toHaveBeenCalledTimes(1)
    expect(app.CloseFile).not.toHaveBeenCalled()
    expect(app.CloseFileWithoutSaving).not.toHaveBeenCalled()
  })

  it('a dirty payload + Cancel calls CancelQuit and never Quit', async () => {
    const { app } = makeApp({ dirty: true })
    mountApp()
    await flushPromises()

    beforeCloseHandler?.({ dirty: true })
    await flushPromises()

    resolveConfirmChoice('cancel')
    await flushPromises()

    expect(app.CancelQuit).toHaveBeenCalledTimes(1)
    expect(app.Quit).not.toHaveBeenCalled()
  })

  it('a dirty payload + Don\'t save calls CloseFileWithoutSaving then Quit', async () => {
    const { app } = makeApp({ dirty: true })
    mountApp()
    await flushPromises()

    beforeCloseHandler?.({ dirty: true })
    await flushPromises()

    resolveConfirmChoice('alt')
    await flushPromises()

    expect(app.CloseFileWithoutSaving).toHaveBeenCalledTimes(1)
    expect(app.CloseFile).not.toHaveBeenCalled()
    expect(app.Quit).toHaveBeenCalledTimes(1)
  })

  it('a dirty payload + Save calls CloseFile then Quit', async () => {
    const { app } = makeApp({ dirty: true })
    mountApp()
    await flushPromises()

    beforeCloseHandler?.({ dirty: true })
    await flushPromises()

    resolveConfirmChoice('confirm')
    await flushPromises()

    expect(app.CloseFile).toHaveBeenCalledTimes(1)
    expect(app.CloseFileWithoutSaving).not.toHaveBeenCalled()
    expect(app.Quit).toHaveBeenCalledTimes(1)
  })

  it('a dirty payload + Save that rejects cancels the quit instead of proceeding', async () => {
    const { app } = makeApp({ dirty: true })
    app.CloseFile.mockRejectedValueOnce(new Error('disk full'))
    mountApp()
    await flushPromises()

    beforeCloseHandler?.({ dirty: true })
    await flushPromises()

    resolveConfirmChoice('confirm')
    await flushPromises()
    // The failure dialog (askConfirm) needs an explicit OK click.
    resolveConfirmChoice('confirm')
    await flushPromises()

    expect(app.CancelQuit).toHaveBeenCalledTimes(1)
    expect(app.Quit).not.toHaveBeenCalled()
  })
})
