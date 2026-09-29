// SettingsView: a Save that happens
// elsewhere (the sidebar, Ctrl+S, an agent write) must refresh the "All
// metadata" list (updated_at) WITHOUT wiping text typed into Title/
// Description that hasn't gone through this view's own "Save settings" yet.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { fileDirty } from '../composables/useFileDirty'
import SettingsView from './SettingsView.vue'

function makeApp(meta: Record<string, string>) {
  const app = {
    GetAllMeta: vi.fn(async () => ({ ...meta })),
    SetMeta: vi.fn(async () => {}),
    SaveFile: vi.fn(async () => {}),
    GetMCPStatus: vi.fn(async () => ({ running: false })),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

function mountView() {
  return mount(SettingsView, { global: { stubs: { Icon: true, AppearanceSettings: true, AgentSettings: true } } })
}

describe('SettingsView', () => {
  beforeEach(() => {
    fileDirty.value = false
  })

  it('a Save elsewhere refreshes updated_at without wiping an unsaved Title edit', async () => {
    const app = makeApp({ title: 'Orig Title', description: 'Orig desc', updated_at: 't1' })
    const wrapper = mountView()
    await flushPromises()

    const titleInput = wrapper.find('#settings-title')
    expect((titleInput.element as HTMLInputElement).value).toBe('Orig Title')

    await titleInput.setValue('Typed but not yet saved')

    // Simulate a Save that happened elsewhere (sidebar/Ctrl+S/agent write):
    // updated_at moves, but the person's own Title edit hasn't gone through
    // "Save settings" yet.
    app.GetAllMeta.mockResolvedValue({ title: 'Orig Title', description: 'Orig desc', updated_at: 't2' })
    fileDirty.value = true
    await flushPromises()
    fileDirty.value = false
    await flushPromises()

    expect(wrapper.text()).toContain('t2')
    expect((wrapper.find('#settings-title').element as HTMLInputElement).value).toBe('Typed but not yet saved')
  })

  it('renders updated_at in the All metadata list (non-blocking item 12)', async () => {
    makeApp({ title: 'T', description: '', updated_at: '2026-01-01T00:00:00Z' })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('updated_at')
    expect(wrapper.text()).toContain('2026-01-01T00:00:00Z')
  })
})
