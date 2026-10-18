// ArtifactViewTrust (#159): a custom view is shown as source and never runs
// here; Allow sends back the hash that was on screen; a changed view asks again.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import ArtifactViewTrust from './ArtifactViewTrust.vue'

const status = (over: Partial<ArtifactViewStatus> = {}): ArtifactViewStatus => ({
  name: 'chart', kind: 'script', state: 'undecided', hash: 'a'.repeat(64),
  manifest: 'version: 1\ntitle: Chart\nview: script\n', source: 'function draw(ctx) { return "<svg/>" }\n', ...over,
})

function makeApp(initial: ArtifactViewStatus, over: MockApp = {}) {
  let cur = initial
  const app: MockApp = {
    ArtifactViewState: vi.fn(async () => cur),
    AllowArtifactView: vi.fn(async () => (cur = status({ state: 'allowed', hash: cur.hash }))),
    DenyArtifactView: vi.fn(async () => (cur = status({ state: 'denied', hash: cur.hash }))),
    RevokeArtifactView: vi.fn(async () => (cur = status({ state: 'undecided', hash: cur.hash }))),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountTrust() {
  const w = mount(ArtifactViewTrust, { props: { name: 'chart' }, global: { stubs: { Icon: true } } })
  await flushPromises()
  return w
}

describe('ArtifactViewTrust', () => {
  it('shows the source and the plain line, and runs nothing, until the person allows', async () => {
    const app = makeApp(status())
    const w = await mountTrust()
    expect(w.get('[data-testid="view-trust-line"]').text()).toContain('runs code written by someone else or by the agent')
    expect(w.get('[data-testid="view-trust-line"]').text()).toContain("cannot reach the network or your files, and can only run this artifact's queries")
    expect(w.get('[data-testid="view-trust-source"]').text()).toContain('function draw')
    expect(w.get('[data-testid="view-trust-state"]').text()).toContain('Not allowed yet')
    expect(w.find('[data-testid="view-trust-placeholder"]').exists()).toBe(false)
    expect(w.get('[data-testid="view-trust-hash"]').text()).toBe('aaaaaaaaaaaa')
    expect(app.RunArtifactQuery).toBeUndefined()
  })

  it('Allow sends the hash that was on screen, then shows the placeholder', async () => {
    const app = makeApp(status())
    const w = await mountTrust()
    await w.get('[data-testid="view-trust-allow"]').trigger('click')
    await flushPromises()
    expect(app.AllowArtifactView).toHaveBeenCalledWith('chart', 'a'.repeat(64))
    expect(w.get('[data-testid="view-trust-placeholder"]').text()).toContain('Allowed. The view host arrives in the next update.')
    expect(w.find('[data-testid="view-trust-allow"]').exists()).toBe(false)
    expect(w.find('[data-testid="view-trust-revoke"]').exists()).toBe(true)
  })

  it('a refused Allow shows the reason and the files as they are now', async () => {
    let calls = 0
    const app = makeApp(status(), {
      ArtifactViewState: vi.fn(async () => (++calls === 1 ? status() : status({ state: 'undecided', hash: 'b'.repeat(64), source: 'changed()\n' }))),
      AllowArtifactView: vi.fn(async () => { throw new Error('the view changed since it was shown; review it again') }),
    })
    const w = await mountTrust()
    await w.get('[data-testid="view-trust-allow"]').trigger('click')
    await flushPromises()
    expect(app.AllowArtifactView).toHaveBeenCalledTimes(1)
    expect(w.get('[data-testid="view-trust-error"]').text()).toContain('changed since it was shown')
    // The new bytes wait behind an acknowledgement; Allow stays off until then.
    expect(w.get('[data-testid="view-trust-changed-reading"]').text()).toContain('changed while you were reading it')
    expect((w.get('[data-testid="view-trust-allow"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.get('[data-testid="view-trust-source"]').text()).not.toContain('changed()')
    await w.get('[data-testid="view-trust-show-new"]').trigger('click')
    expect(w.get('[data-testid="view-trust-source"]').text()).toContain('changed()')
    expect(w.get('[data-testid="view-trust-hash"]').text()).toBe('bbbbbbbbbbbb')
    expect(w.find('[data-testid="view-trust-changed-reading"]').exists()).toBe(false)
    await w.get('[data-testid="view-trust-allow"]').trigger('click')
    await flushPromises()
    expect(app.AllowArtifactView).toHaveBeenLastCalledWith('chart', 'b'.repeat(64))
  })

  it('read-only editors show control and invisible characters instead of hiding them', async () => {
    makeApp(status({ source: 'draw()\u202e\u200b\n' }))
    const w = await mountTrust()
    expect(w.find('[data-testid="view-trust-source"] .cm-specialChar').exists()).toBe(true)
  })

  it('a changed view says so and asks again', async () => {
    makeApp(status({ state: 'changed' }))
    const w = await mountTrust()
    expect(w.get('[data-testid="view-trust-state"]').text()).toContain('Changed since you allowed it')
    expect(w.get('[data-testid="view-trust-line"]').text()).toContain('changed since you allowed it')
    expect(w.find('[data-testid="view-trust-allow"]').exists()).toBe(true)
    expect(w.find('[data-testid="view-trust-placeholder"]').exists()).toBe(false)
  })

  it('a denied view says it will not run and can be revoked', async () => {
    const app = makeApp(status({ state: 'denied' }))
    const w = await mountTrust()
    expect(w.get('[data-testid="view-trust-denied"]').text()).toContain('will not run')
    await w.get('[data-testid="view-trust-revoke"]').trigger('click')
    await flushPromises()
    expect(app.RevokeArtifactView).toHaveBeenCalledWith('chart')
    expect(w.find('[data-testid="view-trust-line"]').exists()).toBe(true)
  })

  it('shows the manifest tab and renders hostile text as text', async () => {
    makeApp(status({ source: '<img src=x onerror=alert(1)>\n', manifest: 'title: "<b>x</b>"\n' }))
    const w = await mountTrust()
    expect(w.find('[data-testid="view-trust-source"] img').exists()).toBe(false)
    await w.get('[data-testid="view-trust-tab-manifest"]').trigger('click')
    expect(w.get('[data-testid="view-trust-manifest"]').text()).toContain('<b>x</b>')
    expect(w.find('[data-testid="view-trust-manifest"] b').exists()).toBe(false)
  })

  it('an unusable view shows its error and offers no Allow', async () => {
    makeApp(status({ state: 'none', hash: '', error: 'view.js is empty' }))
    const w = await mountTrust()
    expect(w.get('[data-testid="view-trust-unusable"]').text()).toContain('view.js is empty')
    expect(w.find('[data-testid="view-trust-allow"]').exists()).toBe(false)
  })
})
