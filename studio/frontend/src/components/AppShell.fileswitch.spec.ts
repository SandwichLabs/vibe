// AppShell "Open file" label staleness (#68 follow-up, reviewer-caught).
//
// The sidebar's "Open file" label was read once at mount and never
// refreshed. If the open file changes underneath an already-mounted
// AppShell — e.g. a direct OpenFile/CreateFile/CreateFromTemplate binding
// call that bypasses UnlockView's own remount-on-file-opened flow, the same
// class of call the SaveNotebook session-token fix guards the data path
// against — the label kept showing the PREVIOUS file's name. AppShell now
// listens for the "file:opened" Wails event and re-fetches GetFileInfo.
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

// Capture the "file:opened" handler so this test can fire it directly,
// mirroring TableBrowserView.spec.ts's approach to the same wailsjs runtime
// module.
let fileOpenedHandler: (() => void) | null = null
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: () => void) => {
    if (name === 'file:opened') fileOpenedHandler = cb
    return () => { fileOpenedHandler = null }
  },
  EventsOff: () => { fileOpenedHandler = null },
  WindowSetTitle: () => {},
}))

import AppShell from './AppShell.vue'

function makeApp(initialPath: string) {
  let path = initialPath
  const app = {
    GetFileInfo: vi.fn(async () => ({ path, session_id: path })),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => {}),
    IsDirty: vi.fn(async () => false),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {} // hasWailsRuntime() gate
  return { app, setPath: (p: string) => { path = p } }
}

function mountShell() {
  return mount(AppShell, { global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AppShell "Open file" label', () => {
  it('shows the initially open file on mount', async () => {
    makeApp('/tmp/first.vibe')
    const wrapper = mountShell()
    await flushPromises()
    expect(wrapper.text()).toContain('first.vibe')
  })

  it('refreshes when a "file:opened" event fires for a DIFFERENT file, without remounting', async () => {
    const { setPath } = makeApp('/tmp/first.vibe')
    const wrapper = mountShell()
    await flushPromises()
    expect(wrapper.text()).toContain('first.vibe')

    // Simulate the file changing underneath this already-mounted shell (a
    // direct binding call, or any path that swaps the container without
    // AppShell itself being torn down and remounted).
    setPath('/tmp/second.vibe')
    expect(fileOpenedHandler).not.toBeNull()
    fileOpenedHandler!()
    await flushPromises()

    expect(wrapper.text()).toContain('second.vibe')
    expect(wrapper.text()).not.toContain('first.vibe')
  })
})
