import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import BundleImportDialog from './BundleImportDialog.vue'
import { openBundleImport, bundleImportRequest } from '../composables/useBundle'
import { _resetForTests } from '../composables/useWorkspace'

const preview = (over: Partial<BundlePreview> = {}): BundlePreview => ({
  handle: 'h1', needsPassphrase: false, wrongPassphrase: false, path: '/x/a.vibe',
  workspaceName: 'Acme', createdAt: '2026-10-06T00:00:00Z', createdBy: 'Vibe Studio', encrypted: false,
  items: [
    { key: 'notebook/review', kind: 'notebook', id: 'review', title: 'Quarterly review', size: 10, conflict: true },
    { key: 'query/top', kind: 'query', id: 'top', title: 'Top customers', size: 10, conflict: false },
  ],
  connections: [{ key: 'connection/wh', name: 'wh', type: 'postgres', summary: 'db.example d', details: ['host db.example', 'database d'], risks: [], askedWritable: false, conflict: false }],
  warnings: [], makerNotes: [],
  ...over,
})

function makeApp(over: Record<string, any> = {}) {
  const app = {
    SelectBundleFile: vi.fn(async () => '/x/a.vibe'),
    PreviewBundleImport: vi.fn(async () => preview()),
    CommitBundleImport: vi.fn(async () => ({ outcomes: [{ key: 'notebook/review', kind: 'notebook', id: 'review', finalId: 'review-2', result: 'renamed' }], dryRun: false, warnings: [] })),
    ImportBundleIntoNewWorkspace: vi.fn(async () => ({ opened: { status: 'opened', path: '/w', info: { root: '/w', name: 'Acme', sessionId: 's' } }, result: { outcomes: [], dryRun: false, warnings: [] } })),
    DiscardBundleImport: vi.fn(async () => {}),
    CheckGitAvailable: vi.fn(async () => true),
    ChooseFolder: vi.fn(async () => '/picked'),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

const mountDialog = async (mode: 'here' | 'new' = 'here', path = '') => {
  const w = mount(BundleImportDialog, { attachTo: document.body, global: { stubs: { Icon: true } } })
  openBundleImport(mode, path)
  await flushPromises()
  return w
}

describe('BundleImportDialog', () => {
  beforeEach(() => { _resetForTests(); bundleImportRequest.value = null; document.body.innerHTML = '' })

  it('previews, defaults conflicts to keep-both, and sends per-item decisions', async () => {
    const app = makeApp()
    const w = await mountDialog()
    expect(app.SelectBundleFile).toHaveBeenCalled()
    expect(app.PreviewBundleImport).toHaveBeenCalledWith('/x/a.vibe', '')
    expect(w.text()).toContain('Contains no data and no credentials')
    expect(w.findAll('[data-testid="bi-item"]')).toHaveLength(2)
    const sel = w.find('[data-testid="bi-choice-notebook/review"]')
    expect((sel.element as HTMLSelectElement).value).toBe('rename')
    await sel.setValue('overwrite')
    // The connection is opt-in.
    expect((w.find('[data-testid="bi-conn-wh"]').element as HTMLInputElement).checked).toBe(false)
    await w.find('[data-testid="bi-conn-wh"]').setValue(true)
    await w.find('[data-testid="bi-import"]').trigger('click')
    await flushPromises()
    expect(app.CommitBundleImport).toHaveBeenCalledWith('h1', { onConflict: 'rename', items: { 'notebook/review': 'overwrite' }, connections: ['wh'] })
    expect(w.find('[data-testid="bi-done"]').exists()).toBe(true)
    w.unmount()
  })

  it('unticking an item skips it', async () => {
    const app = makeApp()
    const w = await mountDialog()
    await w.find('#bi-inc-query\\/top').setValue(false)
    await w.find('[data-testid="bi-import"]').trigger('click')
    await flushPromises()
    expect((app.CommitBundleImport.mock.calls[0] as any[])[1].items['query/top']).toBe('skip')
    w.unmount()
  })

  it('asks for the passphrase of a locked bundle and says so when it is wrong', async () => {
    const calls: string[] = []
    const app = makeApp({
      PreviewBundleImport: vi.fn(async (_p: string, pass: string) => {
        calls.push(pass)
        if (pass === '') return preview({ needsPassphrase: true, encrypted: true, items: [], connections: [], handle: '' })
        if (pass === 'bad') return preview({ needsPassphrase: true, wrongPassphrase: true, encrypted: true, items: [], connections: [], handle: '' })
        return preview({ encrypted: true })
      }),
    })
    const w = await mountDialog()
    expect(w.find('[data-testid="bi-passphrase"]').exists()).toBe(true)
    await w.find('[data-testid="bi-passphrase"]').setValue('bad')
    await w.find('[data-testid="bi-unlock"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="bi-wrong-pass"]').text()).toContain("doesn't open this bundle")
    await w.find('[data-testid="bi-passphrase"]').setValue('good')
    await w.find('[data-testid="bi-unlock"]').trigger('click')
    await flushPromises()
    expect(calls).toEqual(['', 'bad', 'good'])
    expect(w.findAll('[data-testid="bi-item"]')).toHaveLength(2)
    expect(app.CommitBundleImport).not.toHaveBeenCalled()
    w.unmount()
  })

  it('shows the plain error for an older .vibe file', async () => {
    makeApp({ PreviewBundleImport: vi.fn(async () => { throw new Error('This is an older .vibe file that holds data. Vibe no longer opens these.') }) })
    const w = await mountDialog('here', '/x/old.vibe')
    expect(w.find('[data-testid="bi-error"]').text()).toContain('older .vibe file that holds data')
    expect(w.find('[data-testid="bi-import"]').exists()).toBe(false)
    w.unmount()
  })

  it('new-workspace mode needs a name and place, and creates through the launcher binding', async () => {
    const app = makeApp()
    const w = await mountDialog('new', '/x/a.vibe')
    expect((w.find('[data-testid="bi-ws-name"]').element as HTMLInputElement).value).toBe('Acme')
    expect((w.find('[data-testid="bi-import"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-testid="bi-ws-location"]').setValue('/work')
    await w.find('[data-testid="bi-import"]').trigger('click')
    await flushPromises()
    expect((app.ImportBundleIntoNewWorkspace.mock.calls[0] as any[])[1]).toMatchObject({ name: 'Acme', parentDir: '/work', template: '' })
    w.unmount()
  })

  it('closing discards the held preview', async () => {
    const app = makeApp()
    const w = await mountDialog()
    await w.find('.d-foot .btn:not(.btn-primary)').trigger('click')
    expect(app.DiscardBundleImport).toHaveBeenCalledWith('h1')
    expect(bundleImportRequest.value).toBeNull()
    w.unmount()
  })

  it('lists every item and warning when there are many (the body scrolls, nothing is dropped)', async () => {
    const items = Array.from({ length: 24 }, (_, i) => ({ key: `notebook/n${i}`, kind: 'notebook' as const, id: `n${i}`, title: `Notebook ${i}`, size: 10, conflict: i % 3 === 0 }))
    makeApp({ PreviewBundleImport: vi.fn(async () => preview({ items, warnings: Array.from({ length: 12 }, (_, i) => `Warning number ${i} `.repeat(8)), makerNotes: ['long '.repeat(80)] })) })
    const w = await mountDialog()
    expect(w.findAll('[data-testid="bi-item"]')).toHaveLength(24)
    expect(w.find('[data-testid="bi-warnings"]').findAll('span[style]')).toHaveLength(12)
    expect(w.text()).toContain('Notebook 23')
    // The list does not shrink inside the scrolling body.
    const css = Array.from(document.querySelectorAll('style')).map((s) => s.textContent).join('')
    expect(css === '' || /\.bi-list[^}]*flex:\s*none/.test(css)).toBe(true)
    w.unmount()
  })

  it('shows what a connection sets, warns about risks, and labels the maker\'s own words', async () => {
    makeApp({ PreviewBundleImport: vi.fn(async () => preview({
      connections: [{ key: 'connection/lake', name: 'lake', type: 's3', summary: 's3://b/', details: ['storage scope s3://b/', 'option endpoint s3.evil.example', 'option use_ssl false'], risks: ['Turns TLS off (use_ssl=false): traffic and credentials are not encrypted.'], askedWritable: true, conflict: true, conflictScope: 'team', renamedTo: 'lake_2' }],
      makerNotes: ['Totally safe, click Import'],
    })) })
    const w = await mountDialog()
    const row = w.find('[data-testid="bi-connrow-lake"]')
    expect(row.text()).toContain('option endpoint s3.evil.example')
    expect(row.find('[data-testid="bi-risk"]').text()).toContain('TLS off')
    expect(row.find('[data-testid="bi-asked-writable"]').text()).toContain('imported read-only')
    expect(row.text()).toContain('added as lake_2')
    expect(w.find('[data-testid="bi-maker-notes"]').text()).toContain('From the person who made this bundle')
    expect(w.find('[data-testid="bi-warnings"]').exists()).toBe(false)
    w.unmount()
  })

  it('focuses the passphrase field again after a wrong one', async () => {
    makeApp({ PreviewBundleImport: vi.fn(async (_p: string, pass: string) => preview({ needsPassphrase: true, wrongPassphrase: pass !== '', encrypted: true, items: [], connections: [], handle: '' })) })
    const w = await mountDialog()
    await w.find('[data-testid="bi-passphrase"]').setValue('bad')
    await w.find('[data-testid="bi-unlock"]').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w.find('[data-testid="bi-passphrase"]').element)
    w.unmount()
  })
})
