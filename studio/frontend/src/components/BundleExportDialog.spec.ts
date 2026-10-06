import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import BundleExportDialog from './BundleExportDialog.vue'
import { bundleExportOpen } from '../composables/useBundle'

const content = (): BundleContent => ({
  workspaceName: 'Acme',
  notebooks: [{ id: 'review', title: 'Quarterly review' }],
  queries: [{ id: 'top', title: 'Top customers' }],
  skills: [{ id: 'revenue', title: 'How revenue is counted' }],
  connections: [
    { name: 'wh', type: 'postgres', summary: 'db.example d', scope: 'team', portable: true },
    { name: 'mine', type: 'sqlite', summary: '/home/me/x.sqlite', scope: 'this computer', portable: false, reason: 'It points at a file path on this computer.' },
  ],
})

function makeApp(over: Record<string, any> = {}) {
  const app = {
    GetBundleContent: vi.fn(async () => content()),
    ExportBundle: vi.fn(async (req: BundleExportRequest) => ({ path: '/out/a.vibe', cancelled: false, encrypted: !!req.passphrase, notebooks: 1, queries: 1, skills: 1, connections: 0, bytes: 2048, warnings: [] })),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

const mountDialog = async () => {
  const w = mount(BundleExportDialog, { attachTo: document.body, global: { stubs: { Icon: true } } })
  bundleExportOpen.value = true
  await flushPromises()
  return w
}

describe('BundleExportDialog', () => {
  beforeEach(() => { bundleExportOpen.value = false; document.body.innerHTML = '' })

  it('says the bundle has no data and no credentials, and starts with content ticked and connections off', async () => {
    makeApp()
    const w = await mountDialog()
    expect(w.find('[data-testid="bundle-no-data-note"]').text()).toContain('no data and no credentials')
    expect((w.find('[data-testid="be-nb-review"]').element as HTMLInputElement).checked).toBe(true)
    expect((w.find('[data-testid="be-conn-wh"]').element as HTMLInputElement).checked).toBe(false)
    expect(w.text()).toContain('It points at a file path on this computer.')
    w.unmount()
  })

  it('exports the ticked items, unencrypted by default', async () => {
    const app = makeApp()
    const w = await mountDialog()
    await w.find('[data-testid="be-qs-top"]').setValue(false)
    await w.find('[data-testid="be-conn-wh"]').setValue(true)
    await w.find('[data-testid="be-export"]').trigger('click')
    await flushPromises()
    expect(app.ExportBundle).toHaveBeenCalledWith({
      path: '',
      selection: { all: false, notebooks: ['review'], queries: [], skills: ['revenue'], sharedConnections: false, teamConnections: ['wh'], localConnections: [] },
      passphrase: '',
    })
    expect(w.find('[data-testid="bundle-export-done"]').text()).toContain('Saved 1 notebook')
    w.unmount()
  })

  it('encryption needs the passphrase twice, and matching', async () => {
    const app = makeApp()
    const w = await mountDialog()
    await w.find('[data-testid="be-encrypt"]').setValue(true)
    expect((w.find('[data-testid="be-export"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-testid="be-pass1"]').setValue('one')
    await w.find('[data-testid="be-pass2"]').setValue('two')
    expect(w.text()).toContain("The two don't match")
    expect((w.find('[data-testid="be-export"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-testid="be-pass2"]').setValue('one')
    await w.find('[data-testid="be-export"]').trigger('click')
    await flushPromises()
    expect((app.ExportBundle.mock.calls[0] as any[])[0].passphrase).toBe('one')
    expect(w.text()).toContain('locked with a passphrase')
    w.unmount()
  })

  it('a cancelled save dialog leaves the dialog open', async () => {
    makeApp({ ExportBundle: vi.fn(async () => ({ path: '', cancelled: true, encrypted: false, notebooks: 0, queries: 0, skills: 0, connections: 0, bytes: 0, warnings: [] })) })
    const w = await mountDialog()
    await w.find('[data-testid="be-export"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="bundle-export-done"]').exists()).toBe(false)
    expect(bundleExportOpen.value).toBe(true)
    w.unmount()
  })
})
