// ArtifactViewDecisions (#159): Settings > Workspace lists custom-view decisions with Revoke.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import ArtifactViewDecisions from './ArtifactViewDecisions.vue'

let activity: AgentActivityEntry[] = []

function makeApp(list: ArtifactViewDecision[]) {
  let cur = list
  const app: MockApp = {
    ListArtifactViewDecisions: vi.fn(async () => cur),
    ListAgentActivity: vi.fn(async () => activity),
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
    expect(w.get('[data-testid="view-decisions-empty"]').text()).toContain('No pages or custom views')
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

  it('toggles ask separately for a page that declares it', async () => {
    const app = makeApp([{ name: 'p', path: '/x/p', decision: 'allow', state: 'allowed', declaresAsk: true, askOn: true }, { name: 'q', path: '/x/q', decision: 'allow', state: 'allowed' }])
    ;(app as any).SetPageAsk = vi.fn(async () => {})
    const w = mount(ArtifactViewDecisions)
    await flushPromises()
    expect(w.find('[data-testid="view-decision-ask-q"]').exists()).toBe(false)
    await w.get('[data-testid="view-decision-ask-p"]').setValue(false)
    expect((app as any).SetPageAsk).toHaveBeenCalledWith('p', false)
  })

  it('shows the kind and capabilities of a page, and recent model use without any text', async () => {
    activity = [
      { at: '2026-10-22T10:00:00Z', source: 'page', name: 'older', kind: 'ask', promptBytes: 12, replyBytes: 0, outcome: 'no model' },
      { at: '2026-10-22T11:00:00Z', source: 'page', name: 'newer', kind: 'ask', promptBytes: 40, replyBytes: 99, outcome: 'ok' },
    ]
    makeApp([{ name: 'p', path: '/x/p', decision: 'allow', state: 'allowed', kind: 'html', pageKind: 'scripted', capabilities: ['ask', 'state'] }])
    const w = mount(ArtifactViewDecisions)
    await flushPromises()
    expect(w.get('[data-testid="view-decision-kind-p"]').text()).toBe('Scripted page')
    expect(w.get('[data-testid="view-decision-caps-p"]').text()).toBe('ask, state')
    expect(w.get('[data-testid="page-activity-0"]').text()).toContain('newer')
    expect(w.get('[data-testid="page-activity-0"]').text()).toContain('sent 40 bytes, got 99 bytes')
    expect(w.get('[data-testid="page-activity-1"]').text()).toContain('no model')
    activity = []
  })
})
