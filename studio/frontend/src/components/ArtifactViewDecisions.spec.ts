// ArtifactViewDecisions (#159): Settings > Workspace lists custom-view decisions with Revoke.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import ArtifactViewDecisions from './ArtifactViewDecisions.vue'

function makeApp(list: ArtifactViewDecision[]) {
  let cur = list
  const app: MockApp = {
    ListArtifactViewDecisions: vi.fn(async () => cur),
    RevokeArtifactView: vi.fn(async (n: string) => { cur = cur.filter((d) => d.name !== n); return {} }),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

describe('ArtifactViewDecisions', () => {
  it('says so when there are none', async () => {
    makeApp([])
    const w = mount(ArtifactViewDecisions)
    await flushPromises()
    expect(w.get('[data-testid="view-decisions-empty"]').text()).toContain('No custom artifact views')
  })

  it('lists decisions and revokes one', async () => {
    const app = makeApp([
      { name: 'a-chart', path: '/x/a-chart', decision: 'deny', state: 'denied' },
      { name: 'b-chart', path: '/x/b-chart', decision: 'allow', state: 'changed' },
    ])
    const w = mount(ArtifactViewDecisions)
    await flushPromises()
    expect(w.get('[data-testid="view-decision-a-chart"]').text()).toContain('Denied')
    expect(w.get('[data-testid="view-decision-b-chart"]').text()).toContain('edited since')
    await w.get('[data-testid="view-decision-revoke-a-chart"]').trigger('click')
    await flushPromises()
    expect(app.RevokeArtifactView).toHaveBeenCalledWith('a-chart')
    expect(w.find('[data-testid="view-decision-a-chart"]').exists()).toBe(false)
    expect(w.find('[data-testid="view-decision-b-chart"]').exists()).toBe(true)
  })
})
