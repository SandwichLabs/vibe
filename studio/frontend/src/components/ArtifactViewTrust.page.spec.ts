// ArtifactViewTrust for an HTML page (#162): the files are shown, nothing is hosted until
// the person allows, an allowed static page goes to the script-less frame, and a scripted
// one is handed to its page window (ScriptedPage), never to a frame.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import ArtifactViewTrust from './ArtifactViewTrust.vue'

const status = (over: Partial<ArtifactViewStatus> = {}): ArtifactViewStatus => ({
  name: 'pg', kind: 'html', state: 'undecided', hash: 'b'.repeat(64), page_kind: 'static',
  manifest: 'version: 1\ntitle: Pg\nview: html\n', source: '', queries: ['big'],
  files: [{ path: 'index.html.tmpl', role: 'entry', size: 12, text: '<p>hi</p>' }], ...over,
})

function makeApp(initial: ArtifactViewStatus) {
  let cur = initial
  const app: MockApp = {
    ArtifactViewState: vi.fn(async () => cur),
    AllowArtifactView: vi.fn(async () => (cur = status({ ...cur, state: 'allowed' }))),
    DenyArtifactView: vi.fn(async () => (cur = status({ ...cur, state: 'denied' }))),
    RevokeArtifactView: vi.fn(async () => (cur = status({ ...cur, state: 'undecided' }))),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

const detail = { name: 'pg', title: 'Pg', params: [], queries: [], view_kind: 'html', defaults: {}, rev: 'r1', updated_at: '' } as unknown as ArtifactDetail

async function mountTrust() {
  const w = mount(ArtifactViewTrust, {
    props: { name: 'pg', detail, values: {}, refresh: 0 },
    global: { stubs: { Icon: true, ScriptedPage: { template: '<div data-testid="scripted-page-stub" />' }, StaticPage: { template: '<div data-testid="static-page-stub" />' }, CustomView: { template: '<div data-testid="custom-view-stub" />' } } },
  })
  await flushPromises()
  return w
}

describe('ArtifactViewTrust, HTML page', () => {
  it('lists the files and hosts nothing until the person allows', async () => {
    makeApp(status())
    const w = await mountTrust()
    expect(w.text()).toContain('HTML page')
    expect(w.get('[data-testid="view-trust-line"]').text()).toContain('It runs no script and cannot reach the network or your files')
    expect(w.text()).not.toContain('runs code written by someone else')
    expect(w.findAll('.page-files__item').map((i) => i.get('.page-files__path').text())).toEqual(['artifact.yaml', 'index.html.tmpl'])
    expect(w.get('[data-testid="view-trust-state"]').text()).toContain('Not allowed yet')
    expect(w.find('[data-testid="static-page-stub"]').exists()).toBe(false)
    expect(w.find('[data-testid="custom-view-stub"]').exists()).toBe(false)
  })

  it('Allow sends the hash on screen, then hosts the static frame, never the script worker', async () => {
    const app = makeApp(status())
    const w = await mountTrust()
    await w.get('[data-testid="view-trust-allow"]').trigger('click')
    await flushPromises()
    expect(app.AllowArtifactView).toHaveBeenCalledWith('pg', 'b'.repeat(64))
    expect(w.find('[data-testid="static-page-stub"]').exists()).toBe(true)
    expect(w.find('[data-testid="custom-view-stub"]').exists()).toBe(false)
  })

  it('an allowed scripted page goes to its own window and hosts no frame', async () => {
    makeApp(status({ state: 'allowed', page_kind: 'scripted' }))
    const w = await mountTrust()
    expect(w.find('[data-testid="static-page-stub"]').exists()).toBe(false)
    expect(w.find('[data-testid="scripted-page-stub"]').exists()).toBe(true)
    expect(w.find('iframe').exists()).toBe(false)
  })

  it('the allow screen of a scripted page says it has script and runs in its own window', async () => {
    makeApp(status({ page_kind: 'scripted' }))
    const w = await mountTrust()
    const line = w.get('[data-testid="view-trust-line"]').text()
    expect(line).toContain('This page has script')
    expect(line).toContain('in its own window')
    expect(line).not.toContain('It runs no script')
  })

  it('a denied page says so and hosts nothing', async () => {
    makeApp(status({ state: 'denied' }))
    const w = await mountTrust()
    expect(w.get('[data-testid="view-trust-denied"]').text()).toContain('You denied this page')
    expect(w.find('[data-testid="static-page-stub"]').exists()).toBe(false)
  })
})

describe('ArtifactViewTrust, HTML page source', () => {
  it('folds the files into details once allowed, and makes the viewer only when opened', async () => {
    makeApp(status({ state: 'allowed' }))
    const w = await mountTrust()
    const box = w.get('[data-testid="view-trust-page-src"]')
    expect(box.element.tagName).toBe('DETAILS')
    expect(w.find('[data-testid="page-files"]').exists()).toBe(false)
    ;(box.element as HTMLDetailsElement).open = true
    await box.trigger('toggle')
    expect(w.find('[data-testid="page-files"]').exists()).toBe(true)
  })

  it('shows the files unfolded before the person decides', async () => {
    makeApp(status())
    const w = await mountTrust()
    expect(w.get('[data-testid="view-trust-page-src"]').element.tagName).toBe('DIV')
    expect(w.find('[data-testid="page-files"]').exists()).toBe(true)
  })
})
