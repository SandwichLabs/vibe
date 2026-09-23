// AppShell closeFile flush ordering (review fix, step 4a round 2 #1,
// reproduced bug): closeFile() used to call CloseFile() directly, so a view
// with a pending debounced save (registered via composables/usePendingSaves)
// only got to flush AFTER the container had already closed, and its save
// silently failed. This proves closeFile() awaits every registered flush
// BEFORE calling the CloseFile binding.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import { register, _resetForTests } from '../composables/usePendingSaves'
import { resolveConfirm } from '../composables/useConfirm'
import AppShell from './AppShell.vue'

function makeApp() {
  const calls: string[] = []
  const app = {
    GetFileInfo: vi.fn(async () => ({ path: '/tmp/acme.vibe' })),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => { calls.push('CloseFile') }),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, calls }
}

function mountShell() {
  return mount(AppShell, { global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AppShell closeFile flush ordering', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    _resetForTests()
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('awaits every registered pending-save flush before calling CloseFile', async () => {
    const { app, calls } = makeApp()

    // Simulate a view (e.g. NotebookView) with a pending debounced save.
    register(() => 'Notebook', async () => {
      calls.push('flush-start')
      await Promise.resolve()
      calls.push('flush-end')
    })

    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    expect(calls).toEqual(['flush-start', 'flush-end', 'CloseFile'])
    expect(app.CloseFile).toHaveBeenCalledTimes(1)
  })

  it('still calls CloseFile even when no flush is registered', async () => {
    const { app } = makeApp()
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    expect(app.CloseFile).toHaveBeenCalledTimes(1)
  })

  // Round 3 review fix: a failed flush must abort the close and ask before
  // proceeding — never silently close over a lost edit.
  it('a failed flush aborts the close and asks for confirmation; confirming proceeds', async () => {
    const { app } = makeApp()
    register(() => 'My Notebook', async () => { throw new Error('disk full') })

    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    // The close must not have happened yet — it's waiting on the confirm.
    expect(app.CloseFile).not.toHaveBeenCalled()

    resolveConfirm(true)
    await flushPromises()

    expect(app.CloseFile).toHaveBeenCalledTimes(1)
  })

  it('a failed flush aborts the close and asks for confirmation; cancelling keeps the file open', async () => {
    const { app } = makeApp()
    register(() => 'My Notebook', async () => { throw new Error('disk full') })

    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="close-file"]').trigger('click')
    await flushPromises()

    resolveConfirm(false)
    await flushPromises()

    expect(app.CloseFile).not.toHaveBeenCalled()
  })
})
