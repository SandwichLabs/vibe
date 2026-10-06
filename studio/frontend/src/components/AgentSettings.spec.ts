import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import AgentSettings from './AgentSettings.vue'
import { workspaceInfo, _resetForTests } from '../composables/useWorkspace'
import { resolveConfirmChoice, confirmVisible } from '../composables/useConfirm'

function info(over: Partial<WorkspaceInfo> = {}): WorkspaceInfo {
  return {
    root: '/w/a', id: 'i', name: 'A', sessionId: 's', gitBranch: '', isGit: false, defaultConnection: 'local',
    connections: [
      { name: 'local', type: 'duckdb', scope: 'shared', readOnly: false, default: true, status: 'ok' },
      { name: 'prod_pg', type: 'postgres', scope: 'shared', readOnly: false, default: false, status: 'ok' },
      { name: 'ro', type: 'postgres', scope: 'shared', readOnly: true, default: false, status: 'ok' },
    ],
    counts: { notebooks: 0, queries: 0, skills: 0, tables: 0 },
    policy: { sandbox: 'read-write', askBeforeWrites: true },
    team: { sandbox: 'full', askBeforeWrites: false }, teamSandboxSet: false,
    local: { sandbox: 'read-write', askBeforeWrites: true },
    readOnly: false, issues: [], warnings: [], duplicateIds: [], localUntrusted: false, mcpRunning: false,
    ...over,
  } as WorkspaceInfo
}

function makeApp(returns: Partial<WorkspaceInfo> = {}) {
  const app = { SetWorkspaceSettings: vi.fn(async () => info(returns)) }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

describe('AgentSettings (two layers)', () => {
  beforeEach(() => { _resetForTests(); workspaceInfo.value = info() })

  it('shows this person\'s tier and gate, and what the agent can change', () => {
    makeApp()
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="agent-sandbox-read-write"]').attributes('aria-pressed')).toBe('true')
    expect((w.find('[data-testid="agent-ask-before-writes"]').element as HTMLInputElement).checked).toBe(true)
    const table = w.find('[data-testid="agent-write-table"]')
    expect(table.text()).toContain('local')
    expect(table.text()).toContain('prod_pg')
    expect(table.text()).toContain('always asks first')
    expect(table.text()).not.toContain('ro ')
    expect(table.text()).toContain('Read-only. The agent can look but not change.')
  })

  it('this-computer changes save through SetWorkspaceSettings keeping the committed fields', async () => {
    const app = makeApp()
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="agent-sandbox-full"]').trigger('click')
    await flushPromises()
    expect(app.SetWorkspaceSettings).toHaveBeenCalledWith({
      name: 'A', defaultConnection: 'local', teamSandbox: '', teamAskBeforeChanges: false,
      sandbox: 'full', askBeforeChanges: true,
      loadedTeamSandbox: '', loadedTeamAsk: false,
    })
    await w.find('[data-testid="agent-ask-before-writes"]').setValue(false)
    await flushPromises()
    expect(app.SetWorkspaceSettings).toHaveBeenLastCalledWith(expect.objectContaining({ askBeforeChanges: false }))
  })

  it('options above the team limit are disabled with the reason', () => {
    makeApp()
    workspaceInfo.value = info({ team: { sandbox: 'read-write', askBeforeWrites: false }, teamSandboxSet: true })
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    const full = w.find('[data-testid="agent-sandbox-full"]')
    expect((full.element as HTMLButtonElement).disabled).toBe(true)
    expect(full.attributes('title')).toContain('The team limit is read-write')
  })

  it('a team-required gate cannot be turned off on this computer', () => {
    makeApp()
    workspaceInfo.value = info({ team: { sandbox: 'full', askBeforeWrites: true } })
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    expect((w.find('[data-testid="agent-ask-before-writes"]').element as HTMLInputElement).disabled).toBe(true)
    expect(w.text()).toContain('The team requires this, so it stays on.')
  })

  it('raising the team limit asks first, in plain words, and does nothing on Cancel', async () => {
    const app = makeApp()
    workspaceInfo.value = info({ team: { sandbox: 'read-only', askBeforeWrites: false }, teamSandboxSet: true, local: { sandbox: 'read-only', askBeforeWrites: true } })
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="team-sandbox-full"]').trigger('click')
    await flushPromises()
    expect(confirmVisible.value).toBe(true)
    expect(app.SetWorkspaceSettings).not.toHaveBeenCalled()
    resolveConfirmChoice('cancel')
    await flushPromises()
    expect(app.SetWorkspaceSettings).not.toHaveBeenCalled()

    await w.find('[data-testid="team-sandbox-full"]').trigger('click')
    await flushPromises()
    resolveConfirmChoice('confirm')
    await flushPromises()
    expect(app.SetWorkspaceSettings).toHaveBeenCalledWith(expect.objectContaining({ teamSandbox: '', loadedTeamSandbox: 'read-only' }))
  })

  it('tightening the team limit needs no confirmation', async () => {
    const app = makeApp()
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="team-sandbox-read-write"]').trigger('click')
    await flushPromises()
    expect(confirmVisible.value).toBe(false)
    expect(app.SetWorkspaceSettings).toHaveBeenCalled()
  })

  it('lowering the team limit below the person\'s own tier lowers theirs with it', async () => {
    const app = makeApp()
    workspaceInfo.value = info({ local: { sandbox: 'full', askBeforeWrites: true } })
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="team-sandbox-read-only"]').trigger('click')
    await flushPromises()
    expect(app.SetWorkspaceSettings).toHaveBeenCalledWith(expect.objectContaining({ teamSandbox: 'read-only', sandbox: 'read-only' }))
  })

  it('reports a refused change and keeps the old state', async () => {
    const app = makeApp()
    app.SetWorkspaceSettings.mockRejectedValueOnce(new Error('The team limit is read-only, so this computer can\'t go above it.'))
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="agent-sandbox-full"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain("can't go above it")
    expect(workspaceInfo.value?.local.sandbox).toBe('read-write')
  })
})

describe('AgentSettings stale form', () => {
  beforeEach(() => { _resetForTests(); workspaceInfo.value = info() })

  it('a refusal because vibe.yaml changed shows plain words and a Reload settings button', async () => {
    const app = { SetWorkspaceSettings: vi.fn(async () => { throw new Error("conflict: the team's agent limit in vibe.yaml changed since Settings was opened.") }), GetWorkspaceInfo: vi.fn(async () => info({ teamSandboxSet: true, team: { sandbox: 'read-only', askBeforeWrites: false } })) }
    ;(globalThis as any).window = (globalThis as any).window || {}
    ;(window as any).go = { main: { App: app } }
    const w = mount(AgentSettings, { global: { stubs: { Icon: true } } })
    await w.find('[data-testid="agent-sandbox-full"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain("Error: the team's agent limit in vibe.yaml changed")
    expect(w.text()).not.toContain('conflict:')
    await w.find('[data-testid="reload-settings"]').trigger('click')
    await flushPromises()
    expect(app.GetWorkspaceInfo).toHaveBeenCalled()
    expect(workspaceInfo.value?.team.sandbox).toBe('read-only')
    expect(w.find('[data-testid="reload-settings"]').exists()).toBe(false)
  })
})
