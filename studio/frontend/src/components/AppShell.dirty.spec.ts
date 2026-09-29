// AppShell dirty-tracking wiring (#81): the sidebar pill/Save button follow
// the shared fileDirty ref (composables/useFileDirty), the window title
// gets an "● " prefix while dirty, and the Close button asks Save/Don't
// save/Cancel only when the file actually has unsaved table changes.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import { confirmVisible, confirmAltDestructive, resolveConfirmChoice } from '../composables/useConfirm'
import { _resetForTests } from '../composables/useFileDirty'

let fileDirtyHandler: ((d: boolean) => void) | null = null
const windowSetTitle = vi.fn()
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (...args: any[]) => void) => {
    if (name === 'file:dirty') fileDirtyHandler = cb
    return () => { if (name === 'file:dirty') fileDirtyHandler = null }
  },
  EventsOff: () => {},
  WindowSetTitle: (title: string) => windowSetTitle(title),
}))

import AppShell from './AppShell.vue'

function makeApp(opts: { dirty?: boolean } = {}) {
  const app = {
    GetFileInfo: vi.fn(async () => ({ path: '/tmp/acme.vibe' })),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => {}),
    CloseFileWithoutSaving: vi.fn(async () => {}),
    IsDirty: vi.fn(async () => !!opts.dirty),
    RevertFile: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {} // hasWailsRuntime() gate
  return { app }
}

function mountShell() {
  return mount(AppShell, { global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AppShell dirty-tracking wiring', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    fileDirtyHandler = null
    _resetForTests()
    windowSetTitle.mockClear()
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('shows "Saved" and a ghost button while clean, and switches to "Unsaved changes" / primary Save on a dirty event', async () => {
    makeApp({ dirty: false })
    const wrapper = mountShell()
    await flushPromises()

    expect(wrapper.text()).toContain('Saved')
    const saveBtn = wrapper.find('[data-testid="save-file"]')
    expect(saveBtn.attributes('data-dirty')).toBe('false')

    fileDirtyHandler?.(true)
    await flushPromises()

    expect(wrapper.text()).toContain('Unsaved changes')
    expect(saveBtn.attributes('data-dirty')).toBe('true')
    expect(saveBtn.classes()).toContain('btn-primary')
  })

  it('prefixes the window title with "● " while dirty and drops the prefix once clean', async () => {
    makeApp({ dirty: false })
    mountShell()
    await flushPromises()
    expect(windowSetTitle).toHaveBeenCalledWith(expect.not.stringContaining('●'))

    fileDirtyHandler?.(true)
    await flushPromises()
    expect(windowSetTitle).toHaveBeenLastCalledWith(expect.stringContaining('● '))

    fileDirtyHandler?.(false)
    await flushPromises()
    expect(windowSetTitle).toHaveBeenLastCalledWith(expect.not.stringContaining('●'))
  })

  it('closing a clean file calls CloseFile directly, with no prompt', async () => {
    const { app } = makeApp({ dirty: false })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    expect(confirmVisible.value).toBe(false)
    expect(app.CloseFile).toHaveBeenCalledTimes(1)
    expect(app.CloseFileWithoutSaving).not.toHaveBeenCalled()
  })

  it('closing a dirty file prompts, and "Don\'t save" calls CloseFileWithoutSaving (not CloseFile)', async () => {
    const { app } = makeApp({ dirty: true })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    expect(confirmVisible.value).toBe(true)
    // Required item 11: "Don't save" reads as the destructive choice.
    expect(confirmAltDestructive.value).toBe(true)
    resolveConfirmChoice('alt')
    await flushPromises()

    expect(app.CloseFileWithoutSaving).toHaveBeenCalledTimes(1)
    expect(app.CloseFile).not.toHaveBeenCalled()
  })

  it('closing a dirty file and choosing Save calls CloseFile', async () => {
    const { app } = makeApp({ dirty: true })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    resolveConfirmChoice('confirm')
    await flushPromises()

    expect(app.CloseFile).toHaveBeenCalledTimes(1)
    expect(app.CloseFileWithoutSaving).not.toHaveBeenCalled()
  })

  it('closing a dirty file and choosing Cancel calls neither', async () => {
    const { app } = makeApp({ dirty: true })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    resolveConfirmChoice('cancel')
    await flushPromises()

    expect(app.CloseFile).not.toHaveBeenCalled()
    expect(app.CloseFileWithoutSaving).not.toHaveBeenCalled()
  })

  it('hides Revert to last save while clean and shows it once dirty', async () => {
    makeApp({ dirty: false })
    const wrapper = mountShell()
    await flushPromises()

    expect(wrapper.find('[data-testid="revert-file"]').exists()).toBe(false)

    fileDirtyHandler?.(true)
    await flushPromises()

    expect(wrapper.find('[data-testid="revert-file"]').exists()).toBe(true)
  })

  it('clicking Revert then Cancel in the confirm does not call RevertFile', async () => {
    const { app } = makeApp({ dirty: true })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="revert-file"]').trigger('click')
    await flushPromises()

    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('cancel')
    await flushPromises()

    expect(app.RevertFile).not.toHaveBeenCalled()
  })

  it('clicking Revert then "Discard changes" calls RevertFile once', async () => {
    const { app } = makeApp({ dirty: true })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="revert-file"]').trigger('click')
    await flushPromises()

    resolveConfirmChoice('confirm')
    await flushPromises()

    expect(app.RevertFile).toHaveBeenCalledTimes(1)
  })

  it('shows a "Revert failed" toast when RevertFile rejects', async () => {
    const { app } = makeApp({ dirty: true })
    app.RevertFile = vi.fn(async () => { throw new Error('boom') })
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="revert-file"]').trigger('click')
    await flushPromises()

    resolveConfirmChoice('confirm')
    await flushPromises()

    expect(wrapper.text()).toContain('Revert failed')
  })
})
